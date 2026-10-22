//go:build cgo

package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/daulet/tokenizers"
	"github.com/rickchristie/gent/common"
	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/sync/semaphore"
)

// onnxEmbedder implements [Embedder] using ONNX Runtime for inference and daulet/tokenizers
// for tokenization. Works with any BERT-family sentence embedding model exported to ONNX
// (e5, MiniLM, BGE, Nomic, GTE, etc.).
//
// Library choice: we use yalue/onnxruntime_go + daulet/tokenizers instead of the
// higher-level knights-analytics/hugot because it gives more control over the inference
// pipeline (custom pooling strategies, PostProcess hooks, per-model input tensor
// configuration). Both require CGo.
//
// The embedder handles the full pipeline: tokenize → pad → ONNX inference → pool →
// L2 normalize. Prefixes are prepended internally based on whether EmbedQuery or
// EmbedText is called.
type onnxEmbedder struct {
	tokenizer      *tokenizers.Tokenizer
	queryPrefix    string
	passagePrefix  string
	maxSeqLen      int
	hiddenDim      int // final output dimensionality (what Dimensions() returns)
	modelDim       int // ONNX model's native hidden dim (for output tensor shape)
	pooling        PoolingStrategy
	inputNames     []string
	outputName     string
	postProcess    func([]float32) []float32
	sem            *semaphore.Weighted
	maxConcurrency int64
	session        *ort.DynamicAdvancedSession
	numThreads     int
	mu             sync.RWMutex
	closed         bool
	closeErr       error
}

// ortInitOnce ensures ONNX Runtime environment is initialized exactly once.
var ortInitOnce sync.Once
var ortInitErr error

// NewOnnxEmbedder creates a new ONNX-based Embedder from a ModelConfig (model semantics) and
// OnnxOptions (runtime settings). A warm-up inference is run to trigger ONNX Runtime JIT
// compilation.
//
// Usage:
//
//	cfg := common.FindConfig("multilingual-e5-small")
//	embedder, err := search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
//	    ModelPath:     "~/.gent/models/multilingual-e5-small/model.onnx",
//	    TokenizerPath: "~/.gent/models/multilingual-e5-small/tokenizer.json",
//	})
func NewOnnxEmbedder(cfg common.ModelConfig, opts OnnxOptions) (Embedder, error) {
	if opts.NumThreads == 0 {
		opts.NumThreads = 4
	}
	if opts.MaxConcurrency == 0 {
		opts.MaxConcurrency = 4
	}

	if opts.NumThreads < 1 || opts.MaxConcurrency < 1 {
		return nil, errors.New("search: NumThreads and MaxConcurrency must be positive")
	}

	// Load model: file path takes precedence over raw bytes.
	modelData, err := loadData(opts.ModelPath, opts.ModelData, "model")
	if err != nil {
		return nil, err
	}
	tokenizerData, err := loadData(opts.TokenizerPath, opts.TokenizerData, "tokenizer")
	if err != nil {
		return nil, err
	}

	// Initialize ONNX Runtime environment (once globally).
	// Resolution order: OnnxOptions → GENT_ORT_LIB env → ~/.gent/lib/ → error.
	ortInitOnce.Do(func() {
		libPath := resolveOnnxLibPath(opts.OnnxLibraryPath)
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		ortInitErr = ort.InitializeEnvironment()
	})
	if ortInitErr != nil {
		return nil, onnxUnavailableError{fmt.Errorf("search: ONNX Runtime init failed: %w\n"+
			"Run 'gent setup onnx' to install native libraries", ortInitErr)}
	}

	// Load tokenizer from bytes.
	tk, err := tokenizers.FromBytes(tokenizerData)
	if err != nil {
		return nil, fmt.Errorf("search: tokenizer load failed: %w", err)
	}

	modelDim := cfg.HiddenDim()
	maxSeqLen := cfg.Model.MaxTokenChunks
	if maxSeqLen == 0 {
		maxSeqLen = 512
	}

	e := &onnxEmbedder{
		tokenizer:      tk,
		queryPrefix:    cfg.QueryPrefix,
		passagePrefix:  cfg.PassagePrefix,
		maxSeqLen:      maxSeqLen,
		hiddenDim:      cfg.Dimensions,
		modelDim:       modelDim,
		pooling:        cfg.Pooling,
		inputNames:     append([]string(nil), cfg.InputNames...),
		outputName:     cfg.OutputName,
		postProcess:    cfg.PostProcess,
		sem:            semaphore.NewWeighted(int64(opts.MaxConcurrency)),
		maxConcurrency: int64(opts.MaxConcurrency),
		numThreads:     opts.NumThreads,
	}

	// The CPU provider supports concurrent Run calls on one session. Each call owns its tensors
	// and run options, so sharing the graph avoids extra model copies and native thread pools.
	// https://github.com/microsoft/onnxruntime/discussions/10107
	e.session, err = e.newSession(modelData)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("search: ONNX session creation failed: %w", err), e.Close())
	}

	// Keep the warm session alive. Later calls reuse the same optimized graph.
	if _, err = e.embed(context.Background(), "warmup"); err != nil {
		return nil, errors.Join(fmt.Errorf("search: warm-up inference failed: %w", err), e.Close())
	}

	return e, nil
}

// loadData loads from a file path (preferred) or falls back to raw bytes.
func loadData(path string, data []byte, label string) ([]byte, error) {
	if path != "" {
		d, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("search: failed to read %s file %q: %w", label, path, err)
		}
		return d, nil
	}
	if len(data) > 0 {
		return data, nil
	}
	return nil, fmt.Errorf("search: no %s provided; set %sPath or %sData in OnnxOptions",
		label, label, label)
}

func (e *onnxEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return e.embed(ctx, e.queryPrefix+text)
}

func (e *onnxEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return e.embed(ctx, e.passagePrefix+text)
}

func (e *onnxEmbedder) EmbedTextBatch(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.EmbedText(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("search: batch embed failed at index %d: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

func (e *onnxEmbedder) Dimensions() int { return e.hiddenDim }
func (e *onnxEmbedder) MaxTokens() int  { return e.maxSeqLen }

// TokenCount returns the number of tokens the text produces when tokenized. This only runs
// the tokenizer (~12μs), not ONNX inference (~15-200ms).
func (e *onnxEmbedder) TokenCount(text string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return 0
	}
	ids, _ := e.tokenizer.Encode(text, true)
	return len(ids)
}

func (e *onnxEmbedder) Close() error {
	// Take all inference permits before the lifetime lock. Later embeddings then wait on the
	// context-aware semaphore, not on RLock behind a pending close. TokenCount still uses mu
	// because it has no context or inference limit. Always acquire permits before mu.
	if err := e.sem.Acquire(context.Background(), e.maxConcurrency); err != nil {
		return err
	}
	defer e.sem.Release(e.maxConcurrency)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return e.closeErr
	}
	e.closed = true
	e.tokenizer.Close()
	if e.session != nil {
		releaseOnnx(e.session, &e.closeErr)
		e.session = nil
	}
	return e.closeErr
}

// releaseOnnx reports cleanup failures along with the original operation error.
func releaseOnnx(value interface{ Destroy() error }, result *error) {
	if err := value.Destroy(); err != nil {
		*result = errors.Join(*result, fmt.Errorf("search: release ONNX resource: %w", err))
	}
}

func (e *onnxEmbedder) newSession(
	modelData []byte,
) (session *ort.DynamicAdvancedSession, err error) {
	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer releaseOnnx(options, &err)
	if err := options.SetIntraOpNumThreads(e.numThreads); err != nil {
		return nil, err
	}
	// Stop spinning when no inference is active. Keep parallel kernels available during a run.
	if err := options.AddSessionConfigEntry("session.force_spinning_stop", "1"); err != nil {
		return nil, fmt.Errorf("search: stop idle ONNX threads: %w", err)
	}
	// Full-range INT8 weights can overflow ORT's U8S8 AVX2 kernels without VNNI. Preserve
	// integer precision so reuse and thread settings do not change embeddings or rankings.
	// https://onnxruntime.ai/docs/performance/model-optimizations/quantization.html
	if err := options.AddSessionConfigEntry("session.x64quantprecision", "1"); err != nil {
		return nil, fmt.Errorf("search: enable ONNX quantization precision: %w", err)
	}
	// Default session loading copies model bytes; do not retain a second model in the Go heap.
	return ort.NewDynamicAdvancedSessionWithONNXData(modelData, e.inputNames,
		[]string{e.outputName}, options)
}

func runOnnx(ctx context.Context, session *ort.DynamicAdvancedSession,
	inputs, outputs []ort.Value,
) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Done() == nil {
		return session.Run(inputs, outputs)
	}
	options, err := ort.NewRunOptions()
	if err != nil {
		return err
	}
	defer releaseOnnx(options, &err)
	stopped := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() { stopped <- options.Terminate() })
	defer func() {
		// Terminate must finish before its native run options can be released.
		if !stop() {
			err = errors.Join(err, <-stopped)
		}
	}()
	err = session.RunWithOptions(inputs, outputs, options)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// embed runs the full pipeline: tokenize → pad → inference → mean pool → L2 normalize.
func (e *onnxEmbedder) embed(ctx context.Context, text string) (embedding []float32, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer e.sem.Release(1)
	e.mu.RLock()
	defer e.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.closed {
		return nil, errors.New("search: embedder is closed")
	}
	// Tokenize.
	enc := e.tokenizer.EncodeWithOptions(text, true,
		tokenizers.WithReturnAttentionMask(), tokenizers.WithReturnTypeIDs(),
	)
	tokenIDs := enc.IDs
	attentionMask := enc.AttentionMask
	typeIDs := enc.TypeIDs

	// Truncate to max sequence length.
	seqLen := len(tokenIDs)
	if seqLen > e.maxSeqLen {
		seqLen = e.maxSeqLen
		tokenIDs = tokenIDs[:seqLen]
		attentionMask = attentionMask[:seqLen]
		typeIDs = typeIDs[:seqLen]
	}

	// Convert to int64 for ONNX.
	inputIDs := toInt64(tokenIDs)
	maskI64 := toInt64(attentionMask)
	typeI64 := toInt64(typeIDs)

	// Create ONNX input tensors based on configured InputNames. Some models (e.g., e5-base
	// with XLMRoberta architecture) only accept input_ids + attention_mask, while others
	// (BERT-based) also need token_type_ids.
	shape := ort.NewShape(1, int64(seqLen))
	tensorMap := map[string]func() (*ort.Tensor[int64], error){
		"input_ids":      func() (*ort.Tensor[int64], error) { return ort.NewTensor(shape, inputIDs) },
		"attention_mask": func() (*ort.Tensor[int64], error) { return ort.NewTensor(shape, maskI64) },
		"token_type_ids": func() (*ort.Tensor[int64], error) { return ort.NewTensor(shape, typeI64) },
	}

	var inputs []ort.Value
	defer func() {
		for _, tensor := range inputs {
			releaseOnnx(tensor, &err)
		}
	}()
	for _, name := range e.inputNames {
		create, ok := tensorMap[name]
		if !ok {
			return nil, fmt.Errorf("search: unknown input name %q", name)
		}
		tensor, createErr := create()
		if createErr != nil {
			return nil, fmt.Errorf("search: %s tensor: %w", name, createErr)
		}
		inputs = append(inputs, tensor)
	}

	// Create output tensor. Shape: [1, seqLen, modelDim]. Uses the model's native hidden dim,
	// not the final output dim (which may be smaller after PostProcess truncation).
	outputShape := ort.NewShape(1, int64(seqLen), int64(e.modelDim))
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("search: output tensor: %w", err)
	}
	defer releaseOnnx(outputTensor, &err)
	if err := runOnnx(ctx, e.session, inputs, []ort.Value{outputTensor}); err != nil {
		return nil, fmt.Errorf("search: ONNX inference failed: %w", err)
	}

	// Extract output, apply pooling strategy, and L2 normalize.
	outputData := outputTensor.GetData()
	tokenEmbeddings := reshapeToMatrix(outputData, seqLen, e.modelDim)

	var pooled []float32
	switch e.pooling {
	case PoolingCLS:
		pooled = clsPool(tokenEmbeddings)
	default:
		pooled = meanPool(tokenEmbeddings, attentionMask)
	}
	if e.postProcess != nil {
		pooled = e.postProcess(pooled)
	}
	return l2Normalize(pooled), nil
}

// toInt64 converts []uint32 to []int64.
func toInt64(ids []uint32) []int64 {
	result := make([]int64, len(ids))
	for i, id := range ids {
		result[i] = int64(id)
	}
	return result
}

// reshapeToMatrix converts a flat [seqLen*hiddenDim] slice into [seqLen][hiddenDim].
func reshapeToMatrix(flat []float32, rows, cols int) [][]float32 {
	matrix := make([][]float32, rows)
	for i := range rows {
		matrix[i] = flat[i*cols : (i+1)*cols]
	}
	return matrix
}

// clsPool returns the first token's ([CLS]) embedding. Used by BGE and Arctic models.
func clsPool(tokenEmbeddings [][]float32) []float32 {
	out := make([]float32, len(tokenEmbeddings[0]))
	copy(out, tokenEmbeddings[0])
	return out
}

// meanPool computes mean pooling over token embeddings, masked by attention_mask.
func meanPool(tokenEmbeddings [][]float32, attentionMask []uint32) []float32 {
	hiddenDim := len(tokenEmbeddings[0])
	result := make([]float32, hiddenDim)
	var maskSum float32
	for i, mask := range attentionMask {
		if mask == 1 {
			maskSum++
			for j := range hiddenDim {
				result[j] += tokenEmbeddings[i][j]
			}
		}
	}
	if maskSum > 0 {
		for j := range result {
			result[j] /= maskSum
		}
	}
	return result
}

// l2Normalize normalizes a vector to unit length.
func l2Normalize(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		normF := float32(norm)
		for i := range v {
			v[i] /= normF
		}
	}
	return v
}

// resolveOnnxLibPath determines the ONNX Runtime shared library path using a deterministic
// resolution order:
//  1. Explicit configPath (from OnnxOptions.OnnxLibraryPath)
//  2. GENT_ORT_LIB environment variable (for CI/containers)
//  3. ~/.gent/lib/ (default install location from setup tool)
//  4. Empty string (let ONNX Runtime try its default, which will likely fail)
func resolveOnnxLibPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	if envPath := os.Getenv("GENT_ORT_LIB"); envPath != "" {
		return envPath
	}
	if gentLib := gentLibPath(); gentLib != "" {
		return gentLib
	}
	return ""
}

// gentLibPath returns the path to the ONNX Runtime shared library in ~/.gent/lib/,
// or empty string if not found.
func gentLibPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	libDir := filepath.Join(home, ".gent", "lib")

	// Try platform-specific extensions.
	for _, name := range []string{"libonnxruntime.so", "libonnxruntime.dylib"} {
		p := filepath.Join(libDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try versioned .so files (e.g., libonnxruntime.so.1.24.4).
	matches, _ := filepath.Glob(filepath.Join(libDir, "libonnxruntime.so.*"))
	best := ""
	for _, m := range matches {
		if len(m) > len(best) {
			best = m
		}
	}
	return best
}

// Compile-time check.
var _ Embedder = (*onnxEmbedder)(nil)

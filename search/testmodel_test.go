//go:build cgo

package search

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
)

// testEmbedderForConfig creates an Embedder for a ModelConfig. Skips the test if the model
// files are missing or ORT is unavailable.
func testEmbedderForConfig(t testing.TB, cfg common.ModelConfig) Embedder {
	t.Helper()

	if !common.ModelDownloaded(&cfg.Model) {
		t.Skipf("model files not downloaded for %s", cfg.Model.Name)
	}

	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		t.Fatalf("cannot determine model dir: %v", err)
	}

	embedder, err := NewOnnxEmbedder(cfg, OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     2,
		MaxConcurrency: 2,
	})
	if errors.Is(err, ErrOnnxUnavailable) {
		t.Skipf("ONNX embedder not available for %s (run gent setup onnx): %v",
			cfg.ConfigName, err)
	}
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return embedder
}

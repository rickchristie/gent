//go:build cgo

package search

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent/common"
)

// ensureAllTestModels downloads all unique models from common.ModelRegistry using the shared
// common.DownloadModel function. Called from TestMain. Models are stored at
// ~/.gent/models/{modelName}/ — the same location used by the CLI.
func ensureAllTestModels(_ *testing.M) {
	for i := range common.ModelRegistry {
		model := &common.ModelRegistry[i]
		if common.ModelDownloaded(model) {
			continue
		}
		fmt.Printf("Downloading test model %s...\n", model.Name)
		if err := common.DownloadModel(model, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s: %v\n", model.Name, err)
		}
	}
}

// testEmbedderForConfig creates an Embedder for a ModelConfig. Skips the test if the model
// files are missing or ORT is unavailable.
func testEmbedderForConfig(t *testing.T, cfg common.ModelConfig) Embedder {
	t.Helper()

	if !common.ModelDownloaded(&cfg.Model) {
		t.Skipf("model files not downloaded for %s", cfg.Model.Name)
	}

	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		t.Skipf("cannot determine model dir: %v", err)
	}

	embedder, err := NewOnnxEmbedder(cfg, OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     2,
		MaxConcurrency: 2,
	})
	if err != nil {
		t.Skipf("ONNX embedder not available for %s (run gent setup onnx): %v",
			cfg.ConfigName, err)
	}
	return embedder
}

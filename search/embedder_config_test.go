package search_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnnxOptions_UserSnippetPattern(t *testing.T) {
	type input struct {
		configName string
	}
	type expected struct {
		options search.OnnxOptions
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "multilingual e5 small",
			input: input{configName: "multilingual-e5-small"},
			expected: expected{
				options: search.OnnxOptions{
					ModelPath: filepath.Join(
						mustModelDir(t, "multilingual-e5-small"),
						"model_qint8_avx512_vnni.onnx",
					),
					TokenizerPath:  filepath.Join(mustModelDir(t, "multilingual-e5-small"), "tokenizer.json"),
					NumThreads:     4,
					MaxConcurrency: 4,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := common.FindConfig(tt.input.configName)
			require.NotNil(t, cfg)

			dir, err := common.ModelDir(cfg.Model.Name)
			require.NoError(t, err)

			options := search.OnnxOptions{
				ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
				TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
				NumThreads:     4,
				MaxConcurrency: 4,
			}

			assert.Equal(t, tt.expected.options, options)
		})
	}
}

func userSnippetPatternForCompilation() (search.Embedder, error) {
	cfg := common.FindConfig("multilingual-e5-small")
	if cfg == nil {
		return nil, fmt.Errorf("unknown embedding config: %s", "multilingual-e5-small")
	}

	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		return nil, err
	}

	return search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     4,
		MaxConcurrency: 4,
	})
}

func mustModelDir(t *testing.T, modelName string) string {
	t.Helper()
	dir, err := common.ModelDir(modelName)
	require.NoError(t, err)
	return dir
}

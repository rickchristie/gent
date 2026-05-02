package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintModelList(t *testing.T) {
	type expected struct {
		output string
	}

	expectedValue := expected{output: expectedModelListOutput()}

	var buf bytes.Buffer
	printModelList(&buf)

	assert.Equal(t, expectedValue.output, buf.String())
}

func TestPrintConfigs(t *testing.T) {
	type input struct {
		modelName string
		modelDir  string
	}
	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single config model prints current embedder API",
			input: input{
				modelName: "multilingual-e5-small",
				modelDir:  "/tmp/gent-models/multilingual-e5-small",
			},
			expected: expected{
				output: expectedConfigOutput(
					"multilingual-e5-small", "/tmp/gent-models/multilingual-e5-small",
				),
			},
		},
		{
			name: "multi config model prints every runtime config",
			input: input{
				modelName: "nomic-embed-text-v1.5",
				modelDir:  "/tmp/gent-models/nomic-embed-text-v1.5",
			},
			expected: expected{
				output: expectedConfigOutput(
					"nomic-embed-text-v1.5", "/tmp/gent-models/nomic-embed-text-v1.5",
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := FindModel(tt.input.modelName)
			require.NotNil(t, model)

			var buf bytes.Buffer
			printConfigs(&buf, model, tt.input.modelDir)

			assert.Equal(t, tt.expected.output, buf.String())
			assert.NotContains(t, buf.String(), "search.EmbedderConfig")
		})
	}
}

func TestPrintUnknownModel(t *testing.T) {
	type input struct {
		modelName string
	}
	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "unknown model lists valid model names",
			input:    input{modelName: "does-not-exist"},
			expected: expected{output: expectedUnknownModelOutput("does-not-exist")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printUnknownModel(&buf, tt.input.modelName)

			assert.Equal(t, tt.expected.output, buf.String())
		})
	}
}

func TestRunModelDownload_UnknownModelExitsNonZero(t *testing.T) {
	if os.Getenv("GENT_TEST_RUN_MODEL_DOWNLOAD_UNKNOWN") == "1" {
		runModelDownload("does-not-exist")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRunModelDownload_UnknownModelExitsNonZero")
	cmd.Env = append(os.Environ(), "GENT_TEST_RUN_MODEL_DOWNLOAD_UNKNOWN=1")
	output, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Equal(t, expectedUnknownModelOutput("does-not-exist"), string(output))
}

func TestPrintSetupNextSteps(t *testing.T) {
	type expected struct {
		output string
	}

	expectedValue := expected{output: "  go build ./...\n" +
		"  go test ./...\n" +
		"\n" +
		"Next step: download an embedding model:\n" +
		"  gent model list\n" +
		"  gent model download multilingual-e5-small\n"}

	var buf bytes.Buffer
	printSetupNextSteps(&buf)

	assert.Equal(t, expectedValue.output, buf.String())
	assert.NotNil(t, FindModel("multilingual-e5-small"))
}

func expectedModelListOutput() string {
	var b strings.Builder
	fmt.Fprintln(&b, "Downloadable embedding model files:")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "  %-28s %-8s %-8s %-18s %s\n",
		"NAME", "PARAMS", "SIZE", "LANGUAGES", "NOTES")
	for _, m := range Registry {
		notes := m.Notes
		if notes == "" {
			notes = "-"
		}
		fmt.Fprintf(&b, "  %-28s %-8s %-8s %-18s %s\n",
			m.Name, m.Params, m.Size, m.Languages, notes)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Runtime configurations:")
	fmt.Fprintln(&b, "  One downloaded model file may support multiple runtime configurations.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "  %-35s %-6s %-7s %-10s %-10s %s\n",
		"CONFIG", "DIMS", "POOL", "10MB TEXT", "100MB TEXT", "BEST FOR")
	for _, c := range common.ConfigRegistry {
		poolStr := "mean"
		if c.Pooling == common.PoolingCLS {
			poolStr = "cls"
		}
		mem10 := c.EstimateMemoryMB(10 * 1024 * 1024)
		mem100 := c.EstimateMemoryMB(100 * 1024 * 1024)
		fmt.Fprintf(&b, "  %-35s %-6d %-7s %-10s %-10s %s\n",
			c.ConfigName, c.Dimensions, poolStr,
			fmt.Sprintf("%d MB", mem10), fmt.Sprintf("%d MB", mem100), c.BestFor)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Install CLI: go install github.com/rickchristie/gent/cmd/gent@v0.1.0")
	fmt.Fprintln(&b, "Download: gent model download <model-name>")
	return b.String()
}

func expectedConfigOutput(modelName, modelDir string) string {
	configs := common.ConfigsForModel(modelName)
	if len(configs) == 0 {
		return "No configurations registered for this model.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Downloaded files are in %s/\n\n", modelDir)
	fmt.Fprintf(&b, "Available runtime configurations (%d):\n", len(configs))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Required imports:")
	fmt.Fprintln(&b, "  \"fmt\"")
	fmt.Fprintln(&b, "  \"path/filepath\"")
	fmt.Fprintln(&b, "  \"github.com/rickchristie/gent/common\"")
	fmt.Fprintln(&b, "  \"github.com/rickchristie/gent/search\"")
	for _, cfg := range configs {
		fmt.Fprintf(&b, "\n  --- %s ---\n", cfg.ConfigName)
		fmt.Fprintf(&b, "  %s\n\n", cfg.Description)
		fmt.Fprintln(&b, "  func newEmbedder() (search.Embedder, error) {")
		fmt.Fprintf(&b, "      cfg := common.FindConfig(%q)\n", cfg.ConfigName)
		fmt.Fprintln(&b, "      if cfg == nil {")
		fmt.Fprintf(&b, "          return nil, fmt.Errorf(%q, %q)\n",
			"unknown embedding config: %s", cfg.ConfigName)
		fmt.Fprintln(&b, "      }")
		fmt.Fprintln(&b, "      dir, err := common.ModelDir(cfg.Model.Name)")
		fmt.Fprintln(&b, "      if err != nil {")
		fmt.Fprintln(&b, "          return nil, err")
		fmt.Fprintln(&b, "      }")
		fmt.Fprintln(&b, "      return search.NewOnnxEmbedder(*cfg, search.OnnxOptions{")
		fmt.Fprintln(&b, "          ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),")
		fmt.Fprintln(&b, "          TokenizerPath:  filepath.Join(dir, \"tokenizer.json\"),")
		fmt.Fprintln(&b, "          NumThreads:     4,")
		fmt.Fprintln(&b, "          MaxConcurrency: 4,")
		fmt.Fprintln(&b, "      })")
		fmt.Fprintln(&b, "  }")
		fmt.Fprintln(&b, "  // Caller owns the returned embedder and should call Close().")
	}
	return b.String()
}

func expectedUnknownModelOutput(name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Unknown model: %s\n\n", name)
	fmt.Fprintln(&b, "Available models:")
	for _, m := range Registry {
		fmt.Fprintf(&b, "  %s\n", m.Name)
	}
	return b.String()
}

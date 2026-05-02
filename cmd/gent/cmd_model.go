package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rickchristie/gent/common"
)

func runModelList() {
	printModelList(os.Stdout)
}

func printModelList(w io.Writer) {
	fmt.Fprintln(w, "Downloadable embedding model files:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-28s %-8s %-8s %-18s %s\n",
		"NAME", "PARAMS", "SIZE", "LANGUAGES", "NOTES")
	for _, m := range Registry {
		notes := m.Notes
		if notes == "" {
			notes = "-"
		}
		fmt.Fprintf(w, "  %-28s %-8s %-8s %-18s %s\n",
			m.Name, m.Params, m.Size, m.Languages, notes)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Runtime configurations:")
	fmt.Fprintln(w, "  One downloaded model file may support multiple runtime configurations.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-35s %-6s %-7s %-10s %-10s %s\n",
		"CONFIG", "DIMS", "POOL", "10MB TEXT", "100MB TEXT", "BEST FOR")
	for _, c := range common.ConfigRegistry {
		poolStr := "mean"
		if c.Pooling == common.PoolingCLS {
			poolStr = "cls"
		}
		mem10 := c.EstimateMemoryMB(10 * 1024 * 1024)
		mem100 := c.EstimateMemoryMB(100 * 1024 * 1024)
		fmt.Fprintf(w, "  %-35s %-6d %-7s %-10s %-10s %s\n",
			c.ConfigName, c.Dimensions, poolStr,
			fmt.Sprintf("%d MB", mem10), fmt.Sprintf("%d MB", mem100), c.BestFor)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install CLI: go install github.com/rickchristie/gent/cmd/gent@v0.1.0")
	fmt.Fprintln(w, "Download: gent model download <model-name>")
}

func runModelDownload(name string) {
	model := FindModel(name)
	if model == nil {
		printUnknownModel(os.Stdout, name)
		os.Exit(1)
	}

	modelDir, err := modelDownloadDir(model.Name)
	if err != nil {
		fatalf("Cannot determine download directory: %v", err)
	}

	modelPath := filepath.Join(modelDir, model.ModelFile)
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	// Check if already downloaded.
	if fileExists(modelPath) && fileExists(tokenizerPath) {
		fmt.Printf("Model %s is already downloaded at:\n  %s\n\n", model.Name, modelDir)
		printConfigs(os.Stdout, model, modelDir)
		return
	}

	fmt.Printf("Downloading %s (%s)...\n", model.Name, model.Size)
	if err := common.DownloadModel(model, common.PrintProgress); err != nil {
		fatalf("%v", err)
	}

	fmt.Println()
	fmt.Printf("✓ Downloaded to %s/\n", modelDir)
	fmt.Println()
	printConfigs(os.Stdout, model, modelDir)
}

func printUnknownModel(w io.Writer, name string) {
	fmt.Fprintf(w, "Unknown model: %s\n\n", name)
	fmt.Fprintln(w, "Available models:")
	for _, m := range Registry {
		fmt.Fprintf(w, "  %s\n", m.Name)
	}
}

func printConfigs(w io.Writer, model *ModelInfo, modelDir string) {
	configs := common.ConfigsForModel(model.Name)
	if len(configs) == 0 {
		fmt.Fprintln(w, "No configurations registered for this model.")
		return
	}

	fmt.Fprintf(w, "Downloaded files are in %s/\n\n", modelDir)
	fmt.Fprintf(w, "Available runtime configurations (%d):\n", len(configs))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Required imports:")
	fmt.Fprintln(w, "  \"fmt\"")
	fmt.Fprintln(w, "  \"path/filepath\"")
	fmt.Fprintln(w, "  \"github.com/rickchristie/gent/common\"")
	fmt.Fprintln(w, "  \"github.com/rickchristie/gent/search\"")
	for _, cfg := range configs {
		fmt.Fprintf(w, "\n  --- %s ---\n", cfg.ConfigName)
		fmt.Fprintf(w, "  %s\n\n", cfg.Description)
		fmt.Fprintln(w, "  func newEmbedder() (search.Embedder, error) {")
		fmt.Fprintf(w, "      cfg := common.FindConfig(%q)\n", cfg.ConfigName)
		fmt.Fprintln(w, "      if cfg == nil {")
		fmt.Fprintf(w, "          return nil, fmt.Errorf(%q, %q)\n",
			"unknown embedding config: %s", cfg.ConfigName)
		fmt.Fprintln(w, "      }")
		fmt.Fprintln(w, "      dir, err := common.ModelDir(cfg.Model.Name)")
		fmt.Fprintln(w, "      if err != nil {")
		fmt.Fprintln(w, "          return nil, err")
		fmt.Fprintln(w, "      }")
		fmt.Fprintln(w, "      return search.NewOnnxEmbedder(*cfg, search.OnnxOptions{")
		fmt.Fprintln(w, "          ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),")
		fmt.Fprintln(w, "          TokenizerPath:  filepath.Join(dir, \"tokenizer.json\"),")
		fmt.Fprintln(w, "          NumThreads:     4,")
		fmt.Fprintln(w, "          MaxConcurrency: 4,")
		fmt.Fprintln(w, "      })")
		fmt.Fprintln(w, "  }")
		fmt.Fprintln(w, "  // Caller owns the returned embedder and should call Close().")
	}
}

func modelDownloadDir(name string) (string, error) { return common.ModelDir(name) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

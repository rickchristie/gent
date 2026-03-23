package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickchristie/gent/common"
)

func runModelList() {
	fmt.Println("Available embedding models:")
	fmt.Println()
	fmt.Printf("  %-28s %-8s %-8s %-18s %s\n", "NAME", "PARAMS", "SIZE", "LANGUAGES", "NOTES")
	for _, m := range Registry {
		notes := m.Notes
		if notes == "" {
			notes = "-"
		}
		fmt.Printf("  %-28s %-8s %-8s %-18s %s\n",
			m.Name, m.Params, m.Size, m.Languages, notes)
	}
	fmt.Println()
	fmt.Println("Configurations per model:")
	fmt.Println()
	fmt.Printf("  %-35s %-6s %-7s %-10s %-10s %s\n",
		"CONFIG", "DIMS", "POOL", "10MB TEXT", "100MB TEXT", "BEST FOR")
	for _, c := range common.ConfigRegistry {
		poolStr := "mean"
		if c.Pooling == common.PoolingCLS {
			poolStr = "cls"
		}
		mem10 := c.EstimateMemoryMB(10 * 1024 * 1024)
		mem100 := c.EstimateMemoryMB(100 * 1024 * 1024)
		fmt.Printf("  %-35s %-6d %-7s %-10s %-10s %s\n",
			c.ConfigName, c.Dimensions, poolStr,
			fmt.Sprintf("%d MB", mem10), fmt.Sprintf("%d MB", mem100), c.BestFor)
	}
	fmt.Println()
	fmt.Println("Download: gent model download <model-name>")
}

func runModelDownload(name string) {
	model := FindModel(name)
	if model == nil {
		fmt.Printf("Unknown model: %s\n\n", name)
		fmt.Println("Available models:")
		for _, m := range Registry {
			fmt.Printf("  %s\n", m.Name)
		}
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
		printConfigs(model, modelDir)
		return
	}

	fmt.Printf("Downloading %s (%s)...\n", model.Name, model.Size)
	if err := common.DownloadModel(model, common.PrintProgress); err != nil {
		fatalf("%v", err)
	}

	fmt.Println()
	fmt.Printf("✓ Downloaded to %s/\n", modelDir)
	fmt.Println()
	printConfigs(model, modelDir)
}

func printConfigs(model *ModelInfo, modelDir string) {
	configs := common.ConfigsForModel(model.Name)
	if len(configs) == 0 {
		fmt.Println("No configurations registered for this model.")
		return
	}

	fmt.Printf("Available configurations (%d):\n", len(configs))
	for _, cfg := range configs {
		poolStr := "search.PoolingMean"
		if cfg.Pooling == common.PoolingCLS {
			poolStr = "search.PoolingCLS"
		}
		postProcessStr := "nil"
		if cfg.PostProcess != nil {
			postProcessStr = "search.LayerNorm // or custom func"
		}

		fmt.Printf("\n  --- %s ---\n", cfg.ConfigName)
		fmt.Printf("  %s\n\n", cfg.Description)
		fmt.Println("  search.EmbedderConfig{")
		fmt.Printf("      ModelPath:         %q,\n", filepath.Join(modelDir, model.ModelFile))
		fmt.Printf("      TokenizerPath:     %q,\n", filepath.Join(modelDir, "tokenizer.json"))
		fmt.Printf("      Dimensions:        %d,\n", cfg.Dimensions)
		fmt.Printf("      Pooling:           %s,\n", poolStr)
		fmt.Printf("      MaxSequenceLength: %d,\n", cfg.Model.MaxTokenChunks)
		fmt.Printf("      QueryPrefix:       %q,\n", cfg.QueryPrefix)
		fmt.Printf("      PassagePrefix:     %q,\n", cfg.PassagePrefix)
		fmt.Printf("      InputNames:        []string{%s},\n", formatStringSlice(cfg.InputNames))
		fmt.Printf("      OutputName:        %q,\n", cfg.OutputName)
		fmt.Printf("      PostProcess:       %s,\n", postProcessStr)
		fmt.Println("  }")
	}
}

func modelDownloadDir(name string) (string, error) { return common.ModelDir(name) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func formatStringSlice(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}

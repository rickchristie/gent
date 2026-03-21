package common

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DownloadModel downloads a model's ONNX file and tokenizer to its standard directory
// (~/.gent/models/<name>/). Skips files that already exist. The onProgress callback is
// called during the model download with bytes written and total bytes (0 if unknown).
// Pass nil for silent downloads.
func DownloadModel(model *ModelInfo, onProgress func(written, total int64)) error {
	dir, err := ModelDir(model.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	modelPath := filepath.Join(dir, model.ModelFile)
	tokenizerPath := filepath.Join(dir, "tokenizer.json")

	if !fileExists(modelPath) {
		if err := downloadFile(model.ModelURL, modelPath, onProgress); err != nil {
			os.Remove(modelPath)
			return fmt.Errorf("model download failed: %w", err)
		}
	}
	if !fileExists(tokenizerPath) {
		if err := downloadFile(model.TokenizerURL, tokenizerPath, nil); err != nil {
			os.Remove(tokenizerPath)
			return fmt.Errorf("tokenizer download failed: %w", err)
		}
	}
	return nil
}

// ModelDownloaded returns true if both model and tokenizer files exist for the given model.
func ModelDownloaded(model *ModelInfo) bool {
	dir, err := ModelDir(model.Name)
	if err != nil {
		return false
	}
	return fileExists(filepath.Join(dir, model.ModelFile)) &&
		fileExists(filepath.Join(dir, "tokenizer.json"))
}

func downloadFile(url, destPath string, onProgress func(written, total int64)) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if onProgress == nil {
		_, err = io.Copy(f, resp.Body)
		return err
	}

	total := resp.ContentLength
	written := int64(0)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return fmt.Errorf("write failed: %w", err)
			}
			written += int64(n)
			onProgress(written, total)
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read failed: %w", readErr)
		}
	}
}

// PrintProgress is a progress callback that prints a progress bar to stdout.
func PrintProgress(written, total int64) {
	if total <= 0 {
		return
	}
	pct := float64(written) / float64(total)
	barWidth := 32
	filled := int(pct * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r  %s %dMB/%dMB", bar, written/(1024*1024), total/(1024*1024))
	if written >= total {
		fmt.Println()
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

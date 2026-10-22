package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadModel downloads a model's ONNX file and tokenizer to its standard directory
// (~/.gent/models/<name>/). Skips files that already exist. The onProgress callback is
// called during the model download with bytes written and total bytes (0 if unknown).
// Pass nil for silent downloads. Each file has a 30-minute deadline and is published only after
// the complete response is written and closed.
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
			return fmt.Errorf("model download failed: %w", err)
		}
	}
	if !fileExists(tokenizerPath) {
		if err := downloadFile(model.TokenizerURL, tokenizerPath, nil); err != nil {
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
	// Setup must end even if a server stops sending data. Keep partial files private so another
	// caller cannot load them or remove a complete file published by a concurrent download.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return downloadFileContext(ctx, url, destPath, onProgress)
}

func downloadFileContext(ctx context.Context, url, destPath string,
	onProgress func(written, total int64),
) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp(filepath.Dir(destPath), ".gent-download-*")
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		// Progress callbacks are application code; release the file even if a callback panics.
		if !closed {
			err = errors.Join(err, f.Close())
		}
		if removeErr := os.Remove(f.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	copyErr := copyDownload(f, resp, onProgress)
	closeErr := f.Close()
	closed = true
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), destPath)
}

func copyDownload(f io.Writer, resp *http.Response, onProgress func(written, total int64)) error {
	if onProgress == nil {
		_, err := io.Copy(f, resp.Body)
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

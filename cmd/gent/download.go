package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DownloadAndExtract downloads a .tar.gz archive, extracts matching files to destDir.
func DownloadAndExtract(url, destDir string, wantFiles []string) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gzip reader failed: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	want := make(map[string]bool, len(wantFiles))
	for _, f := range wantFiles {
		want[f] = true
	}
	var extracted []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read failed: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		matched := false
		for w := range want {
			if header.Name == w || strings.HasSuffix(header.Name, "/"+filepath.Base(w)) ||
				header.Name == filepath.Base(w) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		destPath := filepath.Join(destDir, filepath.Base(header.Name))
		if err := extractFile(tr, destPath, header.Mode); err != nil {
			return nil, fmt.Errorf("extract %s: %w", header.Name, err)
		}
		extracted = append(extracted, destPath)
	}
	if len(extracted) == 0 {
		return nil, fmt.Errorf("no matching files found in archive from %s", url)
	}
	return extracted, nil
}

// DownloadFileWithProgress downloads a file showing a progress bar.
func DownloadFileWithProgress(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	totalBytes := resp.ContentLength
	written := int64(0)
	buf := make([]byte, 32*1024)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return fmt.Errorf("write failed: %w", err)
			}
			written += int64(n)
			if totalBytes > 0 {
				printProgress(written, totalBytes)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read failed: %w", readErr)
		}
	}
	fmt.Println() // newline after progress bar
	return nil
}

func printProgress(current, total int64) {
	pct := float64(current) / float64(total)
	barWidth := 32
	filled := int(pct * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r  %s %dMB/%dMB", bar, current/(1024*1024), total/(1024*1024))
}

// SHA256File computes the SHA256 hash of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractFile(r io.Reader, destPath string, mode int64) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

package common

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDownloadFilePublishesOnlyCompleteData(t *testing.T) {
	type input struct {
		body     string
		length   string
		progress bool
	}
	type expected struct {
		content string
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	tests := []testCase{
		{"complete", input{"new model", "9", false}, expected{"new model", nil}},
		{"complete with progress", input{"new model", "9", true}, expected{"new model", nil}},
		{"partial", input{"part", "9", false}, expected{"old model", io.ErrUnexpectedEOF}},
		{"partial with progress", input{"part", "9", true},
			expected{"old model", io.ErrUnexpectedEOF}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "model.onnx")
			if !assert.NoError(t, os.WriteFile(path, []byte("old model"), 0o600)) {
				return
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", tc.input.length)
				_, err := io.WriteString(w, tc.input.body)
				assert.NoError(t, err)
			}))
			defer server.Close()
			var progress func(int64, int64)
			var lastWritten int64
			if tc.input.progress {
				progress = func(written, total int64) {
					assert.Greater(t, written, lastWritten)
					lastWritten = written
					assert.Equal(t, int64(9), total)
					// The previous complete file stays available until copying has finished.
					data, err := os.ReadFile(path)
					assert.NoError(t, err)
					assert.Equal(t, "old model", string(data))
				}
			}
			err := downloadFile(server.URL, path, progress)
			assert.ErrorIs(t, err, tc.expected.err)
			if tc.input.progress {
				assert.Equal(t, int64(len(tc.input.body)), lastWritten)
			}
			data, err := os.ReadFile(path)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected.content, string(data))
			files, err := filepath.Glob(filepath.Join(dir, "*"))
			assert.NoError(t, err)
			assert.Equal(t, []string{path}, files)
		})
	}
}

func TestDownloadFileCancelsBlockedRead(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		done <- downloadFileContext(ctx, server.URL, filepath.Join(dir, "model.onnx"), nil)
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		assert.True(t, errors.Is(err, context.Canceled), "%v", err)
	case <-time.After(time.Second):
		t.Fatal("download did not stop after cancellation")
	}
	files, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Empty(t, files)
}

//go:build !cgo

package search

import "fmt"

// NewOnnxEmbedder returns an error when CGo is not available. The ONNX-based embedder requires
// CGo for ONNX Runtime and the Rust tokenizer. Build with CGO_ENABLED=1 or provide a custom
// Embedder implementation.
func NewOnnxEmbedder(_ EmbedderConfig) (Embedder, error) {
	return nil, fmt.Errorf(
		"search: ONNX embedder requires CGo; build with CGO_ENABLED=1 " +
			"or provide a custom Embedder implementation")
}

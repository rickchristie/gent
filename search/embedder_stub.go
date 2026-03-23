//go:build !cgo

package search

import (
	"fmt"

	"github.com/rickchristie/gent/common"
)

// NewOnnxEmbedder returns an error when CGo is not available.
func NewOnnxEmbedder(_ common.ModelConfig, _ OnnxOptions) (Embedder, error) {
	return nil, fmt.Errorf(
		"search: ONNX embedder requires CGo; build with CGO_ENABLED=1 " +
			"or provide a custom Embedder implementation")
}

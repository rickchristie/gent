package toolchain

import (
	"github.com/rickchristie/gent/search"
)

const defaultBM25Guidance = "Use natural language queries for full-text search across tool " +
	"names, descriptions, keywords, and categories. Examples: \"order status\", " +
	"\"send notification to customer\", \"billing payment\""

// NewBleveToolSearcher creates a BM25-based ToolSearcher backed by search.BleveIndex with
// the ToolBleveAdapter. This is a convenience constructor — it wires BleveIndex + ToolBleveAdapter
// + IndexToolSearcher together with sensible defaults.
//
// The returned searcher uses field-specific boosting (exact name 10x, keywords 3x, fuzzy
// name 2x, synthetic queries 1.5x, description 1x) as configured in ToolBleveAdapter.
func NewBleveToolSearcher() *IndexToolSearcher {
	// Disable confidence threshold: standalone BM25 is the only retrieval path here
	// (no semantic fallback). Gating would produce zero results for legitimate queries
	// with small corpora where raw BM25 scores are naturally low.
	bleveIdx, err := search.NewBleveIndex(&ToolBleveAdapter{},
		search.WithTheoreticalMaxConfidenceThreshold(0),
	)
	if err != nil {
		// BleveIndex with NewMemOnly should never fail — this would be a programming error.
		panic("toolchain: failed to create BleveIndex: " + err.Error())
	}
	return NewIndexToolSearcher("bm25", bleveIdx).
		WithSearchGuidance(defaultBM25Guidance)
}

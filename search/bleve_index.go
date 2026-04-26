package search

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	blevesearch "github.com/blevesearch/bleve/v2/search"
)

// BleveOption configures a [BleveIndex].
type BleveOption func(*bleveConfig)

type bleveConfig struct {
	theoreticalMaxNormalization       bool
	theoreticalMaxConfidenceThreshold float64
	kneeTruncation                    bool
}

func defaultBleveConfig() bleveConfig {
	return bleveConfig{
		theoreticalMaxNormalization:       true,
		theoreticalMaxConfidenceThreshold: 0.001,
		kneeTruncation:                    true,
	}
}

// WithTheoreticalMaxNormalization controls whether BleveIndex computes the theoretical
// maximum BM25 score for each query and attaches it to results as metadata. When enabled
// (the default) and the adapter implements [BleveIDFProvider], the Fuser can normalize BM25
// scores against what's theoretically achievable rather than the observed score range,
// preventing noise amplification when all BM25 scores are near-zero.
func WithTheoreticalMaxNormalization(enabled bool) BleveOption {
	return func(c *bleveConfig) { c.theoreticalMaxNormalization = enabled }
}

// WithTheoreticalMaxConfidenceThreshold sets the minimum BM25 score as a fraction of
// the theoretical maximum. Documents scoring below this fraction are removed from
// BleveIndex search results before they reach the Fuser. Default: 0.001 (0.1%).
//
// # How it works
//
// The theoretical maximum is the highest BM25 score a hypothetically perfect document
// could achieve for this query — one where every query term appears at maximum frequency
// in every searched field. The confidence threshold filters against this ceiling:
//
//	keep document if:  raw_score >= threshold × max_possible
//
// With the default threshold of 0.001, a document must score at least 0.1% of what a
// perfect document would score to survive. Documents below this threshold are considered
// noise — incidental matches on common terms that happen to appear in the query.
//
// Set to 0 to disable the confidence filter (keep all BM25 results regardless of score).
//
// # Tuning guide
//
// The right threshold depends on your corpus size, field structure, and query patterns.
// The theoretical maximum is a loose upper bound — it sums contributions across all
// searched fields and assumes infinite term frequency — so real documents typically score
// 1-30% of the maximum even for good matches. To tune:
//
//  1. Run your score analysis test (e.g., index_test.go) with this threshold set to 0
//  2. For each query, note the BM25 raw score and the theoretical max (from metadata)
//  3. Compute ratio = raw_score / theoretical_max for each result
//  4. Identify the ratio boundary between results you want to keep vs discard
//  5. Set the threshold to that boundary
//
// A threshold that is too high filters legitimate BM25 matches (keyword queries stop
// contributing to fusion). A threshold that is too low lets noise through (common-word
// matches inflate fused scores). When in doubt, start low (0.01) and increase until
// noise queries return empty BM25 results while keyword queries still return matches.
//
// Requires [WithTheoreticalMaxNormalization] to be enabled and the adapter to implement
// [BleveIDFProvider]. Has no effect otherwise.
func WithTheoreticalMaxConfidenceThreshold(threshold float64) BleveOption {
	return func(c *bleveConfig) { c.theoreticalMaxConfidenceThreshold = threshold }
}

// WithKneeTruncation controls whether BleveIndex applies Kneedle-based knee detection
// to search results. When enabled (the default), results below the knee point — where
// scores transition from meaningful matches to noise — are removed. This uses the
// Kneedle algorithm (Satopaa et al., 2011) with sensitivity S=1.0.
//
// Knee truncation runs after the confidence threshold filter, operating only on results
// that already passed the minimum score gate. It detects the natural breakpoint in the
// score distribution rather than using a fixed cutoff.
func WithKneeTruncation(enabled bool) BleveOption {
	return func(c *bleveConfig) { c.kneeTruncation = enabled }
}

// BleveIndex provides BM25 full-text search via Bleve.
//
// Documents are converted to Bleve's format via a [BleveAdapter], which controls the index
// schema, document conversion, and query construction. BleveIndex handles the Bleve lifecycle,
// concurrency, and conversion of Bleve results to [SearchResult].
//
// # Snippet Generation
//
// BleveIndex attempts to extract a highlighted snippet from Bleve's search results. If no
// highlights are available (e.g., the query or mapping doesn't support highlighting), the
// document ID is returned as the snippet fallback.
//
// Safe for concurrent use via sync.RWMutex (wrapping Bleve's own thread safety for Swap).
type BleveIndex[Doc any] struct {
	adapter BleveAdapter[Doc]
	index   bleve.Index
	config  bleveConfig
	mu      sync.RWMutex // protects index replacement in Swap
}

// NewBleveIndex creates a BleveIndex with the given adapter. The adapter's Mapping() is used
// to initialize the in-memory Bleve index. Options configure normalization behavior; see
// [WithTheoreticalMaxNormalization].
func NewBleveIndex[Doc any](
	adapter BleveAdapter[Doc], opts ...BleveOption,
) (*BleveIndex[Doc], error) {
	cfg := defaultBleveConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	idx, err := bleve.NewMemOnly(adapter.Mapping())
	if err != nil {
		return nil, fmt.Errorf("search: bleve index creation failed: %w", err)
	}
	return &BleveIndex[Doc]{adapter: adapter, index: idx, config: cfg}, nil
}

// Search returns the top-K most relevant documents for the query. The adapter's Query() builds
// the Bleve query with field-specific boosting; BleveIndex controls the result count via topK.
//
// If the adapter implements [BleveIDFProvider], each result's Metadata includes the theoretical
// maximum BM25 score under [TheoreticalMaxKey]. This enables the Fuser to normalize BM25
// scores against what's theoretically achievable rather than the observed score range.
func (b *BleveIndex[Doc]) Search(
	ctx context.Context, queryText string, topK int,
) ([]SearchResult, error) {
	q, err := b.adapter.Query(queryText)
	if err != nil {
		return nil, fmt.Errorf("search: bleve query build failed: %w", err)
	}

	req := bleve.NewSearchRequestOptions(q, topK, 0, false)
	req.Highlight = bleve.NewHighlight()

	var theoreticalMax float64
	b.mu.RLock()
	bleveResults, err := b.index.Search(req)
	if err == nil && b.config.theoreticalMaxNormalization {
		if provider, ok := b.adapter.(BleveIDFProvider); ok {
			theoreticalMax = b.computeTheoreticalMaxLocked(ctx, queryText, provider)
		}
	}
	b.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("search: bleve search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(bleveResults.Hits))
	for _, hit := range bleveResults.Hits {
		snippet := extractSnippet(hit)
		r := SearchResult{Id: hit.ID, Score: hit.Score, Snippet: snippet}
		if theoreticalMax > 0 {
			r.Metadata = map[string]any{TheoreticalMaxKey: theoreticalMax}
		}
		results = append(results, r)
	}

	// Confidence filter: remove results scoring below threshold × max_possible.
	if theoreticalMax > 0 && b.config.theoreticalMaxConfidenceThreshold > 0 {
		minScore := b.config.theoreticalMaxConfidenceThreshold * theoreticalMax
		n := 0
		for _, r := range results {
			if r.Score >= minScore {
				results[n] = r
				n++
			}
		}
		results = results[:n]
	}

	// Knee truncation: remove results below the natural breakpoint in score distribution.
	if b.config.kneeTruncation {
		results = KneeTruncate(results, 1.0)
	}

	return results, nil
}

// Add indexes a single document. The adapter's Convert() transforms the domain document into
// Bleve's format. If a document with the same ID exists, Bleve replaces it.
func (b *BleveIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
	bleveDoc, err := b.adapter.Convert(doc)
	if err != nil {
		return fmt.Errorf("search: bleve adapter convert failed: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.index.Index(id, bleveDoc)
}

// Remove deletes a document by ID.
func (b *BleveIndex[Doc]) Remove(id string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.index.Delete(id)
}

// Swap atomically replaces the entire index. A new Bleve index is built from the provided
// documents, then swapped in. The old index is closed.
func (b *BleveIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
	newIdx, err := bleve.NewMemOnly(b.adapter.Mapping())
	if err != nil {
		return fmt.Errorf("search: bleve index creation failed: %w", err)
	}

	for id, doc := range docs {
		bleveDoc, err := b.adapter.Convert(doc)
		if err != nil {
			newIdx.Close()
			return fmt.Errorf("search: bleve adapter convert failed for %s: %w", id, err)
		}
		if err := newIdx.Index(id, bleveDoc); err != nil {
			newIdx.Close()
			return fmt.Errorf("search: bleve index failed for %s: %w", id, err)
		}
	}

	b.mu.Lock()
	old := b.index
	b.index = newIdx
	b.mu.Unlock()

	return old.Close()
}

// Close closes the underlying Bleve index.
func (b *BleveIndex[Doc]) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.index.Close()
}

// BM25 constants matching Bleve's defaults (search/util.go).
const (
	bm25K1 = 1.2
)

// computeTheoreticalMax computes the theoretical maximum BM25 score for the query:
//
//	max_possible = Σ_fields(boost × Σ_terms(IDF(t) × (k₁ + 1)))
//
// where IDF uses Bleve's BM25 formula: ln(1 + (N - df + 0.5) / (df + 0.5)).
//
// Terms with zero doc frequency in ALL fields are excluded (they can't contribute
// to any document's actual score). Returns 0 if the analyzer produces no terms
// or all terms are absent from the index.
// computeTheoreticalMaxLocked reads Bleve internals; callers must hold b.mu.RLock so Swap
// cannot replace and close the index while the reader is being opened.
func (b *BleveIndex[Doc]) computeTheoreticalMaxLocked(
	ctx context.Context, queryText string, provider BleveIDFProvider,
) float64 {
	analyzerName, fields := provider.IDFFields()
	if len(fields) == 0 {
		return 0
	}

	// Get the analyzer from the index mapping.
	indexMapping, ok := b.index.Mapping().(*mapping.IndexMappingImpl)
	if !ok {
		return 0
	}
	analyzer := indexMapping.AnalyzerNamed(analyzerName)
	if analyzer == nil {
		return 0
	}

	// Tokenize the query with the same analyzer the index uses.
	tokens := analyzer.Analyze([]byte(queryText))
	if len(tokens) == 0 {
		return 0
	}

	// Deduplicate terms.
	termSet := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		termSet[string(tok.Term)] = struct{}{}
	}

	// Get index reader for doc frequency lookups.
	advanced, err := b.index.Advanced()
	if err != nil {
		return 0
	}
	reader, err := advanced.Reader()
	if err != nil {
		return 0
	}
	defer reader.Close()

	totalDocs, err := reader.DocCount()
	if err != nil || totalDocs == 0 {
		return 0
	}

	// For each term, sum IDF contributions across all fields (DisjunctionQuery sums
	// scores from matching sub-queries).
	var maxPossible float64
	n := float64(totalDocs)

	for term := range termSet {
		var termContrib float64
		for _, field := range fields {
			tfr, err := reader.TermFieldReader(
				ctx, []byte(term), field.Field, false, false, false,
			)
			if err != nil {
				continue
			}
			df := tfr.Count()
			tfr.Close()

			if df == 0 {
				continue
			}

			// BM25 IDF: ln(1 + (N - df + 0.5) / (df + 0.5))
			idf := math.Log(
				1.0 + (n-float64(df)+0.5)/(float64(df)+0.5),
			)
			termContrib += field.Boost * idf * (bm25K1 + 1)
		}
		maxPossible += termContrib
	}
	return maxPossible
}

// extractSnippet returns the first highlighted fragment from Bleve results, or the document
// ID as fallback when no highlights are available.
func extractSnippet(hit *blevesearch.DocumentMatch) string {
	for _, fragments := range hit.Fragments {
		for _, fragment := range fragments {
			trimmed := strings.TrimSpace(fragment)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return hit.ID
}

// Compile-time check.
var _ SearchIndex[any] = (*BleveIndex[any])(nil)

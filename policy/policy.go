// Package policy provides a reusable PolicySearchTool backed by hybrid BM25 + semantic search.
//
// Users define policies as [Policy] structs with Markdown content, then create a
// [PolicySearchTool] that agents can call to discover relevant policies by natural language
// queries. The tool uses [search.FusedIndex] with 40% BM25 + 60% semantic search for
// high-quality policy retrieval.
//
// # Usage
//
//	policies := []*policy.Policy{
//	    {
//	        Id:          "cancellation-refund",
//	        FullContent: "## Cancellation Policy\n\nCustomers may cancel...",
//	        Keywords:    []string{"cancel", "refund"},
//	        SyntheticQueries: []string{"customer wants to cancel"},
//	    },
//	}
//	tool, err := policy.NewPolicySearchTool(ctx, embedder, policies)
//	tc.RegisterTool(tool)
//
// # Policy Design Guidelines
//
// Each Policy should be short and atomic — one topic per struct. Long monolithic policies
// get chunked during indexing, and information buried in the middle of long chunks suffers
// from the "lost in the middle" problem where embedding models and LLMs attend less strongly
// to content far from chunk boundaries.
//
// Recommended: 150-300 words per policy in Markdown format. Use headings for structure. If a
// topic needs more than 300 words, split it into multiple policies.
//
// FullContent must be Markdown. The [search.MarkdownChunker] uses headings as section
// boundaries and prepends heading ancestors for context when chunking. Policies without
// Markdown headings still work but get less structured chunking.
//
// # Table Preprocessing
//
// If your policies contain tables, convert them to natural language sentences before
// setting FullContent. See [search.MarkdownChunker] documentation for details.
package policy

import (
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/rickchristie/gent/search"
)

// Policy represents a searchable policy document. Each policy should cover one topic
// (atomic policies). See package documentation for design guidelines.
type Policy struct {
	// Id is a human-readable unique identifier for the policy. Use lowercase with hyphens
	// (e.g., "cancellation-refund", "baggage-allowance"). This is used as a search key
	// (exact match gets highest BM25 boost) and displayed in search results.
	Id string

	// FullContent is the policy text in Markdown format. Keep it short and atomic — one topic
	// per policy, 150-300 words recommended. Use headings for structure. The MarkdownChunker
	// will split long content at heading boundaries with ancestor context.
	FullContent string

	// Keywords are BM25 search terms that help keyword-based search find this policy. Include
	// synonyms and abbreviations the agent might use. Example: for a cancellation policy,
	// include "cancel", "refund", "credit", "cancellation".
	Keywords []string

	// SyntheticQueries are natural language phrases that should match this policy in semantic
	// search. These represent how an agent or user would describe the need for this policy.
	// Example: "customer wants to cancel their booking", "how to process a refund".
	SyntheticQueries []string
}

// PolicyBleveAdapter implements [search.BleveAdapter] for [*Policy]. It indexes policies
// with field-specific boosting optimized for policy search:
//   - id: keyword (exact match, boost 10.0) — searching by policy ID gets highest priority
//   - keywords: text (analyzed, boost 3.0) — domain-specific search terms
//   - fullcontent: text (analyzed, boost 1.0) — full-text search over policy body
type PolicyBleveAdapter struct{}

func (a *PolicyBleveAdapter) Mapping() mapping.IndexMapping {
	keyword := bleve.NewKeywordFieldMapping()
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"
	text.Store = true

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("id", keyword)
	docMapping.AddFieldMappingsAt("keywords", text)
	docMapping.AddFieldMappingsAt("fullcontent", text)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *PolicyBleveAdapter) Convert(p *Policy) (any, error) {
	return map[string]string{
		"id":          p.Id,
		"keywords":    strings.Join(p.Keywords, " "),
		"fullcontent": p.FullContent,
	}, nil
}

func (a *PolicyBleveAdapter) IDFFields() (string, []search.IDFField) {
	return "standard", []search.IDFField{
		{Field: "keywords", Boost: 3.0},
		{Field: "fullcontent", Boost: 1.0},
	}
}

func (a *PolicyBleveAdapter) Query(queryText string) (query.Query, error) {
	idMatch := bleve.NewMatchQuery(queryText)
	idMatch.SetField("id")
	idMatch.SetBoost(10.0)

	keywordsMatch := bleve.NewMatchQuery(queryText)
	keywordsMatch.SetField("keywords")
	keywordsMatch.SetBoost(3.0)

	contentMatch := bleve.NewMatchQuery(queryText)
	contentMatch.SetField("fullcontent")
	contentMatch.SetBoost(1.0)

	disj := bleve.NewDisjunctionQuery(idMatch, keywordsMatch, contentMatch)
	disj.SetMin(1)
	return disj, nil
}

// PolicyChunkAdapter implements [search.ChunkAdapter] for [*Policy]. It converts policies
// into chunks for semantic embedding:
//  1. Primary content: prepends "# {Id}" heading to FullContent, then uses MarkdownChunker
//  2. Synthetic queries: creates an additional chunk from SyntheticQueries joined by newline,
//     enabling semantic search to match intent-phrased queries
type PolicyChunkAdapter struct{}

func (a *PolicyChunkAdapter) Chunks(
	p *Policy, tc search.TokenCounter, maxTokens int,
) ([]search.Chunk, error) {
	tokenCount := func(s string) int { return len(s) / 4 }
	if tc != nil {
		tokenCount = tc.TokenCount
	}
	if maxTokens == 0 {
		maxTokens = 512
	}

	// Primary content with policy ID as heading.
	primary := fmt.Sprintf("# %s\n\n%s", p.Id, p.FullContent)
	chunker := &search.MarkdownChunker{ChunkSize: maxTokens, TokenCount: tokenCount}
	chunks := chunker.Chunk(primary)

	// Additional chunk for synthetic queries — helps semantic search match on
	// intent-phrased queries independently from policy body text.
	if len(p.SyntheticQueries) > 0 {
		sqText := fmt.Sprintf("# %s\n\n%s", p.Id, strings.Join(p.SyntheticQueries, "\n"))
		chunks = append(chunks, search.Chunk{
			Text:     sqText,
			Metadata: map[string]string{"h1": p.Id, "type": "synthetic_queries"},
		})
	}

	return chunks, nil
}

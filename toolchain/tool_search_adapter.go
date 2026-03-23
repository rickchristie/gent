package toolchain

import (
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/search"
)

// ToolBleveAdapter implements [search.BleveAdapter] for [gent.IndexableTool]. It bridges
// IndexableTool metadata into Bleve's index mapping and query system with field-specific
// boosting optimized for tool search.
//
// The name is indexed twice: once as keyword (exact match, boost 10x) and once as analyzed
// text (partial matching, "billing" matches "get_billing_ledger").
//
// # Boost Hierarchy
//
//   - Exact name match (10.0) — overwhelmingly highest BM25 score
//   - Keywords match (3.0) — tool-registered keywords
//   - Fuzzy name match (2.0, fuzziness 1) — partial name matches
//   - Categories match (2.0) — tool classification categories
//   - Domain match (1.5) — high-level domain grouping
//   - Synthetic queries match (1.5) — natural language intent
//   - Description match (1.0) — general topic overlap
type ToolBleveAdapter struct{}

func (a *ToolBleveAdapter) Mapping() mapping.IndexMapping {
	keyword := bleve.NewKeywordFieldMapping()
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("name", keyword)
	docMapping.AddFieldMappingsAt("name_analyzed", text)
	docMapping.AddFieldMappingsAt("domain", text)
	docMapping.AddFieldMappingsAt("categories", text)
	docMapping.AddFieldMappingsAt("keywords", text)
	docMapping.AddFieldMappingsAt("description", text)
	docMapping.AddFieldMappingsAt("synthetic_queries", text)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *ToolBleveAdapter) Convert(tool gent.IndexableTool) (any, error) {
	return map[string]string{
		"name":              tool.Name(),
		"name_analyzed":     tool.Name(),
		"domain":            tool.Domain(),
		"categories":        strings.Join(tool.Categories(), " "),
		"keywords":          strings.Join(tool.Keywords(), " "),
		"description":       tool.Description(),
		"synthetic_queries": strings.Join(tool.SyntheticQueries(), " "),
	}, nil
}

func (a *ToolBleveAdapter) IDFFields() (string, []search.IDFField) {
	return "standard", []search.IDFField{
		{Field: "keywords", Boost: 3.0},
		{Field: "name_analyzed", Boost: 2.0},
		{Field: "categories", Boost: 2.0},
		{Field: "domain", Boost: 1.5},
		{Field: "synthetic_queries", Boost: 1.5},
		{Field: "description", Boost: 1.0},
	}
}

func (a *ToolBleveAdapter) Query(queryText string) (query.Query, error) {
	exactName := bleve.NewMatchQuery(queryText)
	exactName.SetField("name")
	exactName.SetBoost(10.0)

	keywordsMatch := bleve.NewMatchQuery(queryText)
	keywordsMatch.SetField("keywords")
	keywordsMatch.SetBoost(3.0)

	fuzzyName := bleve.NewFuzzyQuery(queryText)
	fuzzyName.SetField("name_analyzed")
	fuzzyName.SetBoost(2.0)
	fuzzyName.SetFuzziness(1)

	syntheticMatch := bleve.NewMatchQuery(queryText)
	syntheticMatch.SetField("synthetic_queries")
	syntheticMatch.SetBoost(1.5)

	categoriesMatch := bleve.NewMatchQuery(queryText)
	categoriesMatch.SetField("categories")
	categoriesMatch.SetBoost(2.0)

	domainMatch := bleve.NewMatchQuery(queryText)
	domainMatch.SetField("domain")
	domainMatch.SetBoost(1.5)

	descMatch := bleve.NewMatchQuery(queryText)
	descMatch.SetField("description")
	descMatch.SetBoost(1.0)

	disj := bleve.NewDisjunctionQuery(
		exactName, keywordsMatch, fuzzyName, categoriesMatch, domainMatch,
		syntheticMatch, descMatch,
	)
	disj.SetMin(1)
	return disj, nil
}

// ToolChunkAdapter implements [search.ChunkAdapter] for [gent.IndexableTool]. It converts
// tool metadata into a Markdown-formatted string and splits it using [search.MarkdownChunker]
// for token-aware chunking.
//
// The output format uses the tool name as a heading so the MarkdownChunker can use it as a
// section boundary if needed:
//
//	# get_billing_ledger
//
//	Retrieve billing ledger entries and payment invoices for a customer.
//
//	- Domain: Billing
//	- Categories: lookup, billing
//	- Keywords: billing, payment, invoice, ledger
//	- Example queries: check payment status; look up invoices
//
// Tool descriptions are typically 50-100 tokens — well within any model's limit — so the
// chunker usually returns a single chunk. However, users can provide arbitrarily long
// descriptions, keywords, or synthetic queries, so the chunker ensures no chunk exceeds
// the model's maximum sequence length.
type ToolChunkAdapter struct{}

func (a *ToolChunkAdapter) Chunks(
	tool gent.IndexableTool, tc search.TokenCounter, maxTokens int,
) ([]search.Chunk, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n%s\n", tool.Name(), tool.Description())
	if domain := tool.Domain(); domain != "" {
		fmt.Fprintf(&sb, "\n- Domain: %s", domain)
	}
	if cats := tool.Categories(); len(cats) > 0 {
		fmt.Fprintf(&sb, "\n- Categories: %s", strings.Join(cats, ", "))
	}
	if kw := tool.Keywords(); len(kw) > 0 {
		fmt.Fprintf(&sb, "\n- Keywords: %s", strings.Join(kw, ", "))
	}
	if sq := tool.SyntheticQueries(); len(sq) > 0 {
		fmt.Fprintf(&sb, "\n- Example queries: %s", strings.Join(sq, "; "))
	}
	text := sb.String()
	chunker := &search.MarkdownChunker{
		ChunkSize: maxTokens, TokenCount: tc.TokenCount,
	}
	return chunker.Chunk(text), nil
}

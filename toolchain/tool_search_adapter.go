package toolchain

import (
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/rickchristie/gent"
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
	docMapping.AddFieldMappingsAt("domain", keyword)
	docMapping.AddFieldMappingsAt("categories", keyword)
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

	descMatch := bleve.NewMatchQuery(queryText)
	descMatch.SetField("description")
	descMatch.SetBoost(1.0)

	disj := bleve.NewDisjunctionQuery(exactName, keywordsMatch, fuzzyName, syntheticMatch, descMatch)
	disj.SetMin(1)
	return disj, nil
}

// ToolChunkAdapter implements [search.ChunkAdapter] for [gent.IndexableTool]. It converts
// tool metadata into a single text chunk for semantic embedding. The chunk concatenates the
// most semantically meaningful fields to give the embedding model maximum signal.
type ToolChunkAdapter struct{}

func (a *ToolChunkAdapter) Convert(tool gent.IndexableTool) ([]string, error) {
	text := fmt.Sprintf("%s: %s\nKeywords: %s\nExample queries: %s",
		tool.Name(), tool.Description(),
		strings.Join(tool.Keywords(), ", "),
		strings.Join(tool.SyntheticQueries(), "; "),
	)
	return []string{text}, nil
}

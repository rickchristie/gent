package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/search"
)

// PolicySearchInput is the input to the PolicySearchTool.
type PolicySearchInput struct {
	Query string `json:"query"`
}

// PolicySearchTool is a tool that searches policies using hybrid BM25 + semantic search.
// It implements [gent.Tool] with string output (not JSON-wrapped) so that policy content
// renders as Markdown in the agent's observation, not as escaped JSON.
//
// # Fusion Weights (40% BM25, 60% Semantic)
//
// Policy search uses a 40/60 BM25/semantic split (vs 30/70 for tool search) because policies
// have higher keyword overlap with natural language queries — policy titles and content use
// formal, findable terms like "cancellation", "refund", "baggage". BM25 contributes more
// signal for policies than for tool search where vocabulary mismatch is more severe.
//
// # Search Flow
//
//  1. Agent calls search_policy with a natural language query
//  2. FusedIndex fans out to BM25 (Bleve) and semantic (FlatIndex via ONNX embedder)
//  3. BM25 scores are normalized against theoretical max; semantic scores pass through
//  4. Weighted linear combination produces fused ranking
//  5. Top-N matching policies are returned as Markdown text (not JSON)
//
// # Usage
//
//	tool, err := policy.NewPolicySearchTool(ctx, embedder, policies)
//	tc.RegisterTool(tool)
type PolicySearchTool struct {
	name        string
	description string
	index       search.SearchIndex[*Policy]
	policies    map[string]*Policy
	topK        int

	// IndexableTool metadata for SearchJSON compatibility.
	keywords         []string
	syntheticQueries []string
}

// NewPolicySearchTool creates a PolicySearchTool backed by FusedIndex with 40% BM25 + 60%
// semantic search. The embedder must be initialized. All policies are indexed immediately.
func NewPolicySearchTool(
	ctx context.Context, embedder search.Embedder, policies []*Policy,
) (*PolicySearchTool, error) {
	bleveIdx, err := search.NewBleveIndex(&PolicyBleveAdapter{})
	if err != nil {
		return nil, fmt.Errorf("policy: failed to create BleveIndex: %w", err)
	}

	flatIdx := search.NewFlatIndex(&PolicyChunkAdapter{}, embedder)

	fuser := &search.WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.4, "semantic": 0.6},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	fusedIdx := search.NewFusedIndex(fuser,
		map[string]search.SearchIndex[*Policy]{"bm25": bleveIdx, "semantic": flatIdx},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	docs := make(map[string]*Policy, len(policies))
	policyMap := make(map[string]*Policy, len(policies))
	for _, p := range policies {
		docs[p.Id] = p
		policyMap[p.Id] = p
	}

	if err := fusedIdx.Swap(ctx, docs); err != nil {
		return nil, fmt.Errorf("policy: failed to index policies: %w", err)
	}

	return &PolicySearchTool{
		name:        "search_policy",
		description: "Search internal policies and SOPs by describing what you need",
		index:       fusedIdx,
		policies:    policyMap,
		topK:        3,
		keywords: []string{
			"policy", "procedure", "guideline", "rules", "terms", "SOP",
		},
		syntheticQueries: []string{
			"find company policy", "search guidance procedures",
			"look up rules for this situation",
		},
	}, nil
}

// WithName sets the tool name. Default: "search_policy".
func (t *PolicySearchTool) WithName(name string) *PolicySearchTool {
	t.name = name
	return t
}

// WithDescription sets the tool description.
func (t *PolicySearchTool) WithDescription(desc string) *PolicySearchTool {
	t.description = desc
	return t
}

// WithTopK sets the maximum number of policies to return. Default: 3.
func (t *PolicySearchTool) WithTopK(topK int) *PolicySearchTool {
	t.topK = topK
	return t
}

// WithIndexableKeywords sets the keywords for IndexableTool (SearchJSON compatibility).
func (t *PolicySearchTool) WithIndexableKeywords(kw []string) *PolicySearchTool {
	t.keywords = kw
	return t
}

// WithIndexableSyntheticQueries sets synthetic queries for IndexableTool.
func (t *PolicySearchTool) WithIndexableSyntheticQueries(sq []string) *PolicySearchTool {
	t.syntheticQueries = sq
	return t
}

// --- gent.Tool[PolicySearchInput, string] implementation ---

func (t *PolicySearchTool) Name() string        { return t.name }
func (t *PolicySearchTool) Description() string  { return t.description }
func (t *PolicySearchTool) Policy() string       { return "" }
func (t *PolicySearchTool) ParameterSchema() map[string]any {
	return schema.Object(map[string]*schema.Property{
		"query": schema.String(
			"Natural language description of what policy you need. " +
				"Examples: 'cancellation refund rules', 'baggage allowance by class'"),
	}, "query")
}

func (t *PolicySearchTool) Call(
	ctx context.Context, input PolicySearchInput,
) (*gent.ToolResult[string], error) {
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	results, err := t.index.Search(ctx, input.Query, t.topK)
	if err != nil {
		return nil, fmt.Errorf("policy search failed: %w", err)
	}

	if len(results) == 0 {
		return &gent.ToolResult[string]{
			Text: "No policies found matching your query. " +
				"Try different keywords or a broader search.",
		}, nil
	}

	var sb strings.Builder
	for i, r := range results {
		p, ok := t.policies[r.Id]
		if !ok {
			continue
		}
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&sb, "# %s\n\n%s", p.Id, p.FullContent)
	}

	return &gent.ToolResult[string]{Text: sb.String()}, nil
}

// --- gent.IndexableTool implementation (for SearchJSON compatibility) ---

func (t *PolicySearchTool) Domain() string              { return "Policy & Guidance" }
func (t *PolicySearchTool) Categories() []string         { return []string{"lookup", "policy"} }
func (t *PolicySearchTool) Keywords() []string           { return t.keywords }
func (t *PolicySearchTool) SyntheticQueries() []string   { return t.syntheticQueries }

// Compile-time checks.
var _ gent.Tool[PolicySearchInput, string] = (*PolicySearchTool)(nil)
var _ gent.IndexableTool = (*PolicySearchTool)(nil)

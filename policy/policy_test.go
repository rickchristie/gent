package policy

import (
	"context"
	"testing"

	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// PolicyBleveAdapter tests
// ============================================================================

func TestPolicyBleveAdapter_Convert(t *testing.T) {
	adapter := &PolicyBleveAdapter{}
	p := &Policy{
		Id:          "cancellation-refund",
		FullContent: "Customers may cancel within 24 hours.",
		Keywords:    []string{"cancel", "refund"},
	}

	result, err := adapter.Convert(p)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"id":          "cancellation-refund",
		"keywords":    "cancel refund",
		"fullcontent": "Customers may cancel within 24 hours.",
	}, result)
}

func TestPolicyBleveAdapter_Query(t *testing.T) {
	adapter := &PolicyBleveAdapter{}
	q, err := adapter.Query("cancellation refund")
	require.NoError(t, err)
	assert.NotNil(t, q)
}

// ============================================================================
// PolicyChunkAdapter tests
// ============================================================================

func TestPolicyChunkAdapter_Chunks(t *testing.T) {
	adapter := &PolicyChunkAdapter{}
	p := &Policy{
		Id: "cancellation-refund",
		FullContent: `## Cancellation Terms

Customers may cancel within 24 hours for a full refund.`,
		Keywords:         []string{"cancel", "refund"},
		SyntheticQueries: []string{"customer wants to cancel", "how to get a refund"},
	}

	chunks, err := adapter.Chunks(p, nil, 512)
	require.NoError(t, err)
	require.Len(t, chunks, 3, "heading chunk + content chunk + synthetic queries chunk")

	// First chunk: the policy ID heading alone (MarkdownChunker emits headings as sections).
	assert.Equal(t, search.Chunk{
		Text:     "# cancellation-refund",
		Metadata: nil,
	}, chunks[0])

	// Second chunk: content with ancestor prefix.
	assert.Equal(t, search.Chunk{
		Text: `h1: cancellation-refund
## Cancellation Terms

Customers may cancel within 24 hours for a full refund.`,
		Metadata: map[string]string{"h1": "cancellation-refund"},
	}, chunks[1])

	// Third chunk: synthetic queries with policy ID heading.
	assert.Equal(t, search.Chunk{
		Text: `# cancellation-refund

customer wants to cancel
how to get a refund`,
		Metadata: map[string]string{"h1": "cancellation-refund", "type": "synthetic_queries"},
	}, chunks[2])
}

func TestPolicyChunkAdapter_NoSyntheticQueries(t *testing.T) {
	adapter := &PolicyChunkAdapter{}
	p := &Policy{
		Id:          "simple-policy",
		FullContent: "A simple policy with no synthetic queries.",
	}

	chunks, err := adapter.Chunks(p, nil, 512)
	require.NoError(t, err)
	assert.Len(t, chunks, 1, "no synthetic queries → only content chunk")
}

// ============================================================================
// PolicySearchTool.Call formatting tests
// ============================================================================

// mockPolicyIndex is a mock SearchIndex for testing Call formatting.
type mockPolicyIndex struct {
	results []search.SearchResult
}

func (m *mockPolicyIndex) Search(
	_ context.Context, _ string, _ int,
) ([]search.SearchResult, error) {
	return m.results, nil
}

func (m *mockPolicyIndex) Add(_ context.Context, _ string, _ *Policy) error { return nil }
func (m *mockPolicyIndex) Remove(_ string) error                            { return nil }
func (m *mockPolicyIndex) Swap(_ context.Context, _ map[string]*Policy) error {
	return nil
}

func TestPolicySearchTool_Call_NoResults(t *testing.T) {
	tool := &PolicySearchTool{
		name:     "search_policy",
		index:    &mockPolicyIndex{results: nil},
		policies: map[string]*Policy{},
		topK:     3,
	}

	result, err := tool.Call(context.Background(), PolicySearchInput{Query: "nonexistent"})
	require.NoError(t, err)
	assert.Equal(t,
		"No policies found matching your query. Try different keywords or a broader search.",
		result.Text)
}

func TestPolicySearchTool_Call_SingleResult(t *testing.T) {
	tool := &PolicySearchTool{
		name:  "search_policy",
		index: &mockPolicyIndex{results: []search.SearchResult{{Id: "cancel-policy", Score: 0.9}}},
		policies: map[string]*Policy{
			"cancel-policy": {
				Id: "cancel-policy",
				FullContent: `## Cancellation

Customers may cancel within 24 hours.`,
			},
		},
		topK: 3,
	}

	result, err := tool.Call(context.Background(), PolicySearchInput{Query: "cancel"})
	require.NoError(t, err)
	assert.Equal(t, `# cancel-policy

## Cancellation

Customers may cancel within 24 hours.`, result.Text)
}

func TestPolicySearchTool_Call_MultipleResults(t *testing.T) {
	tool := &PolicySearchTool{
		name: "search_policy",
		index: &mockPolicyIndex{results: []search.SearchResult{
			{Id: "cancel-policy", Score: 0.9},
			{Id: "refund-policy", Score: 0.7},
		}},
		policies: map[string]*Policy{
			"cancel-policy": {Id: "cancel-policy", FullContent: "Cancel within 24 hours."},
			"refund-policy": {Id: "refund-policy", FullContent: "Refund in 5-7 days."},
		},
		topK: 3,
	}

	result, err := tool.Call(context.Background(), PolicySearchInput{Query: "cancel refund"})
	require.NoError(t, err)
	assert.Equal(t, `# cancel-policy

Cancel within 24 hours.

---

# refund-policy

Refund in 5-7 days.`, result.Text)
}

func TestPolicySearchTool_Call_EmptyQuery(t *testing.T) {
	tool := &PolicySearchTool{
		name:     "search_policy",
		index:    &mockPolicyIndex{},
		policies: map[string]*Policy{},
		topK:     3,
	}

	_, err := tool.Call(context.Background(), PolicySearchInput{Query: ""})
	assert.Error(t, err)
	assert.Equal(t, "query is required", err.Error())
}

// ============================================================================
// PolicySearchTool metadata tests
// ============================================================================

func TestPolicySearchTool_ToolInterface(t *testing.T) {
	tool := &PolicySearchTool{
		name:             "search_airline_policy",
		description:      "Search airline policies",
		keywords:         []string{"policy"},
		syntheticQueries: []string{"find policy"},
	}

	assert.Equal(t, "search_airline_policy", tool.Name())
	assert.Equal(t, "Search airline policies", tool.Description())
	assert.Equal(t, "", tool.Policy())
	assert.NotNil(t, tool.ParameterSchema())
}

func TestPolicySearchTool_IndexableToolInterface(t *testing.T) {
	tool := &PolicySearchTool{
		keywords:         []string{"policy", "guidance"},
		syntheticQueries: []string{"find policy", "search rules"},
	}

	assert.Equal(t, "Policy & Guidance", tool.Domain())
	assert.Equal(t, []string{"lookup", "policy"}, tool.Categories())
	assert.Equal(t, []string{"policy", "guidance"}, tool.Keywords())
	assert.Equal(t, []string{"find policy", "search rules"}, tool.SyntheticQueries())
}

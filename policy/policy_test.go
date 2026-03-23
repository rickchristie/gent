package policy

import (
	"context"
	"testing"

	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTokenCounter approximates ~4 chars per token for unit testing.
type mockTokenCounter struct{}

func (m *mockTokenCounter) TokenCount(s string) int { return len(s) / 4 }

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

	chunks, err := adapter.Chunks(p, &mockTokenCounter{}, 512)
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
	// Snippet is set to the first content chunk so search results show policy
	// content, not the synthetic query text.
	assert.Equal(t, search.Chunk{
		Text: `# cancellation-refund

customer wants to cancel
how to get a refund`,
		Snippet:  chunks[0].Text,
		Metadata: map[string]string{"h1": "cancellation-refund", "type": "synthetic_queries"},
	}, chunks[2])
}

func TestPolicyChunkAdapter_NoSyntheticQueries(t *testing.T) {
	adapter := &PolicyChunkAdapter{}
	p := &Policy{
		Id:          "simple-policy",
		FullContent: "A simple policy with no synthetic queries.",
	}

	chunks, err := adapter.Chunks(p, &mockTokenCounter{}, 512)
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

// ============================================================================
// PolicySearchTool snippet-only mode tests
// ============================================================================

func TestPolicySearchTool_Call_SnippetOnly_SingleResult(t *testing.T) {
	tool := &PolicySearchTool{
		name:        "search_policy",
		snippetOnly: true,
		index: &mockPolicyIndex{results: []search.SearchResult{
			{Id: "cancel-policy", Score: 0.9, Snippet: "Cancel within 24 hours"},
		}},
		policies: map[string]*Policy{
			"cancel-policy": {
				Id:          "cancel-policy",
				FullContent: "Full cancellation policy content here.",
			},
		},
		topK: 3,
	}

	result, err := tool.Call(
		context.Background(), PolicySearchInput{Query: "cancel"},
	)
	require.NoError(t, err)
	assert.Equal(t, `id: cancel-policy
Cancel within 24 hours`, result.Text)
}

func TestPolicySearchTool_Call_SnippetOnly_MultipleResults(t *testing.T) {
	tool := &PolicySearchTool{
		name:        "search_policy",
		snippetOnly: true,
		index: &mockPolicyIndex{results: []search.SearchResult{
			{Id: "cancel-policy", Score: 0.9, Snippet: "Cancel within 24 hours"},
			{Id: "refund-policy", Score: 0.7, Snippet: "Refund in 5-7 days"},
		}},
		policies: map[string]*Policy{
			"cancel-policy": {
				Id:          "cancel-policy",
				FullContent: "Full cancellation content.",
			},
			"refund-policy": {
				Id:          "refund-policy",
				FullContent: "Full refund content.",
			},
		},
		topK: 3,
	}

	result, err := tool.Call(
		context.Background(), PolicySearchInput{Query: "cancel refund"},
	)
	require.NoError(t, err)
	assert.Equal(t, `id: cancel-policy
Cancel within 24 hours

---

id: refund-policy
Refund in 5-7 days`, result.Text)
}

func TestPolicySearchTool_Call_FullContent_StillWorks(t *testing.T) {
	// Verify that snippetOnly=false (default) still returns full content.
	tool := &PolicySearchTool{
		name:        "search_policy",
		snippetOnly: false,
		index: &mockPolicyIndex{results: []search.SearchResult{
			{Id: "cancel-policy", Score: 0.9, Snippet: "Cancel within 24 hours"},
		}},
		policies: map[string]*Policy{
			"cancel-policy": {
				Id:          "cancel-policy",
				FullContent: "Full cancellation policy content here.",
			},
		},
		topK: 3,
	}

	result, err := tool.Call(
		context.Background(), PolicySearchInput{Query: "cancel"},
	)
	require.NoError(t, err)
	assert.Equal(t, `# cancel-policy

Full cancellation policy content here.`, result.Text)
}

// ============================================================================
// GetPolicyTool tests
// ============================================================================

func TestGetPolicyTool_Call_Found(t *testing.T) {
	tool := &GetPolicyTool{
		name: "get_policy",
		policies: map[string]*Policy{
			"cancel-policy": {
				Id:          "cancel-policy",
				FullContent: "Full cancellation policy content.",
			},
		},
	}

	result, err := tool.Call(
		context.Background(), GetPolicyInput{PolicyID: "cancel-policy"},
	)
	require.NoError(t, err)
	assert.Equal(t, `# cancel-policy

Full cancellation policy content.`, result.Text)
}

func TestGetPolicyTool_Call_NotFound(t *testing.T) {
	tool := &GetPolicyTool{
		name:     "get_policy",
		policies: map[string]*Policy{},
	}

	_, err := tool.Call(
		context.Background(),
		GetPolicyInput{PolicyID: "nonexistent"},
	)
	assert.Error(t, err)
	assert.Equal(t, "policy not found: nonexistent", err.Error())
}

func TestGetPolicyTool_Call_EmptyID(t *testing.T) {
	tool := &GetPolicyTool{
		name:     "get_policy",
		policies: map[string]*Policy{},
	}

	_, err := tool.Call(
		context.Background(), GetPolicyInput{PolicyID: ""},
	)
	assert.Error(t, err)
	assert.Equal(t, "policy_id is required", err.Error())
}

func TestGetPolicyTool_ToolInterface(t *testing.T) {
	tool := NewGetPolicyTool([]*Policy{
		{Id: "test", FullContent: "content"},
	})

	assert.Equal(t, "get_policy", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.Equal(t, "", tool.Policy())
	assert.NotNil(t, tool.ParameterSchema())
}

func TestGetPolicyTool_IndexableToolInterface(t *testing.T) {
	tool := NewGetPolicyTool([]*Policy{})

	assert.Equal(t, "Policy & Guidance", tool.Domain())
	assert.Equal(t, []string{"lookup", "policy"}, tool.Categories())
	assert.NotEmpty(t, tool.Keywords())
	assert.NotEmpty(t, tool.SyntheticQueries())
}

func TestGetPolicyTool_WithName(t *testing.T) {
	tool := NewGetPolicyTool([]*Policy{}).
		WithName("get_airline_policy").
		WithDescription("Get airline policy by ID")

	assert.Equal(t, "get_airline_policy", tool.Name())
	assert.Equal(t, "Get airline policy by ID", tool.Description())
}

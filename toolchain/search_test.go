package toolchain

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
)

type indexableToolFunc = gent.Tool[map[string]any]

func newIndexableTool(name, description, domain string, categories, keywords []string,
	call func(context.Context, map[string]any) (string, error),
) indexableToolFunc {
	return newIndexableToolWithSchema(name, description, domain, categories, keywords, nil, call)
}
func newIndexableToolWithSchema(name, description, domain string, categories, keywords []string,
	inputSchema map[string]any, call func(context.Context, map[string]any) (string, error),
) indexableToolFunc {
	return gent.DefineTool(name, func(ctx context.Context, args map[string]any) (
		*gent.ToolExecutionResponse, error,
	) {
		return capturedToolResponse(call(ctx, args))
	}).Description(description).Domain(domain).
		Categories(categories...).Keywords(keywords...).ParameterSchema(inputSchema).MustBuild()
}

type mockToolSearcher struct {
	id, guidance string
	searchFn     func(context.Context, string) ([]string, error)
	resultFn     func(context.Context, string) (*gent.ToolSearchResult, error)
	indexErr     error
	indexCalled  bool
}

func (s *mockToolSearcher) Id() string             { return s.id }
func (s *mockToolSearcher) SearchGuidance() string { return s.guidance }
func (s *mockToolSearcher) IndexAll([]gent.IndexableTool) error {
	s.indexCalled = true
	return s.indexErr
}
func (s *mockToolSearcher) Search(ctx context.Context, q string) (*gent.ToolSearchResult, error) {
	if s.resultFn != nil {
		return s.resultFn(ctx, q)
	}
	var names []string
	var err error
	if s.searchFn != nil {
		names, err = s.searchFn(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	return &gent.ToolSearchResult{Status: gent.ToolExecuted, Names: names}, nil
}
func setupSearchChain(tools []indexableToolFunc, engines []gent.ToolSearcher) *SearchChain {
	chain := NewSearch(SearchHintDomainCategories)
	for _, engine := range engines {
		chain.RegisterEngine(engine)
	}
	for _, tool := range tools {
		chain.RegisterTool(tool)
	}
	if err := chain.Initialize(); err != nil {
		panic(err)
	}
	return chain
}
func searchTestFormat() gent.TextFormat { return testFormat() }

func TestSharedSearchAndTargetExecution(t *testing.T) {
	ctx := newExecCtx()
	defer ctx.Release()
	calls := 0
	tool := gent.DefineTool("lookup", func(_ context.Context, input validationInput) (
		*gent.ToolExecutionResponse, error,
	) {
		calls++
		return capturedToolResponse(input, nil)
	}).
		Description("Read an order").Parameters(schema.Field("id").MinLength(1)).MustBuild()
	chain := NewSearch(SearchHintSimpleList).RegisterEngine(NewRegexToolSearcher())
	chain.RegisterTool(tool)
	chain.RegisterDeprecatedTool(newIndexableTool("old", "Old order lookup", "Orders", nil, nil,
		func(context.Context, map[string]any) (string, error) { return "old", nil }))
	assert.NoError(t, chain.Initialize())
	wantNames := []string{searchToolName, executeToolName}
	assert.Equal(t, wantNames, nativeNames(t, chain, ctx))
	search := executeNativeTestCall(t, chain, ctx, nativeCall(searchToolName,
		`{"query":"lookup","query_type":"regex"}`))
	assert.Equal(t, gent.ToolExecuted, search.Execution().Status)
	part, ok := search.Execution().Parts[0].(gent.DataContent)
	if !assert.True(t, ok) {
		return
	}
	assert.Equal(t, 1, len(part.Value().([]any)))
	assert.Equal(t, wantNames, nativeNames(t, chain, ctx),
		"discovery must not promote native definitions")
	call := executeNativeTestCall(t, chain, ctx, nativeCall(executeToolName,
		`{"tool":"lookup","args":{"id":"O7"}}`))
	assert.Equal(t, "lookup", call.Name())
	assert.Equal(t, executeToolName, call.Response().Name())
	assert.Equal(t, "provider-id", call.Response().ToolCallID())
	assert.Equal(t, `{"id":"O7"}`, recordText(call))
	assert.Equal(t, 1, calls)
	bad := executeNativeTestCall(t, chain, ctx, nativeCall(executeToolName,
		`{"tool":"lookup","args":{"id":7}}`))
	assert.Equal(t, gent.ToolInvalidArguments, bad.Execution().Status)
	want := gent.DefaultSteeringMessages().InvalidArguments + "\n" +
		"Invalid arguments for tool \"lookup\".\nargs.id: expected string.\n" +
		"Expected input schema:\n" + completeInputSchema
	assert.Equal(t, want, recordText(bad))
	assert.Equal(t, 1, calls)
	old := executeNativeTestCall(t, chain, ctx, nativeCall(executeToolName,
		`{"tool":"old","args":{}}`))
	assert.Equal(t, "old", recordText(old))
	before := ctx.PublishBeforeModelCall("test", gent.NewModelCallRequest(nil))
	assert.Len(t, before.ToolSearches, 1)
	assert.Equal(t, []string{"lookup"}, before.ToolSearches[0].Names)
	assert.Empty(t, ctx.PublishBeforeModelCall("test", gent.NewModelCallRequest(nil)).ToolSearches)
}

func TestSearchResultStates(t *testing.T) {
	type input struct {
		result *gent.ToolSearchResult
		err    error
	}
	type expected struct {
		kind   gent.ToolResponseKind
		status gent.ToolStatus
		text   string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"empty", input{result: &gent.ToolSearchResult{Status: gent.ToolExecuted}}, expected{
			gent.ToolResponseExecution, gent.ToolExecuted, "[]\nNO MATCHES\n\nPage 1; 0 matching tools."}},
		{"blocked", input{result: &gent.ToolSearchResult{Status: gent.ToolBlocked,
			Instructions: "Wait for catalog access."}}, expected{gent.ToolResponseExecution,
			gent.ToolBlocked, "The requested action was blocked.\nWait for catalog access."}},
		{"backend error", input{err: errors.New("private connection string")}, expected{
			gent.ToolResponseCallError, "", gent.DefaultSteeringMessages().ToolError}},
		{"nil nil", input{}, expected{gent.ToolResponseCallError, "",
			gent.DefaultSteeringMessages().ToolError}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			engine := &mockToolSearcher{id: "test", resultFn: func(context.Context, string) (
				*gent.ToolSearchResult, error,
			) {
				return test.input.result, test.input.err
			}}
			chain := NewSearch(SearchHintSimpleList).
				WithNoResultsMessage("NO MATCHES").RegisterEngine(engine)
			assert.NoError(t, chain.Initialize())
			ctx := newExecCtx()
			defer ctx.Release()
			record := executeNativeTestCall(t, chain, ctx, nativeCall(searchToolName,
				`{"query":"order","query_type":"test"}`))
			status, _ := record.Response().ExecutionStatus()
			assert.Equal(t, test.expected, expected{record.Kind(), status, recordText(record)})
		})
	}
}

func TestSearchPaginationAndConcurrentUse(t *testing.T) {
	chain := NewSearch(SearchHintSimpleList).WithPageSize(2).RegisterEngine(NewRegexToolSearcher())
	for _, name := range []string{"order_a", "order_b", "order_c"} {
		chain.RegisterTool(newIndexableTool(name, name, "Orders", nil, nil,
			func(context.Context, map[string]any) (string, error) { return "ok", nil }))
	}
	chain.Pin("order_a")
	assert.NoError(t, chain.Initialize())
	type input struct{ page int }
	type expected struct {
		names        []string
		instructions string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first", input{1}, expected{[]string{"order_a", "order_b"}, "Page 1; 3 matching tools."}},
		{"second", input{2}, expected{[]string{"order_c"}, "Page 2; 3 matching tools."}},
	}
	var workers sync.WaitGroup
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for range 4 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					ctx := newExecCtx()
					defer ctx.Release()
					result, err := chain.Search(ctx, &gent.ToolSearchRequest{Query: "order", QueryType: "regex",
						Page: test.input.page})
					assert.NoError(t, err)
					assert.Equal(t, test.expected, expected{result.Names, result.Instructions})
				}()
			}
			workers.Wait()
		})
	}
	ctx := newExecCtx()
	defer ctx.Release()
	assert.Equal(t, []string{searchToolName, executeToolName, "order_a"}, nativeNames(t, chain, ctx))
}

func TestSearchInitializationFailures(t *testing.T) {
	type input struct{ create func() *SearchChain }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"no engine", input{func() *SearchChain { return NewSearch(SearchHintSimpleList) }},
			expected{"SearchChain.Initialize: at least one search engine is required"}},
		{"index failed", input{func() *SearchChain {
			return NewSearch(SearchHintSimpleList).
				RegisterEngine(&mockToolSearcher{id: "test", indexErr: errors.New("index unavailable")})
		}},
			expected{`SearchChain.Initialize: engine "test" IndexAll failed: index unavailable`}},
		{"missing pin", input{func() *SearchChain {
			return NewSearch(SearchHintSimpleList).
				RegisterEngine(NewRegexToolSearcher()).Pin("missing")
		}},
			expected{`pinned tool "missing" is not registered`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assert.EqualError(t, test.input.create().Initialize(), test.expected.err)
		})
	}
}

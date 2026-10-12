package toolchain

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func nativeCall(name, arguments string) llms.ToolCall {
	return llms.ToolCall{ID: "provider-id", Type: "function",
		FunctionCall: &llms.FunctionCall{Name: name, Arguments: arguments}}
}

func TestNativeTools_DefinitionsAndExecution(t *testing.T) {
	type input struct{ chain func() gent.NativeToolChain }
	type expected struct{ text string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"JSON", input{func() gent.NativeToolChain { return NewJSON().WithOutputSchema(true) }},
			expected{`<lookup>
record 7
</lookup>`}},
		{"YAML", input{func() gent.NativeToolChain { return NewYAML().WithOutputSchema(true) }},
			expected{`<lookup>
record 7
</lookup>`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := newExecCtx()
			chain := test.input.chain()
			var got []map[string]any
			tool := gent.NewToolFunc("lookup", "Read a record", nil,
				func(_ context.Context, args map[string]any) (string, error) {
					got = append(got, args)
					return "record 7", nil
				}).WithPolicy("Read only").WithDeduplicateSummary(
				func(map[string]any, string) string { return "same record" })
			chain.RegisterTool(tool)
			definitions, err := chain.NativeTools(ctx)
			if !assert.NoError(t, err) {
				return
			}
			want := []llms.Tool{{Type: "function", Function: &llms.FunctionDefinition{
				Name: "lookup", Description: `Read a record

Policy: Read only

Returns: {"type":"string"}`,
				Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			}}}
			assert.Equal(t, want, definitions)
			definitions[0].Function.Description = "changed by a request hook"
			again, err := chain.NativeTools(ctx)
			assert.NoError(t, err)
			assert.Equal(t, want, again)
			plan, err := chain.PrepareCalls(ctx, []llms.ToolCall{nativeCall("lookup", `{"id":7}`)})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, []map[string]any(nil), got, "preparation must not execute a tool")
			output, err := chain.ExecuteNext(ctx, plan, format.NewXML())
			if !assert.NoError(t, err) {
				return
			}
			args := map[string]any{"id": float64(7)}
			assert.Equal(t, &gent.ToolCallResult{Name: "lookup", Args: args, Input: args,
				Output: "record 7", Hash: ToolCallResultHash("lookup", args),
				Text: test.expected.text, ToolCallId: plan.Calls[0].Id, AttemptId: ctx.AttemptId(),
				SummaryCaptured: true, DeduplicationSummary: "same record"}, output)
			assert.Equal(t, []map[string]any{args}, got)
			assert.NotEqual(t, "provider-id", output.ToolCallId)
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolCallsFor+"lookup"))
			_, err = chain.ExecuteNext(ctx, plan, format.NewXML())
			assert.ErrorIs(t, err, gent.ErrToolPlanComplete)
		})
	}
}

func TestNativeCalls_ArgumentErrors(t *testing.T) {
	type input struct{ call llms.ToolCall }
	type expected struct{ errorText string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"name", input{llms.ToolCall{Type: "function"}}, expected{gent.ErrMissingToolName.Error()}},
		{"kind", input{llms.ToolCall{Type: "computer"}},
			expected{`unsupported tool call type "computer"`}},
		{"null", input{nativeCall("lookup", "null")},
			expected{gent.ErrInvalidJSON.Error() + ": tool arguments must be an object"}},
		{"array", input{nativeCall("lookup", "[]")}, expected{gent.ErrInvalidJSON.Error() +
			": json: cannot unmarshal array into Go value of type map[string]interface {}"}},
		{"truncated", input{nativeCall("lookup", "{")},
			expected{gent.ErrInvalidJSON.Error() + ": unexpected end of JSON input"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := newExecCtx()
			chain := NewJSON()
			plan, err := chain.PrepareCalls(ctx, []llms.ToolCall{test.input.call})
			assert.Nil(t, plan)
			assert.EqualError(t, err, test.expected.errorText)
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolchainParseErrorTotal))
			assert.Equal(t, float64(1), ctx.Stats().GetGauge(gent.SGToolchainParseErrorConsecutive))
			_, err = chain.PrepareCalls(ctx, []llms.ToolCall{nativeCall("lookup", "{}")})
			assert.NoError(t, err)
			assert.Equal(t, float64(0), ctx.Stats().GetGauge(gent.SGToolchainParseErrorConsecutive))
		})
	}
}

func TestNativeSearch_DiscoveryBelongsToActiveHistory(t *testing.T) {
	type input struct {
		mode SearchType
		hint SearchHintType
	}
	type expected struct{ initial, searched, loaded []string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"search", input{Search, SearchHintDomainCategories}, expected{
			[]string{searchToolName, "pinned"}, []string{searchToolName, "pinned", "lookup"},
			[]string{searchToolName, "pinned", "lookup"}}},
		{"search then schema", input{SearchGet, SearchHintSimpleList}, expected{
			[]string{searchToolName, getSchemaToolName, "pinned"},
			[]string{searchToolName, getSchemaToolName, "pinned"},
			[]string{searchToolName, getSchemaToolName, "pinned", "lookup"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := newExecCtx()
			chain := NewSearchJSON(test.input.hint).WithSearchType(test.input.mode)
			_, err := chain.NativeTools(ctx)
			assert.EqualError(t, err, "SearchJSON must be initialized before native tool calls")
			chain.RegisterEngine(&mockToolSearcher{id: "test", guidance: "Use words",
				searchFn: func(context.Context, string) ([]string, error) {
					return []string{"lookup"}, nil
				}})
			for _, name := range []string{"pinned", "lookup"} {
				chain.RegisterTool(newIndexableTool(name, "Read data", "Sales", nil, nil,
					func(context.Context, map[string]any) (string, error) { return "data", nil }))
			}
			chain.Pin("pinned")
			if !assert.NoError(t, chain.Initialize()) {
				return
			}
			assert.Equal(t, test.expected.initial, nativeNames(t, chain, ctx))
			call := nativeCall(searchToolName, `{"query":"data","query_type":"test"}`)
			result := executeNativeTestCall(t, chain, ctx, call)
			appendNativeSearchResult(t, ctx, result, 0)
			assert.Equal(t, test.expected.searched, nativeNames(t, chain, ctx))
			if test.input.mode == SearchGet {
				result = executeNativeTestCall(t, chain, ctx,
					nativeCall(getSchemaToolName, `{"tool_name":"lookup"}`))
				appendNativeSearchResult(t, ctx, result, 0)
			}
			assert.Equal(t, test.expected.loaded, nativeNames(t, chain, ctx))
			assert.Equal(t, test.expected.initial, nativeNames(t, chain, newExecCtx()))
			draft := ctx.ContextDraft()
			assert.NoError(t, ctx.UpdateHistory(gent.HistoryUpdate{
				ExpectedDraftRevision: draft.Revision, Active: nil, Reason: "test compaction",
			}))
			assert.Equal(t, test.expected.initial, nativeNames(t, chain, ctx))
			appendNativeSearchResult(t, ctx, result, 1)
			ctx.IncrementIteration()
			assert.Equal(t, test.expected.initial, nativeNames(t, chain, ctx))
		})
	}
}

func nativeNames(t *testing.T, chain gent.NativeToolChain, ctx *gent.ExecutionContext) []string {
	t.Helper()
	definitions, err := chain.NativeTools(ctx)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	names := make([]string, len(definitions))
	for i, definition := range definitions {
		names[i] = definition.Function.Name
	}
	return names
}

func executeNativeTestCall(t *testing.T, chain gent.NativeToolChain, ctx *gent.ExecutionContext,
	call llms.ToolCall) *gent.ToolCallResult {
	t.Helper()
	plan, err := chain.PrepareCalls(ctx, []llms.ToolCall{call})
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	result, err := chain.ExecuteNext(ctx, plan, format.NewXML())
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return result
}

func appendNativeSearchResult(t *testing.T, ctx *gent.ExecutionContext,
	result *gent.ToolCallResult, expiry int) {
	t.Helper()
	message := &gent.MessageContent{Role: llms.ChatMessageTypeTool}
	message.SetMetadata(gent.MMKToolChainResult, &gent.ToolChainResult{
		Results: []*gent.ToolCallResult{result},
	})
	assert.NoError(t, ctx.AppendRecord(&gent.HistoryEntry{Messages: []*gent.MessageContent{message},
		ExpireAfterIteration: expiry}, true))
}

type nativeCancelHook struct{ calls int }

func (hook *nativeCancelHook) OnBeforeToolCall(ctx *gent.ExecutionContext,
	_ *gent.BeforeToolCallEvent) {
	hook.calls++
	ctx.CancelRun(errors.New("blocked by hook"))
}

func TestNativeSearch_CancelBeforeEffect(t *testing.T) {
	chain := setupSearchJSON([]*indexableToolFunc{newIndexableTool("lookup", "Read", "Sales",
		nil, nil, func(context.Context, map[string]any) (string, error) {
			t.Error("cancelled tool must not run")
			return "", nil
		})}, []gent.ToolSearcher{&mockToolSearcher{id: "test"}})
	ctx := newExecCtx()
	hook := &nativeCancelHook{}
	ctx.SetEventPublisher(events.NewRegistry().Subscribe(hook))
	plan, err := chain.PrepareCalls(ctx, []llms.ToolCall{nativeCall("lookup", "{}")})
	if !assert.NoError(t, err) {
		return
	}
	result, err := chain.ExecuteNext(ctx, plan, format.NewXML())
	if !assert.NoError(t, err) {
		return
	}
	assert.ErrorIs(t, result.Error, context.Canceled)
	assert.Equal(t, 1, hook.calls)
	assert.Equal(t, 1, plan.NextCall)
}

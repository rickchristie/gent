package react

import (
	"context"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func nativeTestCall(id, name, args string) llms.ToolCall {
	return llms.ToolCall{ID: id, Type: "function",
		FunctionCall: &llms.FunctionCall{Name: name, Arguments: args}}
}

func nativeTestResponse(choice *gent.ContentChoice) *gent.ContentResponse {
	if choice == nil {
		return &gent.ContentResponse{}
	}
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}}
}

// This adapter has no display chunks. Its completed response still needs the same size and
// repetition checks as a streamed response, including arguments that arrive only on completion.
type nativeFinalModel struct{ *mockModel }

func (model *nativeFinalModel) GenerateContentStream(_ context.Context,
	request gent.ModelCallRequest) (gent.Stream, error) {
	response, err := model.GenerateContent(nil, "", "", request.Messages,
		llms.WithOptions(request.Options))
	stream := gent.NewStreamWithDuration()
	stream.Complete(response, err)
	return stream, nil
}

type nativeMediaTool struct {
	*gent.ToolFunc[map[string]any, string]
}

func (tool *nativeMediaTool) Call(ctx context.Context, args map[string]any) (
	*gent.ToolResult[string], error,
) {
	result, err := tool.ToolFunc.Call(ctx, args)
	if err != nil {
		return nil, err
	}
	result.Instructions = "Use the image."
	result.Media = []gent.ContentPart{llms.ImageURLContent{URL: "https://example.com/record.png"}}
	return result, nil
}

func TestNativeAgent_RequestAndToolHistory(t *testing.T) {
	var inputs []map[string]any
	tool := &nativeMediaTool{gent.NewToolFunc("lookup", "Read a record", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			inputs = append(inputs, args)
			return "record", nil
		}).WithDeduplicateSummary(func(map[string]any, string) string { return "same record" })}
	calls := []llms.ToolCall{nativeTestCall("call-a", "lookup", `{"id":7}`),
		nativeTestCall("call-b", "lookup", `{"id":8}`)}
	model := newMockModel(nativeTestResponse(&gent.ContentChoice{Content: "I will check.",
		ToolCalls: calls, ReasoningContent: "private reasoning"}),
		nativeTestResponse(&gent.ContentChoice{Content: "Done."}))
	agent := NewNativeAgent(model).RegisterTool(tool).
		WithBehaviorAndContext("Support agent").WithCriticalRules("Read only").
		WithCallOptions(llms.WithTemperature(0.4), llms.WithTopP(0.9))
	ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read two records."}))
	result, err := agent.Next(ctx)
	if !assert.NoError(t, err) {
		return
	}
	toolText := `<lookup>
<result>
record
</result>
<instructions>
Use the image.
</instructions>
</lookup>`
	assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue,
		NextPrompt: toolText + "\n" + toolText}, result)
	result, err = agent.Next(ctx)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: "Done."}}}, result)
	system := `<behavior>
Support agent
</behavior>
<instructions>
Use the available tools to get facts and complete the task.
Check tool errors before you continue. Do not invent tool results.
Give the final answer when you have enough information.
</instructions>
<critical_rules>
Read only
</critical_rules>
<answer_requirements>
Write your final answer here.
</answer_requirements>`
	initial := []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeSystem, system),
		llms.TextParts(llms.ChatMessageTypeHuman, "Read two records.")}
	want := append(append([]llms.MessageContent(nil), initial...),
		llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
			llms.TextContent{Text: "I will check."}, calls[0], calls[1]}},
		llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "call-a", Name: "lookup", Content: toolText}}},
		llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "call-b", Name: "lookup", Content: toolText}}},
		llms.MessageContent{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{
			llms.TextContent{Text: "Media from lookup (call call-a):"},
			llms.ImageURLContent{URL: "https://example.com/record.png"},
			llms.TextContent{Text: "Media from lookup (call call-b):"},
			llms.ImageURLContent{URL: "https://example.com/record.png"}}})
	assert.Equal(t, [][]llms.MessageContent{initial, want}, model.capturedMessages)
	definitions := []llms.Tool{{Type: "function", Function: &llms.FunctionDefinition{
		Name: "lookup", Description: "Read a record",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}}}
	wantOptions := gent.NewModelCallRequest(nil, llms.WithTemperature(0.4),
		llms.WithTopP(0.9), llms.WithTools(definitions)).Options
	for _, options := range model.capturedOptions {
		assert.Equal(t, wantOptions, gent.NewModelCallRequest(nil, options...).Options)
	}
	assert.Equal(t, []map[string]any{{"id": float64(7)}, {"id": float64(8)}}, inputs)
	assert.Equal(t, int64(2), ctx.Stats().GetCounter(gent.SCToolCalls))
	assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCFormatParseErrorTotal))
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(bundle, ctx.Codecs()))
}

func TestNativeAgent_RetryInvalidCalls(t *testing.T) {
	type input struct{ choice *gent.ContentChoice }
	type expected struct {
		feedback    string
		parseErrors int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"no choice", input{nil}, expected{"The model returned no response. Please try again.", 0}},
		{"incomplete", input{&gent.ContentChoice{StopReason: "length",
			ToolCalls: []llms.ToolCall{nativeTestCall("a", "lookup", "{}")}}},
			expected{"The response was incomplete. Use a shorter answer or fewer tool calls.", 0}},
		{"empty", input{&gent.ContentChoice{}},
			expected{"Use a tool or give a final answer.", 0}},
		{"duplicate IDs", input{&gent.ContentChoice{ToolCalls: []llms.ToolCall{
			nativeTestCall("a", "lookup", "{}"), nativeTestCall("a", "lookup", "{}")}}},
			expected{`duplicate model tool call ID "a"`, 1}},
		{"bad JSON", input{&gent.ContentChoice{ToolCalls: []llms.ToolCall{
			nativeTestCall("a", "lookup", "{")}}},
			expected{gent.ErrInvalidJSON.Error() + ": unexpected end of JSON input", 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := newMockModel(nativeTestResponse(test.input.choice),
				nativeTestResponse(&gent.ContentChoice{Content: "Done."}))
			agent := NewNativeAgent(model).RegisterTool(gent.NewToolFunc("lookup", "Read", nil,
				func(context.Context, map[string]any) (string, error) {
					t.Error("invalid response must not execute tools")
					return "", nil
				}))
			ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
			result, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue,
				NextPrompt: test.expected.feedback}, result)
			assert.Equal(t, test.expected.parseErrors,
				ctx.Stats().GetCounter(gent.SCToolchainParseErrorTotal))
			ctx.IncrementIteration()
			result, err = agent.Next(ctx)
			assert.NoError(t, err)
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LATerminate,
				Result: []gent.ContentPart{llms.TextContent{Text: "Done."}}}, result)
			for _, message := range model.capturedMessages[1] {
				for _, part := range message.Parts {
					_, isCall := part.(llms.ToolCall)
					assert.False(t, isCall, "do not replay an invalid call group")
				}
			}
		})
	}
}

type nativeAnswer struct {
	Status string `json:"status"`
}
type nativeValidator struct{ values []any }

func (*nativeValidator) Name() string { return "status" }
func (validator *nativeValidator) Validate(
	_ *gent.ExecutionContext, value any,
) *gent.ValidationResult {
	validator.values = append(validator.values, value)
	return &gent.ValidationResult{Accepted: value.(nativeAnswer).Status == "done",
		Feedback: []gent.FormattedSection{{Name: "error", Content: "Check the status."}}}
}

func TestNativeAgent_JSONValidation(t *testing.T) {
	model := newMockModel(nativeTestResponse(&gent.ContentChoice{Content: "invalid"}),
		nativeTestResponse(&gent.ContentChoice{Content: `{"status":"pending"}`}),
		nativeTestResponse(&gent.ContentChoice{Content: `{"status":"done"}`}))
	validator := &nativeValidator{}
	term := termination.NewJSON[nativeAnswer]("answer")
	term.SetValidator(validator)
	agent := NewNativeAgent(model).WithTermination(term)
	ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Check."}))
	first, err := agent.Next(ctx)
	assert.NoError(t, err)
	assert.Equal(t, gent.LAContinue, first.Action)
	second, err := agent.Next(ctx)
	assert.NoError(t, err)
	assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<error>
Check the status.
</error>`}, second)
	third, err := agent.Next(ctx)
	assert.NoError(t, err)
	assert.Equal(t, &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: `{"status":"done"}`}}}, third)
	assert.Equal(t, []any{nativeAnswer{"pending"}, nativeAnswer{"done"}}, validator.values)
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCTerminationParseErrorTotal))
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCAnswerRejectedBy+"status"))
}

func TestNativeAgent_FinalOnlySizeAndRepetition(t *testing.T) {
	type input struct {
		content, args string
		max           int
		repetition    bool
	}
	type expected struct{ loops int64 }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"argument size", input{"", `{"id":"too much data"}`, 8, false}, expected{0}},
		{"text size", input{"too much text", "{}", 8, false}, expected{0}},
		{"repetition", input{strings.Repeat("abcdefghij", 40), "{}", 0, true}, expected{1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := newMockModel(nativeTestResponse(&gent.ContentChoice{Content: test.input.content,
				ToolCalls: []llms.ToolCall{nativeTestCall("a", "lookup", test.input.args)}}))
			config := agentutil.DefaultRepetitionConfig()
			config.Enabled = test.input.repetition
			config.BlockSize = 50
			agent := NewNativeAgent(&nativeFinalModel{model}).WithMaxResponseChars(test.input.max).
				WithRepetitionConfig(config).RegisterTool(gent.NewToolFunc("lookup", "Read", nil,
				func(context.Context, map[string]any) (string, error) {
					t.Error("truncated response must not execute tools")
					return "", nil
				}))
			ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
			result, err := agent.Next(ctx)
			assert.NoError(t, err)
			assert.Equal(t, gent.LAContinue, result.Action)
			assert.Equal(t, test.expected.loops, ctx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
		})
	}
}

type nativeLimitHook struct{ key gent.StatKey }

func (hook *nativeLimitHook) OnLimitExceeded(
	_ *gent.ExecutionContext, event *gent.LimitExceededEvent,
) {
	hook.key = event.MatchedKey
}

func TestNativeAgent_ToolLimits(t *testing.T) {
	type input struct{ max float64 }
	type expected struct {
		effects []string
		key     gent.StatKey
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first call", input{0}, expected{nil, gent.SCToolCallsFor + "first"}},
		{"later call", input{1}, expected{[]string{"first", "second"}, gent.SCToolCallsFor + "second"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var effects []string
			model := newMockModel(nativeTestResponse(&gent.ContentChoice{ToolCalls: []llms.ToolCall{
				nativeTestCall("a", "first", "{}"), nativeTestCall("b", "second", "{}")}}),
				nativeTestResponse(&gent.ContentChoice{ToolCalls: []llms.ToolCall{
					nativeTestCall("c", "second", "{}"), nativeTestCall("d", "first", "{}")}}))
			agent := NewNativeAgent(model)
			for _, name := range []string{"first", "second"} {
				agent.RegisterTool(gent.NewToolFunc(name, "Read", nil,
					func(context.Context, map[string]any) (string, error) {
						effects = append(effects, name)
						return "ok", nil
					}))
			}
			ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
			ctx.SetLimits([]gent.Limit{{Type: gent.LimitKeyPrefix,
				Key: gent.SCToolCallsFor, MaxValue: test.input.max}})
			hook := &nativeLimitHook{}
			_, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).
				WithEvents(events.NewRegistry().Subscribe(hook)).Execute(ctx)
			assert.EqualError(t, err, "limit exceeded: gent:tool_calls: > "+
				map[float64]string{0: "0", 1: "1"}[test.input.max])
			assert.Equal(t, test.expected, expected{effects, hook.key})
		})
	}
}

func TestNativeAgent_UnsupportedChain(t *testing.T) {
	agent := NewNativeAgent(newMockModel()).WithToolChain(newMockToolChain())
	ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
	assert.EqualError(t, agent.ConfigureExecution(ctx),
		"tool chain *react.mockToolChain does not support native calls")
	_, err := agent.Next(ctx)
	assert.EqualError(t, err,
		"model call failed: tool chain *react.mockToolChain does not support native calls")
	assert.Equal(t, 0, agent.model.(*mockModel).callCount)
}

func TestNativeAgent_MissingCallID(t *testing.T) {
	call := nativeTestCall("", "lookup", "{}")
	call.Type = ""
	model := newMockModel(nativeTestResponse(&gent.ContentChoice{ToolCalls: []llms.ToolCall{call}}),
		nativeTestResponse(&gent.ContentChoice{Content: "Done."}))
	agent := NewNativeAgent(model).WithToolChain(toolchain.NewYAML().RegisterTool(
		gent.NewToolFunc("lookup", "Read", nil,
			func(context.Context, map[string]any) (string, error) { return "ok", nil })))
	ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
	_, err := agent.Next(ctx)
	if !assert.NoError(t, err) {
		return
	}
	_, err = agent.Next(ctx)
	assert.NoError(t, err)
	saved := model.capturedMessages[1][2].Parts[0].(llms.ToolCall)
	assert.NotEmpty(t, saved.ID)
	assert.Equal(t, "function", saved.Type)
	assert.Equal(t, llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
		llms.ToolCallResponse{ToolCallID: saved.ID, Name: "lookup", Content: `<lookup>
ok
</lookup>`}}}, model.capturedMessages[1][3])
}

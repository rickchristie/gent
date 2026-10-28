package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFormat returns a TextFormat for use in tests.
func testFormat() gent.TextFormat {
	return format.NewXML()
}

func TestRegistry_RegisterTool(t *testing.T) {
	type input struct {
		toolName string
		content  string
	}

	type expected struct {
		callCount int
		callName  string
		err       error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "register and execute tool",
			input: input{
				toolName: "test",
				content:  `{"tool": "test", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				callName:  "test",
				err:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			tool := gent.DefineTool(tt.input.toolName,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "result", nil
				}).Description("A test tool").ParameterSchema(nil).MustBuild()

			tc.RegisterTool(tool)

			result, err := executeTestCalls(tc, nil, tt.input.content, testFormat())

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
			} else {
				require.NoError(t, err)
				assert.Len(t, result.Results, tt.expected.callCount)
				assert.Equal(t, tt.expected.callName, result.Results[0].Name)
			}
		})
	}
}

func TestRegistry_RegisterTool_PanicsOnInvalidOrDuplicate(t *testing.T) {
	type input struct {
		register func(tc *Registry)
	}

	type expected struct {
		panics bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "nil tool panics",
			input: input{register: func(tc *Registry) {
				tc.RegisterTool(nil)
			}},
			expected: expected{panics: true},
		},
		{
			name: "invalid tool type panics",
			input: input{register: func(tc *Registry) {
				tc.RegisterTool("not a tool")
			}},
			expected: expected{panics: true},
		},
		{
			name: "duplicate tool name panics",
			input: input{register: func(tc *Registry) {
				tool1 := gent.DefineTool("duplicate",
					func(ctx context.Context, args map[string]any) (string, error) {
						return "first", nil
					}).Description("First").ParameterSchema(nil).MustBuild()
				tool2 := gent.DefineTool("duplicate",
					func(ctx context.Context, args map[string]any) (string, error) {
						return "second", nil
					}).Description("Second").ParameterSchema(nil).MustBuild()
				tc.RegisterTool(tool1)
				tc.RegisterTool(tool2)
			}},
			expected: expected{panics: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()

			if tt.expected.panics {
				assert.Panics(t, func() {
					tt.input.register(tc)
				})
			} else {
				assert.NotPanics(t, func() {
					tt.input.register(tc)
				})
			}
		})
	}
}

func TestRegistry_Execute(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
		fn          func(ctx context.Context, args map[string]any) (string, error)
	}

	type input struct {
		content string
	}

	type expected struct {
		callCount   int
		results     []string
		errors      []error
		executeErr  error
		resultNames []string
	}

	tests := []struct {
		name     string
		mocks    []mockTool
		input    input
		expected expected
	}{
		{
			name: "success",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						query := args["query"].(string)
						return fmt.Sprintf("Results for: %s", query), nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {"query": "weather"}}`,
			},
			expected: expected{
				callCount:   1,
				results:     []string{`Results for: weather`},
				errors:      []error{nil},
				resultNames: []string{"search"},
			},
		},
		{
			name:  "unknown tool",
			mocks: []mockTool{},
			input: input{
				content: `{"tool": "unknown", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				results:   []string{""},
				errors:    []error{gent.ErrUnknownTool},
			},
		},
		{
			name: "tool error",
			mocks: []mockTool{
				{
					name:        "failing",
					description: "A failing tool",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "", errors.New("tool execution failed")
					},
				},
			},
			input: input{
				content: `{"tool": "failing", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				results:   []string{""},
				errors:    []error{errors.New("tool execution failed")},
			},
		},
		{
			name: "multiple tools",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "search result", nil
					},
				},
				{
					name:        "calendar",
					description: "Calendar",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "calendar result", nil
					},
				},
			},
			input: input{
				content: `[
		{"tool": "search", "args": {}},
		{"tool": "calendar", "args": {}}
	]`,
			},
			expected: expected{
				callCount:   2,
				results:     []string{`search result`, `calendar result`},
				errors:      []error{nil, nil},
				resultNames: []string{"search", "calendar"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			for _, mock := range tt.mocks {
				tool := gent.DefineTool(mock.name, mock.fn).Description(mock.description).
					ParameterSchema(mock.schema).MustBuild()
				tc.RegisterTool(tool)
			}

			result, err := executeTestCalls(tc, nil, tt.input.content, testFormat())

			if tt.expected.executeErr != nil {
				assert.ErrorIs(t, err, tt.expected.executeErr)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Results, tt.expected.callCount)

			for i, expectedResult := range tt.expected.results {
				if tt.expected.errors[i] != nil {
					assert.Error(t, result.Results[i].Error)
					if errors.Is(tt.expected.errors[i], gent.ErrUnknownTool) {
						assert.ErrorIs(t, result.Results[i].Error, gent.ErrUnknownTool)
					} else {
						assert.Equal(t, tt.expected.errors[i].Error(), result.Results[i].Error.Error())
					}
				} else {
					assert.NoError(t, result.Results[i].Error)
					assert.Equal(t, expectedResult, getOutputString(result.Results[i].Output))
					if len(tt.expected.resultNames) > i {
						assert.Equal(t, tt.expected.resultNames[i], result.Results[i].Name)
					}
				}
			}
		})
	}
}

func TestRegistry_Execute_SchemaValidation(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
		fn          func(ctx context.Context, args map[string]any) (string, error)
	}

	type input struct {
		content string
	}

	type expected struct {
		result      string
		errContains string
		resultIsNil bool
		noToolError bool
	}

	tests := []struct {
		name     string
		mocks    []mockTool
		input    input
		expected expected
	}{
		{
			name: "valid args",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return fmt.Sprintf("Results for: %s", args["query"]), nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {"query": "weather"}}`,
			},
			expected: expected{
				result:      `Results for: weather`,
				noToolError: true,
			},
		},
		{
			name: "missing required field",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "should not reach here", nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
		{
			name: "wrong type",
			mocks: []mockTool{
				{
					name:        "counter",
					description: "Count things",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"count": map[string]any{"type": "integer"},
						},
						"required": []any{"count"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "should not reach here", nil
					},
				},
			},
			input: input{
				content: `{"tool": "counter", "args": {"count": "not a number"}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
		{
			name: "no schema allows any args",
			mocks: []mockTool{
				{
					name:        "flexible",
					description: "A flexible tool",
					schema:      nil,
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "success", nil
					},
				},
			},
			input: input{
				content: `{"tool": "flexible", "args": {"anything": "works", "numbers": 123}}`,
			},
			expected: expected{
				result:      `success`,
				noToolError: true,
			},
		},
		{
			name: "multiple properties valid",
			mocks: []mockTool{
				{
					name:        "booking",
					description: "Book a flight",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"origin":      map[string]any{"type": "string"},
							"destination": map[string]any{"type": "string"},
							"passengers":  map[string]any{"type": "integer"},
						},
						"required": []any{"origin", "destination", "passengers"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "booked", nil
					},
				},
			},
			input: input{
				content: `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX", ` +
					`"passengers": 2}}`,
			},
			expected: expected{
				result:      `booked`,
				noToolError: true,
			},
		},
		{
			name: "multiple properties missing required",
			mocks: []mockTool{
				{
					name:        "booking",
					description: "Book a flight",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"origin":      map[string]any{"type": "string"},
							"destination": map[string]any{"type": "string"},
							"passengers":  map[string]any{"type": "integer"},
						},
						"required": []any{"origin", "destination", "passengers"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "booked", nil
					},
				},
			},
			input: input{
				content: `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX"}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			for _, mock := range tt.mocks {
				tool := gent.DefineTool(mock.name, mock.fn).Description(mock.description).
					ParameterSchema(mock.schema).MustBuild()
				tc.RegisterTool(tool)
			}

			result, err := executeTestCalls(tc, nil, tt.input.content, testFormat())
			require.NoError(t, err)

			if tt.expected.noToolError {
				assert.NoError(t, result.Results[0].Error)
				require.NotNil(t, result.Results[0])
				assert.Equal(
					t, tt.expected.result,
					getOutputString(result.Results[0].Output),
				)
			} else {
				require.Error(t, result.Results[0].Error)
				assert.Contains(
					t, result.Results[0].Error.Error(),
					tt.expected.errContains,
				)
				if tt.expected.resultIsNil {
					assert.Nil(t, result.Results[0].Output)
				}
			}
		})
	}
}

// JSONTimeTypedInput is used to test automatic type conversion to time.Time
type JSONTimeTypedInput struct {
	Date      time.Time `json:"date"`
	Timestamp time.Time `json:"timestamp"`
}

// JSONDurationTypedInput is used to test automatic type conversion to time.Duration
type JSONDurationTypedInput struct {
	Duration time.Duration `json:"duration"`
}

func TestRegistry_Execute_TimeConversion(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "time values converted correctly",
			input: input{
				content: `{"tool": "time_tool", "args": {"date": "2026-01-20", ` +
					`"timestamp": "2026-01-20T10:30:00Z"}}`,
			},
			expected: expected{
				output: `2026-01-20|2026-01-20T10:30:00Z`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()

			tool := gent.DefineTool("time_tool",
				func(ctx context.Context, input JSONTimeTypedInput) (string, error) {
					return input.Date.Format("2006-01-02") + "|" +
						input.Timestamp.Format(time.RFC3339), nil
				}).Description("Tool with time.Time input").ParameterSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date":      map[string]any{"type": "string"},
					"timestamp": map[string]any{"type": "string"},
				},
				"required": []any{"date", "timestamp"},
			}).MustBuild()
			tc.RegisterTool(tool)

			result, err := executeTestCalls(tc, nil, tt.input.content, testFormat())
			require.NoError(t, err)
			require.NoError(t, result.Results[0].Error)

			output := getOutputString(result.Results[0].Output)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

func TestRegistry_Execute_DurationConversion(t *testing.T) {
	type input struct {
		duration string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "hours and minutes",
			input:    input{duration: "1h30m"},
			expected: expected{output: `1h30m0s`},
		},
		{
			name:     "just minutes",
			input:    input{duration: "45m"},
			expected: expected{output: `45m0s`},
		},
		{
			name:     "with seconds",
			input:    input{duration: "2h15m30s"},
			expected: expected{output: `2h15m30s`},
		},
		{
			name:     "milliseconds",
			input:    input{duration: "500ms"},
			expected: expected{output: `500ms`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()

			tool := gent.DefineTool("duration_tool",
				func(ctx context.Context, input JSONDurationTypedInput) (string, error) {
					return input.Duration.String(), nil
				}).Description("Tool with time.Duration input").ParameterSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"duration": map[string]any{"type": "string"},
				},
				"required": []any{"duration"},
			}).MustBuild()
			tc.RegisterTool(tool)

			content := fmt.Sprintf(
				`{"tool": "duration_tool", "args": {"duration": "%s"}}`,
				tt.input.duration,
			)

			result, err := executeTestCalls(tc, nil, content, testFormat())
			require.NoError(t, err)
			require.NoError(t, result.Results[0].Error)

			output := getOutputString(result.Results[0].Output)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

// getOutputString extracts a string from a raw output value.
func getOutputString(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", output)
}

// -----------------------------------------------------------------------------
// BeforeToolCallSubscriber Argument Modification Tests
// -----------------------------------------------------------------------------

// jsonArgModifySubscriber is a test subscriber that modifies tool arguments.
// For tools using map[string]any as input, Args is typed as map[string]any.
type jsonArgModifySubscriber struct {
	modifyFunc func(args map[string]any) map[string]any
	called     bool
	seenArgs   map[string]any
}

func (h *jsonArgModifySubscriber) OnBeforeToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	h.called = true
	// Type-assert Args to map[string]any (the tool's input type)
	args, ok := event.Args.(map[string]any)
	if !ok {
		return
	}
	h.seenArgs = make(map[string]any)
	for k, v := range args {
		h.seenArgs[k] = v
	}
	if h.modifyFunc != nil {
		event.Args = h.modifyFunc(args)
	}
}

func TestRegistry_Execute_BeforeToolCallHook_ModifyArgs(t *testing.T) {
	type input struct {
		content    string
		modifyFunc func(args map[string]any) map[string]any
	}

	type expected struct {
		hookCalled   bool
		originalArgs map[string]any
		toolReceived map[string]any
		result       string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "subscriber modifies args in place",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					args["value"] = "modified"
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "modified"},
				result:       `received: modified`,
			},
		},
		{
			name: "subscriber replaces entire args map",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					return map[string]any{"value": "completely new"}
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "completely new"},
				result:       `received: completely new`,
			},
		},
		{
			name: "subscriber adds new args",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					args["extra"] = "added"
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "original", "extra": "added"},
				result:       `received: original`,
			},
		},
		{
			name: "subscriber removes args",
			input: input{
				content: `{"tool": "test", "args": {"value": "original", "remove": "this"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					delete(args, "remove")
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original", "remove": "this"},
				toolReceived: map[string]any{"value": "original"},
				result:       `received: original`,
			},
		},
		{
			name: "subscriber does not modify args",
			input: input{
				content:    `{"tool": "test", "args": {"value": "unchanged"}}`,
				modifyFunc: nil,
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "unchanged"},
				toolReceived: map[string]any{"value": "unchanged"},
				result:       `received: unchanged`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track what the tool actually received
			var receivedArgs map[string]any

			tc := New()
			tool := gent.DefineTool("test", func(ctx context.Context, args map[string]any) (string, error) {
				receivedArgs = args
				val, _ := args["value"].(string)
				return fmt.Sprintf("received: %s", val), nil
			}).Description("A test tool").ParameterSchema(nil).MustBuild()
			tc.RegisterTool(tool)

			// Create subscriber
			sub := &jsonArgModifySubscriber{modifyFunc: tt.input.modifyFunc}

			// Create execution context with events
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			registry := &jsonTestRegistry{subscriber: sub}
			execCtx.SetEventPublisher(registry)
			execCtx.IncrementIteration()

			// Execute
			result, err := executeTestCalls(tc, execCtx, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.True(t, sub.called, "subscriber should have been called")
			assert.Equal(t, tt.expected.originalArgs, sub.seenArgs,
				"subscriber should have seen original args")
			assert.Equal(t, tt.expected.toolReceived, receivedArgs,
				"tool should have received modified args")
			assert.NoError(t, result.Results[0].Error)
			assert.Equal(t, tt.expected.result, getOutputString(result.Results[0].Output))
		})
	}
}

func TestRegistry_Execute_BeforeToolCallHook_MultipleTools(t *testing.T) {
	type input struct {
		content    string
		modifyFunc func(toolName string, args map[string]any) map[string]any
	}

	type expected struct {
		tool1Received map[string]any
		tool2Received map[string]any
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "subscriber modifies args for each tool independently",
			input: input{
				content: `[
					{"tool": "tool1", "args": {"value": "first"}},
					{"tool": "tool2", "args": {"value": "second"}}
				]`,
				modifyFunc: func(toolName string, args map[string]any) map[string]any {
					args["modified_by"] = toolName + "_hook"
					return args
				},
			},
			expected: expected{
				tool1Received: map[string]any{"value": "first", "modified_by": "tool1_hook"},
				tool2Received: map[string]any{"value": "second", "modified_by": "tool2_hook"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tool1Received, tool2Received map[string]any

			tc := New()
			tc.RegisterTool(gent.DefineTool("tool1",
				func(ctx context.Context, args map[string]any) (string, error) {
					tool1Received = args
					return "ok1", nil
				}).Description("First tool").ParameterSchema(nil).MustBuild())
			tc.RegisterTool(gent.DefineTool("tool2",
				func(ctx context.Context, args map[string]any) (string, error) {
					tool2Received = args
					return "ok2", nil
				}).Description("Second tool").ParameterSchema(nil).MustBuild())

			// Create subscriber that tracks tool name
			sub := &jsonMultiToolSubscriber{modifyFunc: tt.input.modifyFunc}

			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			registry := &jsonTestRegistry{multiSubscriber: sub}
			execCtx.SetEventPublisher(registry)
			execCtx.IncrementIteration()

			result, err := executeTestCalls(tc, execCtx, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.NoError(t, result.Results[0].Error)
			assert.NoError(t, result.Results[1].Error)
			assert.Equal(t, tt.expected.tool1Received, tool1Received)
			assert.Equal(t, tt.expected.tool2Received, tool2Received)
		})
	}
}

// jsonMultiToolSubscriber is a test subscriber that modifies args based on tool name.
// For tools using map[string]any as input, Args is typed as map[string]any.
type jsonMultiToolSubscriber struct {
	modifyFunc func(toolName string, args map[string]any) map[string]any
}

func (h *jsonMultiToolSubscriber) OnBeforeToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	if h.modifyFunc != nil {
		// Type-assert Args to map[string]any (the tool's input type)
		args, ok := event.Args.(map[string]any)
		if !ok {
			return
		}
		event.Args = h.modifyFunc(event.ToolName, args)
	}
}

// jsonTestRegistry implements EventPublisher for testing.
type jsonTestRegistry struct {
	subscriber      *jsonArgModifySubscriber
	multiSubscriber *jsonMultiToolSubscriber
}

func (r *jsonTestRegistry) Dispatch(execCtx *gent.ExecutionContext, event gent.Event) {
	switch e := event.(type) {
	case *gent.BeforeToolCallEvent:
		if r.subscriber != nil {
			r.subscriber.OnBeforeToolCall(execCtx, e)
		}
		if r.multiSubscriber != nil {
			r.multiSubscriber.OnBeforeToolCall(execCtx, e)
		}
	}
}

func (r *jsonTestRegistry) MaxRecursion() int {
	return 10
}

// instructionsTool is a test tool that returns Instructions in its result.
type instructionsTool struct {
	name         string
	description  string
	schema       map[string]any
	result       string
	instructions string
}

func (t *instructionsTool) Name() string                 { return t.name }
func (t *instructionsTool) Description() string          { return t.description }
func (t *instructionsTool) Policy() string               { return "" }
func (t *instructionsTool) Strict() bool                 { return false }
func (t *instructionsTool) OutputSchema() map[string]any { return nil }
func (t *instructionsTool) Recover(context.Context, *gent.ToolRecoveryRequest) (
	*gent.ToolRecoveryResponse, error,
) {
	return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved}, nil
}
func (t *instructionsTool) ParameterSchema() map[string]any { return t.schema }
func (t *instructionsTool) Call(
	ctx context.Context,
	input map[string]any,
) (*gent.ToolResult[string], error) {
	return &gent.ToolResult[string]{
		Text:         t.result,
		Instructions: t.instructions,
	}, nil
}

func (t *instructionsTool) DeduplicateSummary(
	input map[string]any, output string,
) string {
	return ""
}

func TestRegistry_Execute_TracesToolCallErrors(t *testing.T) {
	t.Run("tool error increments all error counters", func(t *testing.T) {
		tc := New()
		tool := gent.DefineTool("failing_tool",
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("tool execution failed")
			}).Description("A test tool").ParameterSchema(nil).MustBuild()
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		execCtx.IncrementIteration()

		_, err := executeTestCalls(tc, execCtx, `{"tool": "failing_tool", "args": {}}`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"error total mismatch")
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorFor+"failing_tool"),
			"error for tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"error consecutive mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"failing_tool"),
			"error consecutive for tool mismatch")
	})

	t.Run("successful tool after failure resets consecutive gauges", func(t *testing.T) {
		tc := New()
		callCount := 0
		tool := gent.DefineTool("test_tool",
			func(ctx context.Context, args map[string]any) (string, error) {
				callCount++
				if callCount == 1 {
					return "", errors.New("first call fails")
				}
				return "success", nil
			}).Description("A test tool").ParameterSchema(nil).MustBuild()
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// First iteration: tool fails
		execCtx.IncrementIteration()
		_, err := executeTestCalls(tc, execCtx, `{"tool": "test_tool", "args": {}}`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"after first call: error total mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"after first call: error consecutive mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"test_tool"),
			"after first call: error consecutive for tool mismatch")

		// Second iteration: tool succeeds
		execCtx.IncrementIteration()
		_, err = executeTestCalls(tc,
			execCtx,
			`{"tool": "test_tool", "args": {}}`,
			testFormat(),
		)
		require.NoError(t, err)

		// Total should remain 1, but consecutive should be reset to 0
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"after second call: error total should not change")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"after second call: error consecutive should be reset")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"test_tool"),
			"after second call: error consecutive for tool should be reset")
	})

	t.Run("multiple consecutive failures accumulate", func(t *testing.T) {
		tc := New()
		tool := gent.DefineTool("always_fails",
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("always fails")
			}).Description("A test tool").ParameterSchema(nil).MustBuild()
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// Three consecutive failures
		for i := 1; i <= 3; i++ {
			execCtx.IncrementIteration()
			_, err := executeTestCalls(tc, execCtx, `{"tool": "always_fails", "args": {}}`, testFormat())
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, int64(i),
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"after iteration %d: error total mismatch", i)
			assert.Equal(t, float64(i),
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"after iteration %d: error consecutive mismatch", i)
		}
	})
}

func TestRegistry_Execute_TracesToolCallErrors_UnknownTool(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		errorTotal       int64
		errorFor         int64
		errorConsecutive float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "unknown tool increments error counters",
			input: input{
				content: `{"tool": "nonexistent", "args": {}}`,
			},
			expected: expected{
				errorTotal:       1,
				errorFor:         1,
				errorConsecutive: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			// Don't register any tools

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			_, err := executeTestCalls(tc,
				execCtx, tt.input.content, testFormat(),
			)
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCalls),
				"tool calls mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsFor+"nonexistent"),
				"tool calls for tool mismatch")
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"error total mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsErrorFor+"nonexistent"),
				"error for tool mismatch")
			assert.Equal(t, tt.expected.errorConsecutive,
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"error consecutive mismatch")
		})
	}
}

func TestRegistry_Execute_TracesToolCallErrors_SchemaValidation(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		errorTotal       int64
		errorFor         int64
		errorConsecutive float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "schema validation error increments error counters",
			input: input{
				content: `{"tool": "validated_tool", "args": {}}`,
			},
			expected: expected{
				errorTotal:       1,
				errorFor:         1,
				errorConsecutive: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			tool := gent.DefineTool("validated_tool",
				func(ctx context.Context, args map[string]any) (string, error) {
					return "should not reach", nil
				}).Description("A tool with required schema").ParameterSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"required_field": map[string]any{"type": "string"},
				},
				"required": []any{"required_field"},
			}).MustBuild()
			tc.RegisterTool(tool)

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			_, err := executeTestCalls(tc,
				execCtx, tt.input.content, testFormat(),
			)
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCalls),
				"tool calls mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsFor+"validated_tool"),
				"tool calls for tool mismatch")
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"error total mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsErrorFor+"validated_tool"),
				"error for tool mismatch")
			assert.Equal(t, tt.expected.errorConsecutive,
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"error consecutive mismatch")
		})
	}
}

func TestRegistry_Execute_InvalidAttemptsCountAgainstLimits(t *testing.T) {
	type input struct {
		content  string
		setup    func(tc *Registry)
		limitKey gent.StatKey
	}

	type expected struct {
		exceededLimit gent.Limit
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "unknown tool trips per-tool call limit",
			input: input{
				content:  `{"tool": "nonexistent", "args": {}}`,
				setup:    func(tc *Registry) {},
				limitKey: gent.SCToolCallsFor + "nonexistent",
			},
			expected: expected{exceededLimit: gent.Limit{
				Type:     gent.LimitExactKey,
				Key:      gent.SCToolCallsFor + "nonexistent",
				MaxValue: 0,
			}},
		},
		{
			name: "schema validation failure trips per-tool call limit",
			input: input{
				content: `{"tool": "validated_tool", "args": {}}`,
				setup: func(tc *Registry) {
					tc.RegisterTool(gent.DefineTool("validated_tool", func(
						ctx context.Context,
						args map[string]any,
					) (string, error) {
						return "should not reach", nil
					}).Description("A tool with required schema").ParameterSchema(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"required_field": map[string]any{"type": "string"},
						},
						"required": []any{"required_field"},
					}).MustBuild())
				},
				limitKey: gent.SCToolCallsFor + "validated_tool",
			},
			expected: expected{exceededLimit: gent.Limit{
				Type:     gent.LimitExactKey,
				Key:      gent.SCToolCallsFor + "validated_tool",
				MaxValue: 0,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			tt.input.setup(tc)
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.SetLimits([]gent.Limit{tt.expected.exceededLimit})

			_, err := executeTestCalls(tc, execCtx, tt.input.content, testFormat())

			require.NoError(t, err)
			require.NotNil(t, execCtx.ExceededLimit())
			assert.Equal(t, tt.expected.exceededLimit, *execCtx.ExceededLimit())
			assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.SCToolCalls))
			assert.Equal(t, int64(1), execCtx.Stats().GetCounter(tt.input.limitKey))
			assert.Error(t, execCtx.Context().Err())
		})
	}
}

func TestRegistry_Execute_TracesToolCallErrors_MultipleTools(t *testing.T) {
	t.Run("multiple tools with mixed success/failure in single execute", func(t *testing.T) {
		tc := New()
		tc.RegisterTool(gent.DefineTool("success_tool",
			func(ctx context.Context, args map[string]any) (string, error) {
				return "success", nil
			}).Description("A successful tool").ParameterSchema(nil).MustBuild())
		tc.RegisterTool(gent.DefineTool("failing_tool",
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("tool failed")
			}).Description("A failing tool").ParameterSchema(nil).MustBuild())

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		execCtx.IncrementIteration()

		content := `[
			{"tool": "success_tool", "args": {}},
			{"tool": "failing_tool", "args": {}}
		]`
		_, err := executeTestCalls(tc, execCtx, content, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"error total mismatch")
		assert.Equal(t, int64(0),
			stats.GetCounter(gent.SCToolCallsErrorFor+"success_tool"),
			"error for success_tool mismatch")
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorFor+"failing_tool"),
			"error for failing_tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"error consecutive mismatch")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"success_tool"),
			"error consecutive for success_tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"failing_tool"),
			"error consecutive for failing_tool mismatch")
	})

	t.Run("per-tool consecutive resets independently across executions", func(t *testing.T) {
		tc := New()
		tool1CallCount := 0
		tool2CallCount := 0
		tc.RegisterTool(gent.DefineTool("tool1",
			func(ctx context.Context, args map[string]any) (string, error) {
				tool1CallCount++
				if tool1CallCount == 1 {
					return "", errors.New("tool1 fails first time")
				}
				return "tool1 success", nil
			}).Description("First tool").ParameterSchema(nil).MustBuild())
		tc.RegisterTool(gent.DefineTool("tool2",
			func(ctx context.Context, args map[string]any) (string, error) {
				tool2CallCount++
				// Tool2 always fails
				return "", errors.New("tool2 always fails")
			}).Description("Second tool").ParameterSchema(nil).MustBuild())

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// First iteration: both tools fail
		execCtx.IncrementIteration()
		_, err := executeTestCalls(tc, execCtx, `[
			{"tool": "tool1", "args": {}},
			{"tool": "tool2", "args": {}}
		]`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(2),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"iteration 1: total errors")
		assert.Equal(t, float64(2),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"iteration 1: consecutive errors")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool1"),
			"iteration 1: tool1 consecutive")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool2"),
			"iteration 1: tool2 consecutive")

		// Second iteration: tool1 succeeds, tool2 still fails
		execCtx.IncrementIteration()
		_, err = executeTestCalls(tc, execCtx, `[
			{"tool": "tool1", "args": {}},
			{"tool": "tool2", "args": {}}
		]`, testFormat())
		require.NoError(t, err)

		// Total increments by 1 (only tool2 fails)
		// Global consecutive resets then increments to 1
		// tool1 consecutive should be reset
		// tool2 consecutive should be 2
		assert.Equal(t, int64(3),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"iteration 2: total errors")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"iteration 2: consecutive errors (reset by tool1 success, then +1 by tool2)")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool1"),
			"iteration 2: tool1 consecutive reset")
		assert.Equal(t, float64(2),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool2"),
			"iteration 2: tool2 consecutive accumulated")
	})
}

func TestRegistry_Execute_WithInstructions(t *testing.T) {
	type input struct {
		content      string
		result       string
		instructions string
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "tool without instructions has flat output",
			input: input{
				content:      `{"tool": "test", "args": {}}`,
				result:       "search result",
				instructions: "",
			},
			expected: expected{
				text: "<test>\nsearch result\n</test>",
			},
		},
		{
			name: "tool with instructions has nested sections",
			input: input{
				content:      `{"tool": "test", "args": {}}`,
				result:       "customer info",
				instructions: "Remember to verify customer ID before proceeding.",
			},
			expected: expected{
				text: "<test>\n<result>\ncustomer info\n</result>\n" +
					"<instructions>\nRemember to verify customer ID before proceeding.\n" +
					"</instructions>\n</test>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New()
			tool := &instructionsTool{
				name:         "test",
				description:  "A test tool",
				result:       tt.input.result,
				instructions: tt.input.instructions,
			}
			tc.RegisterTool(tool)

			result, err := executeTestCalls(tc, nil, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.Equal(t, tt.expected.text, collectText(result))
		})
	}
}

func TestRegistry_UnknownToolErrorMessage(t *testing.T) {
	tc := New()
	result, err := executeTestCalls(tc,
		nil,
		`{"tool": "nonexistent", "args": {}}`,
		testFormat(),
	)
	require.NoError(t, err)
	assert.ErrorIs(
		t, result.Results[0].Error,
		gent.ErrUnknownTool,
	)
	assert.Equal(t, `<nonexistent>
Error: unknown tool "nonexistent". Use a function from the current native tool definitions.
</nonexistent>`, collectText(result))
}

func (t *instructionsTool) Domain() string             { return "" }
func (t *instructionsTool) Categories() []string       { return nil }
func (t *instructionsTool) Keywords() []string         { return nil }
func (t *instructionsTool) SyntheticQueries() []string { return nil }

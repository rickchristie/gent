package toolchain

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/stretchr/testify/assert"
)

func TestNativeInputUsesJSONFieldNames(t *testing.T) {
	type request struct {
		Delay time.Duration `json:"default_delay"`
		Count int           `json:"delay"`
	}
	type input struct {
		arguments string
		hook      *request
	}
	type expected struct {
		value request
		calls int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"tag matches another Go name", input{arguments: `{"default_delay":"1s","delay":2}`},
			expected{request{time.Second, 2}, 1}},
		{"hook changes both fields", input{`{"default_delay":"1s","delay":2}`,
			&request{2 * time.Second, 3}}, expected{request{2 * time.Second, 3}, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := expected{}
			tool := gent.DefineTool("schedule", func(_ context.Context, value request) (
				*gent.ToolExecutionResponse, error,
			) {
				actual.value, actual.calls = value, actual.calls+1
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().Subscribe(&replaceInput{value: *test.input.hook}))
			}
			chain := New().RegisterTool(tool)
			record := executeNativeTestCall(t, chain, ctx, nativeCall("schedule", test.input.arguments))
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, record.Execution())
			restored, err := ctx.Codecs().Decode(*record.Input())
			assert.NoError(t, err)
			assert.Equal(t, test.expected.value, restored)
		})
	}
}

func TestNativeInputChecksRenamedHookField(t *testing.T) {
	type request struct {
		Delay time.Duration `json:"default_delay"`
		Label string        `json:"delay"`
	}
	type input struct{ hook *request }
	type expected struct {
		value   request
		calls   int
		details string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"original input", input{}, expected{request{time.Second, "ready"}, 1, ""}},
		{"valid changed label", input{&request{time.Second, "1s"}},
			expected{request{time.Second, "1s"}, 1, ""}},
		{"duration must not hide invalid label", input{&request{time.Second, "wrong"}},
			expected{details: "args.delay: must satisfy pattern (^(ready|1s)$)."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var value request
			calls := 0
			tool := gent.DefineTool("schedule", func(_ context.Context, input request) (
				*gent.ToolExecutionResponse, error,
			) {
				value, calls = input, calls+1
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).ParameterSchema(map[string]any{"type": "object", "properties": map[string]any{
				"default_delay": map[string]any{"type": "string"},
				"delay":         map[string]any{"type": "string", "pattern": "^(ready|1s)$"},
			}}).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().Subscribe(&replaceInput{value: *test.input.hook}))
			}
			chain := New().RegisterTool(tool)
			record := executeNativeTestCall(t, chain, ctx,
				nativeCall("schedule", `{"default_delay":"1s","delay":"ready"}`))
			assert.Equal(t, test.expected.calls, calls)
			assert.Equal(t, test.expected.value, value)
			want := &gent.ToolExecutionResponse{Status: gent.ToolExecuted}
			if test.expected.details != "" {
				want.Status = gent.ToolInvalidArguments
				want.Instructions = chain.GetToolSchema("schedule").Correction(
					"schedule", test.expected.details)
			}
			assert.Equal(t, want, record.Execution())
			assert.Equal(t, gent.PrepareExecutionParts(want, gent.DefaultSteeringMessages()),
				record.Response().Parts())
		})
	}
}

type EmbeddedDelay struct {
	Delay time.Duration `json:"delay"`
}

// The embedded pointer type is exported so encoding/json can allocate it on input.
type EmbeddedTimeout struct {
	Timeout time.Duration `json:"timeout"`
}

func TestNativeInputUsesEmbeddedJSONFields(t *testing.T) {
	type request struct {
		EmbeddedDelay
		*EmbeddedTimeout
		Count int `json:"delay"`
	}
	type input struct{ hook *request }
	type expected struct {
		value   request
		calls   int
		details string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	value := request{EmbeddedTimeout: &EmbeddedTimeout{time.Second}, Count: 2}
	changed := request{EmbeddedDelay{9 * time.Minute}, &EmbeddedTimeout{2 * time.Second}, 3}
	cases := []testCase{
		{"allocate embedded pointer and prefer outer field", input{}, expected{value, 1, ""}},
		{"ignore shadowed duration", input{&changed}, expected{changed, 1, ""}},
		{"nil embedded pointer omits required field", input{&request{Count: 2}},
			expected{details: "args.timeout: required field is missing."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var received request
			calls := 0
			tool := gent.DefineTool("schedule", func(_ context.Context, input request) (
				*gent.ToolExecutionResponse, error,
			) {
				received, calls = input, calls+1
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).ParameterSchema(map[string]any{"type": "object", "required": []string{"delay", "timeout"},
				"properties": map[string]any{
					"delay":   map[string]any{"type": "integer"},
					"timeout": map[string]any{"type": "string"},
				},
			}).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().Subscribe(&replaceInput{value: *test.input.hook}))
			}
			chain := New().RegisterTool(tool)
			record := executeNativeTestCall(t, chain, ctx,
				nativeCall("schedule", `{"delay":2,"timeout":"1s"}`))
			assert.Equal(t, test.expected.calls, calls)
			assert.Equal(t, test.expected.value, received)
			want := &gent.ToolExecutionResponse{Status: gent.ToolExecuted}
			if test.expected.details != "" {
				want.Status = gent.ToolInvalidArguments
				want.Instructions = chain.GetToolSchema("schedule").Correction(
					"schedule", test.expected.details)
			}
			assert.Equal(t, want, record.Execution())
		})
	}
}

func TestTransformArgsMatchesJSONDecodingNames(t *testing.T) {
	type request struct {
		First  time.Duration `json:"delay"`
		Second int           `json:"Delay"`
		Hidden time.Duration `json:"renamed"`
	}
	type input struct{ args map[string]any }
	type expected struct {
		value request
		err   error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"exact name before folded name", input{map[string]any{"Delay": 2, "delay": "1s"}},
			expected{value: request{First: time.Second, Second: 2}}},
		{"first folded JSON name", input{map[string]any{"DELAY": "2s"}},
			expected{value: request{First: 2 * time.Second}}},
		{"renamed Go name stays unknown", input{map[string]any{"Hidden": "1s"}}, expected{}},
	}
	tool := gent.DefineTool("schedule", func(context.Context, request) (
		*gent.ToolExecutionResponse, error,
	) {
		panic("conversion must not call the tool")
	}).MustBuild()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value, err := TransformArgsReflect(tool, test.input.args)
			assert.Equal(t, test.expected.err, err)
			assert.Equal(t, test.expected.value, value)
		})
	}
}

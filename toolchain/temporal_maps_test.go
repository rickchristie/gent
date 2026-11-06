package toolchain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/stretchr/testify/assert"
)

type durationMapKey struct{ Name string }

func (key durationMapKey) MarshalText() ([]byte, error) {
	return []byte("slot/" + key.Name), nil
}

func (key *durationMapKey) UnmarshalText(data []byte) error {
	name, found := strings.CutPrefix(string(data), "slot/")
	if !found {
		return fmt.Errorf("invalid schedule key %q", data)
	}
	key.Name = name
	return nil
}

func TestNativeInputValidatesDurationMapKeys(t *testing.T) {
	type request struct {
		Signed   map[int]time.Duration                    `json:"signed"`
		Unsigned map[uint64][]*time.Duration              `json:"unsigned"`
		Text     map[durationMapKey]map[int]time.Duration `json:"text"`
		Labels   map[string]time.Duration                 `json:"labels"`
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
	delay := 2 * time.Second
	value := request{
		Signed:   map[int]time.Duration{1: time.Second},
		Unsigned: map[uint64][]*time.Duration{18446744073709551615: {&delay, nil}},
		Text:     map[durationMapKey]map[int]time.Duration{{"a"}: {2: 3 * time.Second}},
		Labels:   map[string]time.Duration{"next": 4 * time.Second},
	}
	invalidSigned := value
	invalidSigned.Signed = map[int]time.Duration{1: -time.Second}
	invalidText := value
	invalidText.Text = map[durationMapKey]map[int]time.Duration{{"a"}: {2: -time.Second}}
	cases := []testCase{
		{"integer text and string keys", input{}, expected{value, 1, ""}},
		{"hook preserves typed keys", input{&value}, expected{value, 1, ""}},
		{"invalid integer-keyed hook value", input{&invalidSigned},
			expected{details: "args.signed[1]: must satisfy pattern (^[0-9]+s$)."}},
		{"invalid nested text-keyed hook value", input{&invalidText},
			expected{details: "args.text.slot/a[2]: must satisfy pattern (^[0-9]+s$)."}},
	}
	const arguments = `{
"signed":{"1":"1s"},
"unsigned":{"18446744073709551615":["2s",null]},
"text":{"slot/a":{"2":"3s"}},
"labels":{"next":"4s"}
}`
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var received request
			calls := 0
			mapSchema := func(item map[string]any) map[string]any {
				return map[string]any{"type": "object", "additionalProperties": item}
			}
			duration := map[string]any{"type": "string", "pattern": "^[0-9]+s$"}
			tool := gent.DefineTool("schedule", func(_ context.Context, input request) (
				*gent.ToolExecutionResponse, error,
			) {
				received, calls = input, calls+1
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).ParameterSchema(map[string]any{"type": "object", "properties": map[string]any{
				"signed": mapSchema(duration),
				"unsigned": mapSchema(map[string]any{"type": "array", "items": map[string]any{
					"anyOf": []any{duration, map[string]any{"type": "null"}},
				}}),
				"text":   mapSchema(mapSchema(duration)),
				"labels": mapSchema(duration),
			}}).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().Subscribe(&replaceInput{value: *test.input.hook}))
			}
			chain := New().RegisterTool(tool)
			record := executeNativeTestCall(t, chain, ctx, nativeCall("schedule", arguments))
			assert.Equal(t, test.expected.calls, calls)
			assert.Equal(t, test.expected.value, received)
			want := &gent.ToolExecutionResponse{Status: gent.ToolExecuted}
			if test.expected.details != "" {
				want.Status = gent.ToolInvalidArguments
				want.Instructions = chain.GetToolSchema("schedule").Correction(
					"schedule", test.expected.details)
			}
			assert.Equal(t, want, record.Execution())
			assert.Equal(t, gent.PrepareExecutionParts(want, gent.DefaultSteeringMessages()),
				record.Response().Parts())
			if calls == 1 {
				restored, err := ctx.Codecs().Decode(*record.Input())
				assert.NoError(t, err)
				assert.Equal(t, test.expected.value, restored)
			}
		})
	}
}

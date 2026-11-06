package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/stretchr/testify/assert"
)

// Wire arrays include a count before their items. Their indices do not match the Go slice.
type countedInput []int

func (v countedInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(append([]int{len(v)}, v...))
}

func (v *countedInput) UnmarshalJSON(data []byte) error {
	var wire []int
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire) == 0 || wire[0] != len(wire)-1 {
		return errors.New("invalid item count")
	}
	*v = wire[1:]
	return nil
}

type countedDelays []time.Duration

func (v countedDelays) MarshalJSON() ([]byte, error) {
	wire := []string{strconv.Itoa(len(v)) + "s"}
	for _, delay := range v {
		wire = append(wire, delay.String())
	}
	return json.Marshal(wire)
}

func (v *countedDelays) UnmarshalJSON(data []byte) error {
	var wire []string
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire) == 0 || wire[0] != strconv.Itoa(len(wire)-1)+"s" {
		return errors.New("invalid delay count")
	}
	*v = make(countedDelays, len(wire)-1)
	for i, value := range wire[1:] {
		delay, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		(*v)[i] = delay
	}
	return nil
}

// This JSON property has the same name as its Go field but uses seconds, not a duration string.
type encodedWindow struct {
	Delay time.Duration `json:"delay"`
}

func (v *encodedWindow) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]int64{"delay": int64(v.Delay / time.Second)})
}

func (v *encodedWindow) UnmarshalJSON(data []byte) error {
	var wire map[string]int64
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	v.Delay = time.Duration(wire["delay"]) * time.Second
	return nil
}

type customWireInput struct {
	Items  countedInput   `json:"items"`
	Delays countedDelays  `json:"delays"`
	Window *encodedWindow `json:"window"`
	Delay  time.Duration  `json:"delay"`
}

func TestCustomInputRepresentationValidation(t *testing.T) {
	type input struct {
		arguments string
		hook      *customWireInput
	}
	type expected struct {
		calls    int
		received customWireInput
		details  string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const arguments = `{"items":[1,7],"delays":["1s","2s"],"window":{"delay":2},"delay":"1m"}`
	value := customWireInput{countedInput{7}, countedDelays{2 * time.Second},
		&encodedWindow{2 * time.Second}, time.Minute}
	changed := value
	changed.Items = countedInput{7, 8}
	rejected := value
	rejected.Items = countedInput{11}
	windowRejected := value
	windowRejected.Window = &encodedWindow{11 * time.Second}
	cases := []testCase{
		{"valid custom representations", input{arguments: arguments}, expected{1, value, ""}},
		{"changed custom input", input{arguments, &changed}, expected{1, changed, ""}},
		{"invalid effective array", input{arguments, &rejected},
			expected{details: "args.items[1]: must satisfy maximum (10/1)."}},
		{"invalid effective object", input{arguments, &windowRejected},
			expected{details: "args.window.delay: must satisfy maximum (10/1)."}},
		{"invalid supplied array", input{arguments: `{"items":[1,11],"delays":["1s","2s"],
"window":{"delay":2},"delay":"1m"}`},
			expected{details: "args.items[1]: must satisfy maximum (10/1)."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var received customWireInput
			calls := 0
			tool := gent.DefineTool("read", func(_ context.Context, request customWireInput) (
				*gent.ToolExecutionResponse, error,
			) {
				received = request
				calls++
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).ParameterSchema(map[string]any{
				"type": "object", "properties": map[string]any{
					"items": map[string]any{"type": "array", "items": map[string]any{
						"type": "integer", "maximum": 10}},
					"delays": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"window": map[string]any{"type": "object", "properties": map[string]any{
						"delay": map[string]any{"type": "integer", "maximum": 10}}},
					"delay": map[string]any{"type": "string"},
				},
			}).MustBuild()
			chain := New().RegisterTool(tool)
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().Subscribe(&replaceInput{value: *test.input.hook}))
			}
			record := executeNativeTestCall(t, chain, ctx, nativeCall("read", test.input.arguments))
			assert.Equal(t, test.expected.calls, calls)
			assert.Equal(t, test.expected.received, received)
			want := &gent.ToolExecutionResponse{Status: gent.ToolExecuted}
			if test.expected.details != "" {
				want.Status = gent.ToolInvalidArguments
				want.Instructions = chain.GetToolSchema("read").Correction("read", test.expected.details)
			}
			assert.Equal(t, want, record.Execution())
			assert.Equal(t, gent.PrepareExecutionParts(want, gent.DefaultSteeringMessages()),
				record.Response().Parts())
			if calls == 1 {
				restored, err := ctx.Codecs().Decode(*record.Input())
				assert.NoError(t, err)
				assert.Equal(t, test.expected.received, restored)
			}
		})
	}
}

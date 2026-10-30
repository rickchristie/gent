package gent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCodecRegistryRoundTrip(t *testing.T) {
	type customValue struct {
		Id     uint64
		Values map[string]any
	}
	type namedMap map[string]int64
	type namedSlice []uint64
	type input struct{ value any }
	type expected struct {
		value     any
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	registry := NewCodecRegistry()
	assert.NoError(t, registry.RegisterType("test/value", 2, customValue{}))
	assert.NoError(t, registry.RegisterType("test/map", 1, namedMap{}))
	assert.NoError(t, registry.RegisterType("test/slice", 1, namedSlice{}))
	values := []any{
		nil, []string(nil), []string{}, map[string]any(nil), map[string]any{},
		map[string]any{"id": uint64(1<<63 + 7), "count": int32(12), "decimal": json.Number("1.20")},
		namedMap{"large": 1<<62 + 7}, namedSlice{1<<63 + 8}, json.RawMessage(`{"value":7}`),
		json.RawMessage(nil), json.RawMessage(`null`), EncodedValue{},
		json.RawMessage(` {
  "escaped": "\u0061<&>", "number": 1.20, "nested": { "keep": true }
} `),
		[2][2]int64{{1, 2}, {3, 1<<62 + 9}},
		CachedContent{
			ContentPart:  BinaryContent{MIMEType: "image/png", Data: []byte{0, 255, 7}},
			CacheControl: &CacheControl{Type: "ephemeral", Duration: 15 * time.Minute},
		},
		ImageURLContent{URL: "https://example.test/original.png"},
		customValue{Id: 1<<63 + 19, Values: map[string]any{"nested": []any{true, "text"}}},
		NewTask(TextContent{Text: "original task"}, TextContent{Text: "media"}),
		&HistoryEntry{Origin: RecordOriginal, ExpireAfterIteration: 5,
			Messages: []*MessageContent{{Role: ChatMessageTypeHuman,
				Parts: []ContentPart{TextContent{Text: "policy P-10: exact original text"}},
				Metadata: map[MessageContentMetadataKey]any{MMKToolChainResult: &ToolChainResult{
					Results: []*ToolCallResult{{Name: "policy", Args: map[string]any{"id": "P-10"},
						Input: customValue{
							Id: 12,
						}, Output: "exact original text", Text: "rendered"}}}}}},
			Metadata: map[RecordMetadataKey]any{
				RecordMetadataKey("custom"): []any{int64(100), true},
			}},
		time.Date(2026, 9, 11, 5, 0, 0, 12, time.UTC), time.Duration(42 * time.Minute),
		CallOptions{Model: "fixture", Temperature: func() *float64 { v := 0.4; return &v }(),
			Tools: []ToolDefinition{{Type: "function", Function: &FunctionDefinition{
				Name: "lookup", Parameters: map[string]any{"type": "object"}}}}},
	}
	var cases []testCase
	for i, value := range values {
		cases = append(cases, testCase{name: fmt.Sprintf("%d_%T", i, value), input: input{value},
			expected: expected{value: value}})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := registry.Encode(tt.input.value)
			if !assert.NoError(t, err) {
				return
			}
			data, err := json.Marshal(encoded)
			if !assert.NoError(t, err) {
				return
			}
			var wire EncodedValue
			if !assert.NoError(t, json.Unmarshal(data, &wire)) {
				return
			}
			actual, err := registry.Decode(wire)
			assert.Equal(t, tt.expected.value, actual)
			assert.NoError(t, err)
			cloned, err := registry.Clone(tt.input.value)
			assert.NoError(t, err)
			assert.Equal(t, actual, cloned)
		})
	}
}

func TestCodecRegistryRejectsUnsupportedValues(t *testing.T) {
	type unknown struct{ Text string }
	type input struct{ value any }
	type expected struct{ errorText string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	cases := []testCase{
		{"unknown", input{unknown{}}, expected{"gent: no codec for gent.unknown"}},
		{"function", input{func() {}}, expected{"gent: unsupported value func()"}},
		{"channel", input{make(chan int)}, expected{"gent: unsupported value chan int"}},
		{"nan", input{math.NaN()}, expected{"gent: non-finite number"}},
		{"cycle", input{cyclic}, expected{"gent: cyclic value map[string]interface {}"}},
		{"invalid raw JSON", input{json.RawMessage(`{"broken":`)},
			expected{"gent: invalid raw JSON"}},
		{"empty raw JSON", input{json.RawMessage{}}, expected{"gent: invalid raw JSON"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCodecRegistry().Encode(tt.input.value)
			assert.EqualError(t, err, tt.expected.errorText)
			_, err = NewCodecRegistry().Clone(tt.input.value)
			assert.EqualError(t, err, tt.expected.errorText)
		})
	}
}

func TestSavedErrorRoundTrip(t *testing.T) {
	original := fmt.Errorf("request stopped: %w", errors.Join(context.Canceled, ErrInvalidToolArgs))
	registry := NewCodecRegistry()
	encoded, err := registry.Encode(original)
	if !assert.NoError(t, err) {
		return
	}
	value, err := registry.Decode(encoded)
	if !assert.NoError(t, err) {
		return
	}
	restored := value.(error)
	assert.Equal(t, original.Error(), restored.Error())
	assert.ErrorIs(t, restored, context.Canceled)
	assert.ErrorIs(t, restored, ErrInvalidToolArgs)
	assert.False(t, errors.Is(restored, context.DeadlineExceeded))
}

func TestSavedFrameworkErrorClassifications(t *testing.T) {
	type input struct{ err error }
	type expected struct{ code string }
	type testCase struct {
		input    input
		expected expected
	}
	cases := []testCase{
		{input{ErrEventGap}, expected{"event_gap"}},
		{input{ErrWrongRun}, expected{"wrong_run"}},
		{input{ErrHistoryConflict}, expected{"history_conflict"}},
		{input{ErrPersistenceStopped}, expected{"persistence_stopped"}},
		{input{ErrToolPlanComplete}, expected{"tool_plan_complete"}},
		{input{ErrReplayUnavailable}, expected{"replay_unavailable"}},
	}
	for _, tt := range cases {
		t.Run(tt.expected.code, func(t *testing.T) {
			value, err := NewCodecRegistry().Clone(tt.input.err)
			assert.NoError(t, err)
			assert.Equal(t, &SavedError{
				Message: tt.input.err.Error(), Codes: []string{tt.expected.code},
			}, value)
			assert.ErrorIs(t, value.(error), tt.input.err)
		})
	}
}

type privateHistoryError struct {
	message string
	detail  []byte
}

func (e *privateHistoryError) Error() string     { return e.message }
func (e *privateHistoryError) ErrorCode() string { return "fixture:policy_error" }

type privateErrorCodec struct{}

func (privateErrorCodec) Type() string { return "fixture/private-error" }
func (privateErrorCodec) Version() int { return 3 }
func (privateErrorCodec) Encode(value any) (json.RawMessage, error) {
	err := value.(*privateHistoryError)
	return json.Marshal([][]byte{[]byte(err.message), err.detail})
}
func (privateErrorCodec) Decode(data json.RawMessage) (any, error) {
	var fields [][]byte
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if len(fields) != 2 {
		return nil, errors.New("invalid private error fields")
	}
	return &privateHistoryError{message: string(fields[0]), detail: fields[1]}, nil
}

func TestCustomErrorCodecPreservesPrivateFields(t *testing.T) {
	registry := NewCodecRegistry()
	assert.NoError(t, registry.Register((*privateHistoryError)(nil), privateErrorCodec{}))
	original := &privateHistoryError{message: "policy unavailable", detail: []byte{0, 255, 7}}
	encoded, err := registry.Encode(original)
	assert.NoError(t, err)
	original.detail[0] = 99
	fresh := NewCodecRegistry()
	assert.NoError(t, fresh.Register((*privateHistoryError)(nil), privateErrorCodec{}))
	value, err := fresh.Decode(encoded)
	assert.NoError(t, err)
	want := &privateHistoryError{message: "policy unavailable", detail: []byte{0, 255, 7}}
	assert.Equal(t, want, value)
	assert.Equal(t, 3, encoded.Version)
	// Terminal outcomes preserve classification even without the optional private value codec.
	saved := SaveError(want)
	assert.Equal(t, &SavedError{
		Message: "policy unavailable", Codes: []string{"fixture:policy_error"},
	}, saved)
	assert.ErrorIs(t, saved, &privateHistoryError{})
}

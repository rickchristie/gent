package gent_test

import (
	"encoding/json"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type dataCustomKey struct{ Currency string }
type dataCustomMap map[dataCustomKey]float64
type dataPointerMap map[dataCustomKey]float64
type dataCustomArray []float64
type dataPointerStruct struct {
	Amount float64 `json:"amount"`
}

func (dataCustomMap) MarshalJSON() ([]byte, error)   { return []byte(`{"amount":2}`), nil }
func (*dataPointerMap) MarshalJSON() ([]byte, error) { return []byte(`{"amount":2}`), nil }
func (dataCustomArray) MarshalJSON() ([]byte, error) { return []byte(`[7,2.0]`), nil }
func (*dataPointerStruct) MarshalJSON() ([]byte, error) {
	return []byte(`{"amount":2}`), nil
}

func TestDataCaptureCustomEncodingBoundary(t *testing.T) {
	type input struct{ value any }
	type expected struct {
		json  string
		value any
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	value := dataCustomMap{{Currency: "USD"}: 99}
	pointer := dataPointerMap{{Currency: "USD"}: 99}
	custom := map[string]any{"amount": int64(2)}
	cases := []testCase{
		{"value encoder", input{value}, expected{`{"amount":2}`, custom}},
		{"pointer to value encoder", input{&value}, expected{`{"amount":2}`, custom}},
		{"pointer encoder", input{&pointer}, expected{`{"amount":2}`, custom}},
		{"nil value encoder", input{dataCustomMap(nil)}, expected{`{"amount":2}`, custom}},
		{"nil pointer encoder", input{(*dataPointerMap)(nil)}, expected{`null`, nil}},
		{"nested encoders", input{map[string]any{"values": []any{value, &pointer}}},
			expected{`{"values":[{"amount":2},{"amount":2}]}`,
				map[string]any{"values": []any{custom, custom}}}},
		{"changed array shape and number meaning", input{dataCustomArray{99}},
			expected{`[7,2.0]`, []any{int64(7), json.Number("2.0")}}},
		{"changed struct field meaning", input{&dataPointerStruct{99}},
			expected{`{"amount":2}`, custom}},
		{"nonaddressable struct uses ordinary encoding", input{dataPointerStruct{99}},
			expected{`{"amount":99}`, map[string]any{"amount": float64(99)}}},
		{"addressable slice element uses pointer encoder", input{[]dataPointerStruct{{99}}},
			expected{`[{"amount":2}]`, []any{custom}}},
		{"nonaddressable map element uses ordinary encoding",
			input{map[string]dataPointerStruct{"value": {99}}},
			expected{`{"value":{"amount":99}}`,
				map[string]any{"value": map[string]any{"amount": float64(99)}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.input.value)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.json, string(encoded))
			part, err := gent.DataPart(test.input.value)
			if !assert.NoError(t, err) {
				return
			}
			data := part.(gent.DataContent)
			assert.Equal(t, test.expected.value, data.Value())
			assert.Equal(t, test.expected.json, data.String())
			parts, err := gent.NewParts().Data(test.input.value).Build()
			assert.NoError(t, err)
			assert.Equal(t, []gent.ContentPart{part}, parts)
			saved, err := gent.NewCodecRegistry().Marshal(part)
			if !assert.NoError(t, err) {
				return
			}
			var restored gent.DataContent
			assert.NoError(t, gent.NewCodecRegistry().Unmarshal(saved, &restored))
			assert.Equal(t, data, restored)
		})
	}
}

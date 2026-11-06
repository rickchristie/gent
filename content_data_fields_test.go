package gent_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type dataTextKey int

func (k dataTextKey) MarshalText() ([]byte, error) { return []byte(fmt.Sprintf("key:%d", k)), nil }

type dataStringKey string

func (k dataStringKey) MarshalText() ([]byte, error) { return []byte("ignored"), nil }

func TestDataNumbersUseJSONFieldSelection(t *testing.T) {
	type Deep struct {
		Number float64 `json:"number"`
	}
	type First struct{ Deep }
	type Second struct {
		Number int64 `json:"number"`
	}
	type Domain struct {
		First
		Second
	}
	type Float struct{ Number float64 }
	type Tagged struct {
		Value int64 `json:"Number"`
	}
	type TaggedWinner struct {
		Float
		Tagged
	}
	type Direct struct {
		Tagged
		Number float64
	}
	type Ambiguous struct {
		Deep
		Second
		Value float64 `json:"value"`
	}
	type Named struct {
		Deep `json:"nested"`
	}
	type private struct{ Number float64 }
	type PrivateFields struct{ private }
	type Optional struct{ *Deep }
	type input struct{ value any }
	type expected struct{ value any }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"integer map keys", input{map[int]float64{7: 2}},
			expected{map[string]any{"7": float64(2)}}},
		{"unsigned map keys", input{map[uint64]any{7: float64(2), 8: uint64(9)}},
			expected{map[string]any{"7": float64(2), "8": uint64(9)}}},
		{"text map keys", input{map[dataTextKey]float64{7: 2}},
			expected{map[string]any{"key:7": float64(2)}}},
		{"string key takes priority", input{map[dataStringKey]float64{"key": 2}},
			expected{map[string]any{"key": float64(2)}}},
		{"nearest embedded field", input{Domain{First{Deep{1.5}}, Second{7}}},
			expected{map[string]any{"number": int64(7)}}},
		{"tagged field at same depth", input{TaggedWinner{Float{1.5}, Tagged{7}}},
			expected{map[string]any{"Number": int64(7)}}},
		{"direct field before embedded tag", input{Direct{Tagged{7}, 2}},
			expected{map[string]any{"Number": float64(2)}}},
		{"ambiguous field omitted", input{Ambiguous{Deep{1.5}, Second{7}, 2}},
			expected{map[string]any{"value": float64(2)}}},
		{"named embedded field", input{Named{Deep{2}}},
			expected{map[string]any{"nested": map[string]any{"number": float64(2)}}}},
		{"exported fields in private embedding", input{PrivateFields{private{2}}},
			expected{map[string]any{"Number": float64(2)}}},
		{"nil embedded pointer", input{Optional{}}, expected{map[string]any{}}},
		{"embedded pointer", input{Optional{&Deep{2}}},
			expected{map[string]any{"number": float64(2)}}},
		{"nested numeric keys and field selection", input{[]map[int]Domain{
			{1: {First{Deep{1.5}}, Second{7}}}, {2: {First{Deep{2.5}}, Second{9}}}}},
			expected{[]any{map[string]any{"1": map[string]any{"number": int64(7)}},
				map[string]any{"2": map[string]any{"number": int64(9)}}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			part, err := gent.DataPart(test.input.value)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.value, part.(gent.DataContent).Value())
			original, err := json.Marshal(test.input.value)
			assert.NoError(t, err)
			assert.JSONEq(t, string(original), part.(gent.DataContent).String())
			encoded, err := gent.NewCodecRegistry().Marshal(part)
			if !assert.NoError(t, err) {
				return
			}
			var restored gent.DataContent
			assert.NoError(t, gent.NewCodecRegistry().Unmarshal(encoded, &restored))
			assert.Equal(t, part, restored)
			assert.Equal(t, test.expected.value, restored.Value())
		})
	}
}

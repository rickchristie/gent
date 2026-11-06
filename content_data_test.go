package gent_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type encodedFloat float64

func (encodedFloat) MarshalJSON() ([]byte, error) { return []byte("7.5"), nil }

func TestDataPartsCaptureAndRestore(t *testing.T) {
	type order struct {
		ID       uint64  `json:"id"`
		Amount   float64 `json:"amount"`
		Optional string  `json:"optional,omitempty"`
		Hidden   string  `json:"-"`
	}
	type input struct{ value any }
	type expected struct {
		value any
		json  string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"struct and exact numbers", input{order{9007199254740993, 2, "", "private"}},
			expected{map[string]any{"id": uint64(9007199254740993), "amount": float64(2)},
				`{"amount":2,"id":9007199254740993}`}},
		{"unsigned maximum", input{uint64(math.MaxUint64)},
			expected{uint64(math.MaxUint64), `18446744073709551615`}},
		{"custom numeric encoding", input{encodedFloat(99)}, expected{json.Number("7.5"), `7.5`}},
		{"float32 JSON value", input{float32(1.1)}, expected{float64(1.1), `1.1`}},
		{"arbitrary integer", input{json.Number("123456789012345678901234567890")},
			expected{json.Number("123456789012345678901234567890"), `123456789012345678901234567890`}},
		{"nested", input{map[string]any{"items": []any{int32(-1), float64(2), nil}}},
			expected{map[string]any{"items": []any{int64(-1), float64(2), nil}},
				`{"items":[-1,2,null]}`}},
		{"nil", input{nil}, expected{nil, `null`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			part, err := gent.DataPart(test.input.value)
			if !assert.NoError(t, err) {
				return
			}
			data := part.(gent.DataContent)
			assert.Equal(t, test.expected.value, data.Value())
			assert.Equal(t, test.expected.json, data.String())
			// A fresh registry needs neither the old struct nor its custom JSON method.
			bytes, err := gent.NewCodecRegistry().Marshal(part)
			if !assert.NoError(t, err) {
				return
			}
			var restored gent.DataContent
			assert.NoError(t, gent.NewCodecRegistry().Unmarshal(bytes, &restored))
			assert.Equal(t, data, restored)
		})
	}
}

func TestPartsBuilderCapturesDataAndOwnsBytes(t *testing.T) {
	source := map[string]any{"ids": []int{7, 8}}
	pdf := []byte("%PDF-1.7\nfixture")
	builder := gent.NewParts().Text("Start").Data(source).PDF(pdf).
		ImageURL("https://example.test/image.png").Text("End")
	source["ids"].([]int)[0] = 99
	parts, err := builder.Build()
	if !assert.NoError(t, err) {
		return
	}
	data, err := gent.DataPart(map[string]any{"ids": []int{7, 8}})
	assert.NoError(t, err)
	assert.Equal(t, []gent.ContentPart{gent.TextPart("Start"), data, gent.PDFPart(pdf),
		gent.ImageURLPart("https://example.test/image.png"), gent.TextPart("End")}, parts)
	assert.Equal(t, reflect.ValueOf(pdf).Pointer(),
		reflect.ValueOf(parts[2].(gent.BinaryContent).Data).Pointer())
	encoded, err := gent.NewCodecRegistry().Marshal(parts)
	assert.NoError(t, err)
	var restored []gent.ContentPart
	assert.NoError(t, gent.NewCodecRegistry().Unmarshal(encoded, &restored))
	assert.Equal(t, parts, restored)
}

func TestPartsBuilderReturnsNoPartialContent(t *testing.T) {
	type input struct{ data any }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"channel", input{make(chan int)},
			expected{"gent: capture data: json: unsupported type: chan int"}},
		{"function", input{func() {}}, expected{"gent: capture data: json: unsupported type: func()"}},
		{"not finite", input{math.Inf(1)}, expected{"gent: capture data: json: unsupported value: +Inf"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			parts, err := gent.NewParts().Text("before").Data(test.input.data).Text("after").Build()
			assert.Nil(t, parts)
			assert.EqualError(t, err, test.expected.err)
		})
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	parts, err := gent.NewParts().Data(cyclic).Image([]byte("not an image")).Build()
	assert.Nil(t, parts)
	expectedError :=
		`gent: capture data: json: unsupported value: encountered a cycle via map[string]interface {}
gent: cannot detect a supported image format (text/plain; charset=utf-8)`
	assert.EqualError(t, err, expectedError)
}

package gent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCodecCloneMatchesPortableValue(t *testing.T) {
	type input struct {
		value any
	}
	type expected struct {
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"monotonic time", input{time.Now()}, expected{}},
		{"named time zone", input{time.Date(2026, 10, 29, 1, 2, 3, 4,
			time.FixedZone("fixture", 7*60*60))}, expected{}},
		{"empty encoded data", input{EncodedValue{Type: "fixture", Data: json.RawMessage{}}}, expected{}},
		{"encoded HTML", input{EncodedValue{Type: "fixture", Data: json.RawMessage(` {
  "html": "<div>&", "line": "\u2028", "number": 1.20
} `)}}, expected{}},
		{"raw HTML", input{json.RawMessage(` {"html":"<div>&", "number":1.20} `)}, expected{}},
		{"empty number", input{json.Number("")}, expected{}},
		{"large number", input{json.Number("12345678901234567890.123456789")}, expected{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewCodecRegistry()
			encoded, err := registry.Encode(tt.input.value)
			assert.NoError(t, err)
			portable, err := registry.Decode(encoded)
			assert.NoError(t, err)
			actual, err := registry.Clone(tt.input.value)
			if tt.expected.errorText != "" {
				assert.EqualError(t, err, tt.expected.errorText)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, portable, actual)
		})
	}
}

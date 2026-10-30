package gent

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type savedImageBytes []byte

func TestCodecBinaryRepresentation(t *testing.T) {
	type input struct {
		value any
	}
	type expected struct {
		wire  string
		value any
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"nil", input{[]byte(nil)}, expected{
			`{"type":"bytes","version":1,"data":null}`, []byte(nil)}},
		{"empty", input{[]byte{}}, expected{
			`{"type":"bytes","version":1,"data":""}`, []byte{}}},
		{"binary", input{[]byte{0, 1, 2, 255}}, expected{
			`{"type":"bytes","version":1,"data":"AAEC/w=="}`, []byte{0, 1, 2, 255}}},
		{"named", input{savedImageBytes{0, 255}}, expected{
			`{"type":"image-bytes","version":2,"data":{"type":"bytes","version":1,"data":"AP8="}}`,
			savedImageBytes{0, 255}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewCodecRegistry()
			assert.NoError(t, registry.RegisterType("image-bytes", 2, savedImageBytes{}))
			wire, err := registry.Marshal(tt.input.value)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, tt.expected.wire, string(wire))
			var actual any
			assert.NoError(t, registry.Unmarshal(wire, &actual))
			assert.Equal(t, tt.expected.value, actual)
		})
	}
}

type customBytesCodec struct{}

func (customBytesCodec) Type() string { return "fixture:custom-bytes" }
func (customBytesCodec) Version() int { return 3 }
func (customBytesCodec) Encode(value any) (json.RawMessage, error) {
	return json.Marshal(map[string][]byte{"buffer": value.([]byte)})
}
func (customBytesCodec) Decode(data json.RawMessage) (any, error) {
	var fields map[string][]byte
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields["buffer"], nil
}

func TestCodecBinaryCustomCodec(t *testing.T) {
	registry := NewCodecRegistry()
	assert.NoError(t, registry.Register([]byte(nil), customBytesCodec{}))
	value := &BinaryContent{MIMEType: "image/png", Data: []byte{0, 255, 7}}
	wire, err := registry.Encode(value.Data)
	assert.NoError(t, err)
	assert.Equal(t, EncodedValue{Type: "fixture:custom-bytes", Version: 3,
		Data: json.RawMessage(`{"buffer":"AP8H"}`)}, wire)
	cloned, err := registry.Clone(value)
	assert.NoError(t, err)
	assert.Equal(t, value, cloned)
}

func TestCodecBinaryOwnership(t *testing.T) {
	registry := NewCodecRegistry()
	assert.NoError(t, registry.RegisterValueType(reflect.TypeFor[savedImageBytes]()))
	bytesValue := savedImageBytes{0, 255, 7}
	input := []any{
		&BinaryContent{MIMEType: "image/png", Data: []byte{0, 1, 255}},
		BinaryContent{MIMEType: "application/pdf", Data: []byte{0, 2, 255}},
		&bytesValue,
	}
	wire, err := registry.Marshal(input)
	if !assert.NoError(t, err) {
		return
	}
	input[0].(*BinaryContent).Data[0] = 99
	input[1].(BinaryContent).Data[0] = 88
	bytesValue[0] = 77
	wantBytes := savedImageBytes{0, 255, 7}
	expected := []any{
		&BinaryContent{MIMEType: "image/png", Data: []byte{0, 1, 255}},
		BinaryContent{MIMEType: "application/pdf", Data: []byte{0, 2, 255}},
		&wantBytes,
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			var restored []any
			if !assert.NoError(t, registry.Unmarshal(wire, &restored)) {
				return
			}
			assert.Equal(t, expected, restored)
			restored[0].(*BinaryContent).Data[0] = 66
			restored[1].(BinaryContent).Data[0] = 55
			(*restored[2].(*savedImageBytes))[0] = 44
		}()
	}
	workers.Wait()
	var restored []any
	assert.NoError(t, registry.Unmarshal(wire, &restored))
	assert.Equal(t, expected, restored)
}

func TestCodecBinaryDecodeValidation(t *testing.T) {
	type input struct {
		value EncodedValue
		limit int
	}
	type expected struct {
		value     any
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"boundary", input{EncodedValue{"bytes", 1, json.RawMessage(`"AAH/"`)}, 3},
			expected{value: []byte{0, 1, 255}}},
		{"padding boundary", input{EncodedValue{"bytes", 1, json.RawMessage(`"AP8="`)}, 2},
			expected{value: []byte{0, 255}}},
		{"two padding boundary", input{EncodedValue{"bytes", 1, json.RawMessage(`"/w=="`)}, 1},
			expected{value: []byte{255}}},
		{"too large", input{EncodedValue{"bytes", 1, json.RawMessage(`"AAH/"`)}, 2},
			expected{errorText: "gent: binary value exceeds 2 bytes"}},
		{"version", input{EncodedValue{"bytes", 2, json.RawMessage(`"AAH/"`)}, 0},
			expected{errorText: "gent: unsupported bytes version 2"}},
		{"array", input{EncodedValue{"bytes", 1, json.RawMessage(`[0,1,255]`)}, 0},
			expected{errorText: "gent: byte payload must be a base64 string or null"}},
		{"invalid alphabet", input{EncodedValue{"bytes", 1, json.RawMessage(`"!!!!"`)}, 0},
			expected{errorText: "illegal base64 data at input byte 0"}},
		{"missing padding", input{EncodedValue{"bytes", 1, json.RawMessage(`"/w"`)}, 0},
			expected{errorText: "illegal base64 data at input byte 0"}},
		{"trailing bits", input{EncodedValue{"bytes", 1, json.RawMessage(`"/x=="`)}, 0},
			expected{errorText: "illegal base64 data at input byte 2"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewCodecRegistry(WithCodecLimits(CodecLimits{MaxBinaryBytes: tt.input.limit}))
			value, err := registry.Decode(tt.input.value)
			assert.Equal(t, tt.expected.value, value)
			if tt.expected.errorText != "" {
				assert.EqualError(t, err, tt.expected.errorText)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestCodecBinaryLimits(t *testing.T) {
	registry := NewCodecRegistry(WithCodecLimits(CodecLimits{
		MaxBinaryBytes: 3, MaxEncodedBytes: 64,
	}))
	wire, err := registry.Marshal([]byte{0, 1, 255})
	assert.NoError(t, err)
	assert.Equal(t, `{"type":"bytes","version":1,"data":"AAH/"}`, string(wire))
	_, err = registry.Encode([]byte{0, 1, 2, 255})
	assert.EqualError(t, err, "gent: binary value exceeds 3 bytes")
	_, err = registry.Marshal(string(bytes.Repeat([]byte{'a'}, 65)))
	assert.EqualError(t, err, "gent: encoded value exceeds 64 bytes")
	var value []byte
	err = registry.Unmarshal(bytes.Repeat([]byte{' '}, 65), &value)
	assert.EqualError(t, err, "gent: encoded value exceeds 64 bytes")
	assert.Nil(t, value)
}

func BenchmarkCodecBinaryRoundTrip(b *testing.B) {
	registry := NewCodecRegistry()
	value := &BinaryContent{MIMEType: "image/png", Data: bytes.Repeat([]byte{7}, 64*1024)}
	b.ReportAllocs()
	b.SetBytes(int64(len(value.Data)))
	for b.Loop() {
		wire, err := registry.Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
		var restored *BinaryContent
		if err := registry.Unmarshal(wire, &restored); err != nil {
			b.Fatal(err)
		}
	}
}

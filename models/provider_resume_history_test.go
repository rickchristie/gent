package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Check the saved native group against protocol values, in addition to comparing the whole
// resumed request to an uninterrupted run. This catches lost opaque items in either path.
func assertProviderResumeNativeHistory(t *testing.T, provider string, body map[string]any,
	compact bool,
) {
	t.Helper()
	field, start, size := "messages", 2, 2
	want := `[{"role":"assistant","reasoning_content":"Read the record.","tool_calls":[
{"id":"call-7","type":"function","function":{"name":"lookup","arguments":"{\"id\":7}"}}]},
{"role":"tool","tool_call_id":"call-7","content":"record-seven"}]`
	switch provider {
	case "openai", "xai":
		field, size = "input", 3
		want = `[{"type":"reasoning","encrypted_content":"sealed"},
{"type":"function_call","call_id":"call-7","name":"lookup","arguments":"{\"id\":7}"},
{"type":"function_call_output","call_id":"call-7","output":"record-seven"}]`
	case "anthropic":
		start = 1
		want = `[{"role":"assistant","content":[
{"type":"thinking","thinking":"Read the record.","signature":"sealed"},
{"type":"tool_use","id":"call-7","name":"lookup","input":{"id":7}}]},
{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-7",
"content":[{"type":"text","text":"record-seven"}]}]}]`
	case "gemini":
		field, start = "contents", 1
		want = `[{"role":"model","parts":[{"text":"Read the record.","thought":true},
{"functionCall":{"id":"call-7","name":"lookup","args":{"id":7}},"thoughtSignature":"sealed"}]},
{"role":"user","parts":[{"functionResponse":{"id":"call-7","name":"lookup",
"response":{"result":["record-seven"]}}}]}]`
	case "ollama":
		want = `[{"role":"assistant","thinking":"Read the record.",
"tool_calls":[{"function":{"name":"lookup","arguments":{"id":7}}}]},
{"role":"tool","tool_name":"lookup","content":"record-seven"}]`
	}
	if compact {
		start++
	}
	items, ok := body[field].([]any)
	if !assert.True(t, ok) || !assert.GreaterOrEqual(t, len(items), start+size) {
		return
	}
	data, err := json.Marshal(items[start : start+size])
	if assert.NoError(t, err) {
		assert.JSONEq(t, want, string(data))
	}
}

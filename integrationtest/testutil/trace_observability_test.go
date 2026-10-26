package testutil

import (
	"bytes"
	"testing"
	"time"

	genttrace "github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceStreamRenderer_Consume(t *testing.T) {
	type input struct {
		events []*genttrace.Event
	}
	type expected struct {
		output string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	testCases := []testCase{
		{
			name: "iteration starts without model text",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
			}},
			expected: expected{output: `
--- Iteration 1 ---
`},
		},
		{
			name: "empty model chunk",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
`},
		},
		{
			name: "commentary stream chunks",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{Type: genttrace.EventTypeModelStreamChunk, Iteration: 1,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"metaContent": []any{map[string]any{"type": "commentary", "text": "Checking."}}}},
			}},
			expected: expected{output: `
--- Iteration 1 ---
  LLM: Checking.
`},
		},
		{
			name: "model stream chunks",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"content": "hello "},
				},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"content": "world"},
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
  LLM: hello world
`},
		},
		{
			name: "ignore other stream topic",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: "other-topic",
					Payload:       map[string]any{"content": "hidden"},
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
`},
		},
		{
			name: "iteration with only tool calls between model text iterations",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"content": "Checking the booking."},
				},
				{Type: genttrace.EventTypeIterationFinished, Iteration: 1},
				{Type: genttrace.EventTypeIterationStarted, Iteration: 2},
				{
					Type:       genttrace.EventTypeToolCallStarted,
					Iteration:  2,
					ToolCallId: "tool-1",
					Payload: map[string]any{
						"name": "get_booking_info",
						"args": map[string]any{"booking_id": "BK001"},
					},
				},
				{
					Type:       genttrace.EventTypeToolCallFinished,
					Iteration:  2,
					ToolCallId: "tool-1",
					Payload: map[string]any{
						"name":       "get_booking_info",
						"output":     map[string]any{"status": "confirmed"},
						"durationMs": float64(12),
					},
				},
				{Type: genttrace.EventTypeIterationFinished, Iteration: 2},
				{Type: genttrace.EventTypeIterationStarted, Iteration: 3},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     3,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"metaContent": []any{map[string]any{"type": "reasoning", "text": "The booking is confirmed."}}},
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
  LLM: Checking the booking.

--- Iteration 2 ---


  [Tool: get_booking_info]
    Args: {"booking_id":"BK001"}
    Output: {"status":"confirmed"}
    Duration: 12ms

--- Iteration 3 ---
  LLM: The booking is confirmed.
`},
		},
		{
			name: "stream error without model text",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Error:         &genttrace.Error{Message: "connection closed"},
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
  [Stream Error: connection closed]
`},
		},
		{
			name: "stream error after model text",
			input: input{events: []*genttrace.Event{
				{Type: genttrace.EventTypeIterationStarted, Iteration: 1},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Payload:       map[string]any{"content": "Checking the booking."},
				},
				{
					Type:          genttrace.EventTypeModelStreamChunk,
					Iteration:     1,
					StreamTopicId: llmResponseTopic,
					Error:         &genttrace.Error{Message: "connection closed"},
				},
			}},
			expected: expected{output: `
--- Iteration 1 ---
  LLM: Checking the booking.
  [Stream Error: connection closed]
`},
		},
		{
			name: "tool call result",
			input: input{events: []*genttrace.Event{
				{
					Type:       genttrace.EventTypeToolCallStarted,
					ToolCallId: "tool-1",
					Payload: map[string]any{
						"name": "lookup",
						"args": map[string]any{"id": "A1"},
					},
				},
				{
					Type:       genttrace.EventTypeToolCallFinished,
					ToolCallId: "tool-1",
					Payload: map[string]any{
						"name":       "lookup",
						"durationMs": float64(12),
						"output":     map[string]any{"status": "ok"},
					},
				},
			}},
			expected: expected{output: `

  [Tool: lookup]
    Args: {"id":"A1"}
    Output: {"status":"ok"}
    Duration: 12ms
`},
		},
		{
			name: "compaction and limit",
			input: input{events: []*genttrace.Event{
				{
					Type: genttrace.EventTypeCompaction,
					Payload: map[string]any{
						"scratchpadLengthBefore": float64(5),
						"scratchpadLengthAfter":  float64(2),
						"durationMs":             float64(7),
					},
				},
				{
					Type: genttrace.EventTypeLimitExceeded,
					Payload: map[string]any{
						"matchedKey":   "gent:iterations",
						"currentValue": float64(10),
						"limit":        map[string]any{"MaxValue": float64(5)},
					},
				},
			}},
			expected: expected{output: `

  [Compaction: 5 -> 2 entries (removed 3, took 7ms)]


  [Limit Exceeded: gent:iterations = 10 (max: 5)]
`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan *genttrace.Event, len(tc.input.events))
			for _, event := range tc.input.events {
				events <- event
			}
			close(events)

			var output bytes.Buffer
			renderer := newTraceStreamRenderer(&output, llmResponseTopic)
			renderer.Consume(events)

			assert.Equal(t, tc.expected.output, output.String())
		})
	}
}

func TestTraceEventLogger_WritesEventsAndSnapshot(t *testing.T) {
	type input struct {
		event    *genttrace.Event
		snapshot *genttrace.Snapshot
	}
	type expected struct {
		eventsOutput   string
		snapshotOutput string
	}

	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	tc := struct {
		input    input
		expected expected
	}{
		input: input{
			event: &genttrace.Event{
				EventNumber: 1,
				RunId:       "run",
				Ts:          ts,
				Type:        genttrace.EventTypeCommon,
				Payload:     map[string]any{"x": "y"},
			},
			snapshot: &genttrace.Snapshot{
				SchemaVersion:   genttrace.SchemaVersion,
				RunId:           "run",
				Status:          genttrace.RunStatusSucceeded,
				StartedTs:       ts,
				LastUpdatedTs:   ts,
				LastEventNumber: 1,
			},
		},
		expected: expected{
			eventsOutput: `
>>> TraceEvent 1 common
{
  "eventNumber": 1,
  "runId": "run",
  "ts": "2026-05-10T12:00:00Z",
  "type": "common",
  "payload": {
    "x": "y"
  }
}
`,
			snapshotOutput: `
>>> FinalTraceSnapshot
{
  "schemaVersion": 5,
  "runId": "run",
  "status": "succeeded",
  "startedTs": "2026-05-10T12:00:00Z",
  "lastUpdatedTs": "2026-05-10T12:00:00Z",
  "lastEventNumber": 1,
  "stats": {
    "contextCount": 0,
    "iterationCount": 0,
    "modelCallCount": 0,
    "toolCallCount": 0,
    "inputTokens": 0,
    "outputTokens": 0,
    "reasoningTokens": 0,
    "cachedInputTokens": 0,
    "totalTokens": 0,
    "durationMs": 0,
    "modelDurationMs": 0,
    "toolDurationMs": 0,
    "parseErrorCount": 0,
    "validatorRejectionCount": 0,
    "errorCount": 0,
    "limitExceededCount": 0,
    "compactionCount": 0
  }
}
`,
		},
	}

	events := make(chan *genttrace.Event, 1)
	events <- tc.input.event
	close(events)

	var eventsOutput bytes.Buffer
	writeTraceEvents(&eventsOutput, events)
	assert.Equal(t, tc.expected.eventsOutput, eventsOutput.String())

	var snapshotOutput bytes.Buffer
	WriteTraceSnapshot(&snapshotOutput, tc.input.snapshot)
	assert.Equal(t, tc.expected.snapshotOutput, snapshotOutput.String())
	require.JSONEq(
		t,
		jsonFromBlock(t, tc.expected.snapshotOutput),
		jsonFromBlock(t, snapshotOutput.String()),
	)
}

func jsonFromBlock(t *testing.T, output string) string {
	t.Helper()
	start := bytes.IndexByte([]byte(output), '{')
	require.GreaterOrEqual(t, start, 0)
	return output[start:]
}

package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	genttrace "github.com/rickchristie/gent/trace"
)

const llmResponseTopic = "llm-response"

// NewIntegrationTrace creates the trace configuration used by integration tests and the CLI.
// Integration observability intentionally uses trace as the primary source so real scenarios
// exercise the same event stream and snapshots that applications should consume.
func NewIntegrationTrace(runID string) *genttrace.Sequencer {
	return genttrace.NewSequencer(runID, genttrace.Config{
		IncludeChunkText:      true,
		IncludeModelRequests:  true,
		IncludeModelResponses: true,
		IncludeToolArgs:       true,
		IncludeToolOutput:     true,
		IncludeCommonPayload:  true,
		IncludeRunOutput:      true,
	})
}

// StartTraceStreamOutput renders live trace events to the interactive integration output.
func StartTraceStreamOutput(seq *genttrace.Sequencer, w io.Writer) func() {
	events, _ := seq.Subscribe()
	done := make(chan struct{})
	renderer := newTraceStreamRenderer(w, llmResponseTopic)
	go func() {
		defer close(done)
		renderer.Consume(events)
	}()
	return func() { <-done }
}

// StartTraceEventLogger writes live trace events to the debug log writer.
func StartTraceEventLogger(seq *genttrace.Sequencer, w io.Writer) func() {
	events, _ := seq.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeTraceEvents(w, events)
	}()
	return func() { <-done }
}

func writeTraceEvents(w io.Writer, events <-chan *genttrace.Event) {
	for event := range events {
		writeTraceEvent(w, event)
	}
}

func writeTraceEvent(w io.Writer, event *genttrace.Event) {
	fmt.Fprintf(w, "\n>>> TraceEvent %d %s\n", event.EventNumber, event.Type)
	writeJSONBlock(w, event)
}

// WriteTraceSnapshot writes the final materialized trace snapshot to the debug log writer.
func WriteTraceSnapshot(w io.Writer, snapshot *genttrace.Snapshot) {
	fmt.Fprintln(w, "\n>>> FinalTraceSnapshot")
	writeJSONBlock(w, snapshot)
}

func writeJSONBlock(w io.Writer, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "{\"error\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(w, string(data))
}

func printTraceEvents(w io.Writer, events []*genttrace.Event) {
	for i, event := range events {
		fmt.Fprintf(w, "\n[%d] #%d %s", i+1, event.EventNumber, event.Type)
		if event.Iteration > 0 {
			fmt.Fprintf(w, " iteration=%d", event.Iteration)
		}
		if event.Source != "" {
			fmt.Fprintf(w, " source=%s", event.Source)
		}
		if event.ContextId != "" {
			fmt.Fprintf(w, " context=%s", event.ContextId)
		}
		if event.ModelCallId != "" {
			fmt.Fprintf(w, " model_call=%s", event.ModelCallId)
		}
		if event.ToolCallId != "" {
			fmt.Fprintf(w, " tool_call=%s", event.ToolCallId)
		}
		if event.Error != nil {
			fmt.Fprintf(w, " error=%s", event.Error.Message)
		}
		fmt.Fprintln(w)
	}
}

type traceStreamRenderer struct {
	w     io.Writer
	topic string

	currentIter     int
	lastChunkIter   int
	iterHeaderShown bool
	hasContent      bool
	toolArgs        map[string]any
}

func newTraceStreamRenderer(w io.Writer, topic string) *traceStreamRenderer {
	return &traceStreamRenderer{w: w, topic: topic, toolArgs: make(map[string]any)}
}

func (r *traceStreamRenderer) Consume(events <-chan *genttrace.Event) {
	for event := range events {
		r.ConsumeEvent(event)
	}
	if r.hasContent {
		fmt.Fprintln(r.w)
	}
}

func (r *traceStreamRenderer) ConsumeEvent(event *genttrace.Event) {
	switch event.Type {
	case genttrace.EventTypeIterationStarted:
		r.currentIter = event.Iteration
		r.iterHeaderShown = false
	case genttrace.EventTypeModelStreamChunk:
		r.writeModelChunk(event)
	case genttrace.EventTypeToolCallStarted:
		r.captureToolArgs(event)
	case genttrace.EventTypeToolCallFinished:
		r.writeToolResult(event)
	case genttrace.EventTypeCompaction:
		r.writeCompaction(event)
	case genttrace.EventTypeLimitExceeded:
		r.writeLimitExceeded(event)
	case genttrace.EventTypeError:
		r.writeError(event)
	}
}

func (r *traceStreamRenderer) writeModelChunk(event *genttrace.Event) {
	if event.StreamTopicId != r.topic {
		return
	}
	payload := tracePayload(event)
	content := traceString(payload, "content")
	reasoning := traceString(payload, "reasoningContent")
	if content == "" && reasoning == "" && event.Error == nil {
		return
	}
	r.ensureChunkHeader(event)
	if content != "" {
		fmt.Fprint(r.w, content)
		r.hasContent = true
	}
	if reasoning != "" {
		fmt.Fprint(r.w, reasoning)
		r.hasContent = true
	}
	if event.Error != nil {
		r.finishContentLine()
		fmt.Fprintf(r.w, "  [Stream Error: %s]\n", event.Error.Message)
	}
}

func (r *traceStreamRenderer) ensureChunkHeader(event *genttrace.Event) {
	iteration := event.Iteration
	if iteration == 0 {
		iteration = r.currentIter
	}
	if iteration == 0 {
		return
	}
	if iteration == r.lastChunkIter && r.iterHeaderShown {
		return
	}
	if r.hasContent {
		fmt.Fprintln(r.w)
		r.hasContent = false
	}
	fmt.Fprintf(r.w, "\n--- Iteration %d ---\n", iteration)
	fmt.Fprint(r.w, "  LLM: ")
	r.lastChunkIter = iteration
	r.iterHeaderShown = true
}

func (r *traceStreamRenderer) captureToolArgs(event *genttrace.Event) {
	if event.ToolCallId == "" {
		return
	}
	payload := tracePayload(event)
	if args, ok := payload["args"]; ok {
		r.toolArgs[event.ToolCallId] = args
	}
}

func (r *traceStreamRenderer) writeToolResult(event *genttrace.Event) {
	r.finishContentLine()
	payload := tracePayload(event)
	name := traceString(payload, "name")
	if name == "" {
		name = "unknown"
	}
	fmt.Fprintf(r.w, "\n\n  [Tool: %s]\n", name)
	if args, ok := r.toolArgs[event.ToolCallId]; ok {
		fmt.Fprintf(r.w, "    Args: %s\n", inlineJSON(args))
	}
	if event.Error != nil {
		fmt.Fprintf(r.w, "    Error: %s\n", event.Error.Message)
	} else if output, ok := payload["output"]; ok {
		fmt.Fprintf(r.w, "    Output: %s\n", inlineJSON(output))
	}
	fmt.Fprintf(r.w, "    Duration: %s\n", durationFromPayload(payload))
	delete(r.toolArgs, event.ToolCallId)
}

func (r *traceStreamRenderer) writeCompaction(event *genttrace.Event) {
	r.finishContentLine()
	payload := tracePayload(event)
	before := traceInt(payload, "scratchpadLengthBefore")
	after := traceInt(payload, "scratchpadLengthAfter")
	fmt.Fprintf(r.w,
		"\n\n  [Compaction: %d -> %d iterations (removed %d, took %s)]\n",
		before, after, before-after, durationFromPayload(payload),
	)
}

func (r *traceStreamRenderer) writeLimitExceeded(event *genttrace.Event) {
	r.finishContentLine()
	payload := tracePayload(event)
	limit := tracePayloadFromAny(payload["limit"])
	fmt.Fprintf(r.w,
		"\n\n  [Limit Exceeded: %s = %.0f (max: %.0f)]\n",
		traceString(payload, "matchedKey"), traceFloat(payload, "currentValue"),
		traceFloat(limit, "MaxValue"),
	)
}

func (r *traceStreamRenderer) writeError(event *genttrace.Event) {
	if event.Error == nil {
		return
	}
	r.finishContentLine()
	fmt.Fprintf(r.w, "\n\n  [Error: %s]\n", event.Error.Message)
}

func (r *traceStreamRenderer) finishContentLine() {
	if !r.hasContent {
		return
	}
	fmt.Fprintln(r.w)
	r.hasContent = false
}

func tracePayload(event *genttrace.Event) map[string]any {
	return tracePayloadFromAny(event.Payload)
}

func tracePayloadFromAny(value any) map[string]any {
	payload, _ := value.(map[string]any)
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func traceString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func traceInt(payload map[string]any, key string) int {
	return int(traceFloat(payload, key))
}

func traceFloat(payload map[string]any, key string) float64 {
	switch value := payload[key].(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	case float32:
		return float64(value)
	default:
		return 0
	}
}

func durationFromPayload(payload map[string]any) time.Duration {
	return time.Duration(traceFloat(payload, "durationMs")) * time.Millisecond
}

func inlineJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return string(data)
}

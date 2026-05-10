# Trace-Primary Integration Refactor Plan

This document is the source of truth for making `trace.Sequencer` the primary observability path
for integration tests and the integration CLI. Refer to it before changing integration logging,
streaming output, trace event rendering, or integration test helpers.

## Context

Gent now has a generic `trace` package that observes Gent lifecycle events, stream chunks, model
calls, tool calls, compaction, limits, child contexts, and errors. It assigns monotonic trace event
numbers, maintains a materialized `trace.Snapshot`, supports live subscriptions, and captures
JSON-safe payloads for UI/replay use.

The integration layer still has two older observability paths:

- `integrationtest/loggers.LoggerSubscriber` implements many Gent event subscriber interfaces and
  writes ad-hoc logs directly from raw events.
- `integrationtest/testutil.StreamConsumer` reads raw `ExecutionContext.SubscribeToTopic` chunks
  while `StreamingOutputHook` listens to separate raw events for iteration, tool, compaction, and
  limit output.

Those old paths duplicate trace behavior and let integration tests bypass the API that applications
should use. They also create maintenance overhead: every new event, payload shape, correlation field,
or stream behavior needs to be represented twice.

## Decision

Integration tests and the integration CLI should use `trace.Sequencer` as their primary
observability source.

This means:

- Each integration run creates exactly one `trace.Sequencer`.
- The sequencer is subscribed to the executor event registry.
- CLI streaming consumes `trace.Event` subscriptions, not raw `StreamChunk` channels.
- File/debug logging consumes `trace.Event` subscriptions and final `trace.Snapshot`, not raw Gent
  event subscriber callbacks.
- `ShowEvents` prints trace events, not `execCtx.Events()`.
- The old `integrationtest/loggers` package is removed.
- Unit tests for raw event publishing and raw stream subscriptions remain elsewhere because trace
  depends on those primitives.

## Goals

- Dogfood the production trace API in real integration scenarios.
- Keep CLI output readable and close to current output while changing the data source underneath.
- Reduce duplicate observability code.
- Make integration logs replay-friendly and JSON-safe.
- Increase confidence that trace correlation, stream capture, tool capture, compaction, limits, and
  final snapshots work together under real agent runs.

## Non-Goals

- Do not remove core event subscriber interfaces.
- Do not remove raw `ExecutionContext` stream subscription APIs.
- Do not rewrite the domain scenarios or tools.
- Do not make trace output chat-specific or ReAct-specific.
- Do not add persistent trace storage in this refactor.

## Current Code References

Integration entry points:

- `integrationtest/testutil/testutil.go:631`: `RunScenario` creates executor, registry, streaming,
  and file logging for one-shot scenarios.
- `integrationtest/testutil/testutil.go:1122`: `InteractiveChat.SendMessage` does the same for each
  chat message.
- `integrationtest/cli/main.go`: menu-driven CLI calls one-shot and chat helpers.

Old observability to remove or replace:

- `integrationtest/loggers/logger.go`: raw-event debug logger.
- `StreamingOutputHook` in `integrationtest/testutil/testutil.go`: raw-event side channel for
  streaming display.
- `StreamConsumer` in `integrationtest/testutil/testutil.go`: raw `StreamChunk` consumer.
- `printEvents` in `integrationtest/testutil/testutil.go`: raw `execCtx.Events()` printer.

Trace APIs to use:

- `trace.NewSequencer(runID, trace.Config)` creates a run trace.
- `events.NewRegistry().Subscribe(seq)` routes Gent lifecycle events into the sequencer.
- `seq.Subscribe()` streams future `*trace.Event` values.
- `seq.Close()` closes live subscriptions after execution finishes.
- `seq.Snapshot()` returns the final materialized trace state.

## New Integration Helpers

Create `integrationtest/testutil/trace_observability.go` in package `testutil`.

### Trace Creation

Use one helper so integration runs use the same trace capture policy:

```go
const llmResponseTopic = "llm-response"

func NewIntegrationTrace(runID string) *trace.Sequencer {
	return trace.NewSequencer(runID, trace.Config{
		IncludeChunkText:      true,
		IncludeModelRequests:  true,
		IncludeModelResponses: true,
		IncludeToolArgs:       true,
		IncludeToolOutput:     true,
		IncludeCommonPayload:  true,
		IncludeRunOutput:      true,
	})
}
```

Rationale:

- `IncludeChunkText` is required because the CLI stream renderer reads chunk text from trace events.
- Tool args/output are needed to preserve current CLI tool display.
- Model request/response and run output make integration logs useful for diagnosis.
- Common payloads preserve child context and state-change breadcrumbs.

### Trace Consumers

Use live subscriptions for both interactive output and file logging:

```go
func StartTraceStreamOutput(seq *trace.Sequencer, w io.Writer) func() {
	events, _ := seq.Subscribe()
	done := make(chan struct{})
	renderer := newTraceStreamRenderer(w, llmResponseTopic)
	go func() {
		defer close(done)
		renderer.Consume(events)
	}()
	return func() { <-done }
}

func StartTraceEventLogger(seq *trace.Sequencer, w io.Writer) func() {
	events, _ := seq.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeTraceEvents(w, events)
	}()
	return func() { <-done }
}
```

Important ordering:

1. Create sequencer.
2. Subscribe live consumers.
3. Register sequencer with the event registry.
4. Execute the agent.
5. Call `seq.Close()`.
6. Wait for live consumers to drain.
7. Write final snapshot/log summaries.

This preserves all events because `trace.Sequencer` uses unbounded live subscriptions by default and
drains buffered events after close.

### Stream Rendering

The renderer should consume trace events and preserve current CLI readability:

```go
switch event.Type {
case trace.EventTypeIterationStarted:
	r.currentIter = event.Iteration
	r.iterHeaderShown = false
case trace.EventTypeModelStreamChunk:
	if event.StreamTopicId == llmResponseTopic {
		r.writeModelChunk(event)
	}
case trace.EventTypeToolCallStarted:
	r.toolArgs[event.ToolCallId] = payload["args"]
case trace.EventTypeToolCallFinished:
	r.writeToolResult(event)
case trace.EventTypeCompaction:
	r.writeCompaction(event)
case trace.EventTypeLimitExceeded:
	r.writeLimit(event)
case trace.EventTypeError:
	r.writeError(event)
}
```

Tool args are captured on `tool_call_started`; tool output and duration are captured on
`tool_call_finished`. The renderer should store args by `ToolCallId` so the visible CLI output can
show both args and output when the tool completes.

The chunk payload has JSON-safe values. Read string fields from `payload["content"]` and
`payload["reasoningContent"]`. Event payload numbers may be `float64` because trace stores
JSON-safe copies; helper functions should accept `int`, `int64`, and `float64`.

### File Logging

Trace file logging should be simple, complete, and JSON-safe:

```text
>>> TraceEvent 12 model_call_finished
{
  "eventNumber": 12,
  "runId": "airline-reschedule",
  "type": "model_call_finished",
  ...
}

>>> FinalTraceSnapshot
{
  "schemaVersion": 1,
  "status": "succeeded",
  ...
}
```

Do not reconstruct another bespoke event model for file logging. The whole point is to log the
same trace event/snapshot shape that UI/replay users consume.

## Integration Wiring

In `RunScenario`:

```go
seq := NewIntegrationTrace(scenario.Name)
streamWait := StartTraceStreamOutput(seq, w)
var logWait func()
if testCfg.LogWriter != nil {
	logWait = StartTraceEventLogger(seq, testCfg.LogWriter)
}
registry := events.NewRegistry().Subscribe(seq)
// subscribe other non-observability hooks, like PolicySuggestionHook

exec.Execute(execCtx)
seq.Close()
streamWait()
if logWait != nil {
	logWait()
	WriteTraceSnapshot(testCfg.LogWriter, seq.Snapshot())
}
```

Use the same pattern in `InteractiveChat.SendMessage`, with a run ID like
`s.ChatCfg.Name + "-chat"`.

Do not call `execCtx.SubscribeToTopic` from integration code after this refactor.

## ShowEvents Behavior

When `TestConfig.ShowEvents` is true, print recent trace events:

```go
if testCfg.ShowEvents {
	PrintHeader(w, "TRACE EVENTS")
	printTraceEvents(w, seq.Snapshot().RecentEvents)
}
```

The output can be concise. It should include event number, type, iteration, source/context, model or
tool IDs when available, and a short payload summary. Full payloads belong in `LogWriter` trace logs.

## Removal Plan

Remove:

- `integrationtest/loggers/logger.go`
- `LoggerSubscriber` imports and subscriptions
- `StreamingOutputHook`
- `StreamConsumer`
- raw `execCtx.SubscribeToTopic("llm-response")` integration usage
- raw `printEvents(execCtx)` integration usage

Keep:

- `PolicySuggestionHook`, because it mutates agent state and is not observability-only.
- `TestConfig.LogWriter`, but change it to receive trace logs.
- `TestConfig.ShowEvents`, but change it to print trace events.

## Testing Plan

Add `integrationtest/testutil/trace_observability_test.go`.

Use table-driven tests with explicit `input` and `expected` structs.

### Stream Renderer Tests

Feed synthetic `trace.Event` values to the renderer and assert exact output.

Scenarios:

1. Iteration start plus two model stream chunks for `llm-response` prints exactly:

```text
--- Iteration 1 ---
  LLM: hello world
```

2. Chunks from another topic are ignored.
3. Tool start plus tool finish prints tool name, args, output, and duration.
4. Compaction and limit events print expected one-line status messages.
5. Stream error prints `[Stream Error: ...]`.

### Trace Logger Tests

Feed synthetic trace events into `writeTraceEvents` and assert exact JSON/log framing for known
events. This protects the log file format without needing real LLM calls.

### Integration Wiring Tests

Add small tests around helper composition when practical:

- `StartTraceEventLogger` drains events after `seq.Close()`.
- `WriteTraceSnapshot` writes JSON-safe final snapshot.

Do not add external-model integration tests for this refactor. Existing airline/e-commerce tests
remain real end-to-end coverage when credentials are present.

## Verification Commands

Run all commands with output redirected to `/tmp`:

```sh
go test ./integrationtest/testutil -count=1 -timeout 300s \
  > /tmp/gent-trace-primary-testutil-test.txt 2>&1
go test ./integrationtest/... -count=1 -timeout 300s \
  > /tmp/gent-trace-primary-integration-test.txt 2>&1
go test ./... -count=1 -timeout 300s \
  > /tmp/gent-trace-primary-full-test.txt 2>&1
go test ./... -race -count=1 -timeout 300s \
  > /tmp/gent-trace-primary-race-test.txt 2>&1
git diff --check > /tmp/gent-trace-primary-diff-check.txt 2>&1
```

## Success Criteria

- Integration one-shot and chat paths create and register one trace sequencer per run/message.
- CLI streaming reads trace events, not raw `StreamChunk` channels.
- File logging writes trace events and final trace snapshots.
- `ShowEvents` prints trace events.
- `integrationtest/loggers` is removed.
- No integration code calls `execCtx.SubscribeToTopic` for LLM output.
- Targeted tests, full tests, race tests, and whitespace checks pass.

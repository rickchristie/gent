# Programmatic Tool Calling (PTC) via JsToolChainWrapper

## Context

LLM agents currently call tools one at a time across iterations (ReAct loop). For multi-step workflows this burns tokens and latency. PTC lets the LLM write JavaScript that orchestrates multiple tool calls in a single iteration — loops, conditionals, chaining results. Research shows 37-85% token reduction and up to 20% accuracy improvement on complex tasks.

We use Sobek (grafana/sobek), a pure-Go ES5.1+ engine. Each PTC execution is just a goroutine — no containers, no IPC.

## Architecture Overview

`JsToolChainWrapper` wraps any existing ToolChain. The LLM outputs either:
- `<direct_call>` — passes through to the wrapped ToolChain unchanged
- `<code>` — executes JS via Sobek, where `tool.call()` routes back through the wrapped ToolChain

This means all existing stats, events, schema validation, and limits work unchanged for tool calls made from code.

## File Structure

```
toolchain/
  jsruntime/
    runtime.go          - Sobek lifecycle (create, register globals, execute, timeout)
    runtime_test.go
    error.go            - LLM-friendly error formatting with source context
    error_test.go
    bridge.go           - tool.call() / tool.parallelCall() JS→Go bridge
    bridge_test.go
  js_wrapper.go         - JsToolChainWrapper (the ToolChain implementation)
  js_wrapper_test.go    - All tests wrap SearchJSON as inner ToolChain

stats_keys.go           - Add SCCodeExecutions, SGCodeExecutionsErrorConsecutive
limit.go                - Add code execution error to DefaultLimits()
```

## Phase 0: Verify Assumptions

Before writing code, verify each assumption with a small spike:

1. **Sobek can execute synchronous Go-backed functions from JS**: Write a minimal test that registers a Go function as a JS global, calls it from `RunString()`, and verifies the return value.
2. **Sobek's `Interrupt()` stops infinite loops**: Write a test with `while(true){}`, call `Interrupt()` from a goroutine, verify `*InterruptedError` is returned.
3. **Sobek error types are as documented**: Trigger `*sobek.Exception`, `*CompilerSyntaxError`, verify we can extract line/column/stack via the public API.
4. **Inner TextFormat can parse sub-sections from raw text**: Create a fresh `format.NewXML()`, register "direct_call" and "code" sections, parse a string like `<direct_call>...</direct_call>`, verify extraction works.
5. **Wrapped ToolChain.Execute() can be called multiple times**: Call `SearchJSON.Execute()` twice in the same iteration with different tool calls. Verify stats accumulate correctly and no state leaks.
6. **`context.Context` cancellation can trigger `Interrupt()`**: Verify that cancelling the Go context while Sobek is running triggers the interrupt path.

If any assumption fails, adjust the plan before proceeding.

## Phase 1: Sobek Error Formatter — `toolchain/jsruntime/error.go`

Pure utility, zero dependencies on ToolChain or gent. Converts Sobek errors into LLM-friendly text with source code snippets.

### API

```go
// FormatError converts a Sobek error + original source
// into an LLM-friendly error message with code context.
func FormatError(source string, err error) string

// extractSourceContext builds a code snippet around
// line:col showing 2 lines before/after with a caret.
func extractSourceContext(
    source string, line, col int, message string,
) string
```

### Output format

```
TypeError: Cannot read property 'id' of undefined

  1 | const c = tool.call({tool: "lookup", args: {}});
  2 | const id = c.output.id;
  3 | const name = c.output.details.name;
    |                      ^ Cannot read property 'name' of undefined
  4 | console.log(name);
```

### Error type handling

| Sobek Type | Handling |
|---|---|
| `*sobek.CompilerSyntaxError` | Extract line/col from `File.Position(Offset)`, show source context |
| `*sobek.Exception` | Use `String()` for full stack, extract first frame's Position for source context |
| `*sobek.InterruptedError` | Format as timeout message, include source context from first stack frame |
| Other `error` | Return `err.Error()` as-is |

### Tests — `error_test.go`

Table-driven with explicit `input` (source string + error) and `expected` (full formatted string):
- Syntax error at various positions (line 1, middle, last line)
- Runtime TypeError with stack trace
- ReferenceError (undefined variable)
- Custom `throw new Error("msg")`
- Timeout (InterruptedError)
- Single-line source
- Empty source string
- Error at column 1 (caret at start)

## Phase 2: Sobek Runtime — `toolchain/jsruntime/runtime.go`

Manages Sobek lifecycle. No ToolChain awareness.

### API

```go
type Config struct {
    Timeout      time.Duration // default 30s
    MaxCallStack int           // default 1024
}

func DefaultConfig() Config

type Result struct {
    ConsoleLog []string // captured console.log() calls
}

type Runtime struct { /* unexported fields */ }

func New(config Config) *Runtime

// RegisterFunc registers a synchronous Go function as
// a JS global. fn receives FunctionCall, returns Value.
func (r *Runtime) RegisterFunc(
    name string,
    fn func(call sobek.FunctionCall) sobek.Value,
)

// RegisterObject registers a Go object with methods as
// a JS global (e.g., tool.call, tool.parallelCall).
func (r *Runtime) RegisterObject(
    name string,
    methods map[string]func(sobek.FunctionCall) sobek.Value,
)

// Execute runs source with timeout and context
// cancellation. Returns Result or LLM-friendly error
// (via FormatError).
func (r *Runtime) Execute(
    ctx context.Context, source string,
) (*Result, error)
```

### Implementation details

- `New()` creates `sobek.New()`, registers `console.log` that appends to internal `[]string`, sets `SetMaxCallStackSize`.
- `Execute()`:
  1. Start `time.AfterFunc(timeout, func() { vm.Interrupt("timeout") })`
  2. Start goroutine watching `ctx.Done()` → `vm.Interrupt("cancelled")`
  3. Call `vm.RunString(source)`
  4. Cancel timer + context watcher
  5. On error: return `FormatError(source, err)` wrapped in a custom error type that preserves both the formatted message and the original error
  6. On success: return `Result{ConsoleLog: captured}`

### Tests — `runtime_test.go`

Table-driven:
- Execute simple expression, verify no error
- Execute with registered Go function, verify function called
- RegisterObject with methods, verify `obj.method()` callable
- console.log capture (single, multiple, mixed types)
- Timeout on `while(true){}`
- Context cancellation interrupts execution
- Syntax error returns formatted error
- Runtime error (ReferenceError) returns formatted error
- Stack overflow returns error
- Empty source string

## Phase 3: Tool Bridge — `toolchain/jsruntime/bridge.go`

Connects JS `tool.call()` / `tool.parallelCall()` to Go. Uses a callback function to execute tool calls — no direct ToolChain dependency.

### API

```go
// ToolCallFn executes a tool call given JSON content.
// Returns the ToolChainResult from the wrapped ToolChain.
// The JSON content is in the same format the wrapped
// ToolChain expects (e.g., {"tool":"x","args":{...}}).
type ToolCallFn func(content string) (
    *gent.ToolChainResult, error,
)

// RegisterToolBridge registers tool.call() and
// tool.parallelCall() on the given Sobek runtime.
//
// tool.call({tool: "name", args: {...}})
//   → returns {name, output} or {name, error}
//
// tool.parallelCall([{tool, args}, ...])
//   → returns [{name, output}, ...]
func RegisterToolBridge(
    vm *sobek.Runtime,
    callFn ToolCallFn,
)
```

### Implementation

**`tool.call(request)`:**
1. Export JS argument to `map[string]any` via `vm.ExportTo()`
2. `json.Marshal()` → JSON string like `{"tool":"x","args":{...}}`
3. Call `callFn(jsonString)` → `*ToolChainResult`
4. Parse `ToolChainResult.Raw` to build JS return object: `{name, output}` or `{name, error}`
5. Return as `vm.ToValue(result)`

**`tool.parallelCall(requests)`:**
1. Export JS array to `[]map[string]any`
2. `json.Marshal()` → JSON array `[{...},{...}]`
3. Call `callFn(jsonArrayString)` once — the wrapped ToolChain handles arrays natively
4. Parse `ToolChainResult.Raw.Results` to build JS array of results
5. Return as `vm.ToValue(results)`

Key insight: both methods produce JSON in the exact format the wrapped ToolChain expects. The bridge is purely a marshaling layer.

### Tests — `bridge_test.go`

Uses a mock `ToolCallFn` that records calls and returns preset results:
- Single tool.call with valid args → verify JSON passed to callFn, verify JS return value
- tool.call when callFn returns error → verify JS gets `{name, error: "message"}`
- tool.parallelCall with 2 calls → verify JSON array passed, verify JS array returned
- tool.parallelCall with empty array → verify no error
- tool.call with non-object argument → verify JS gets error
- tool.call with missing "tool" field → verify error propagated
- Verify `ToolChainResult.Raw` is correctly mapped to JS return structure

## Phase 4: Stats Keys & Default Limits

### `stats_keys.go` — append new keys

```go
// Code execution tracking keys (Programmatic Tool Calling).
//
// Auto-updated by JsToolChainWrapper when code blocks
// are executed.
const (
    // Counter: total code blocks executed
    SCCodeExecutions StatKey = "gent:code_executions"

    // Counter: code blocks that failed
    SCCodeExecutionsError StatKey = "gent:code_executions_error"

    // Gauge: consecutive code execution errors
    // (reset on success)
    SGCodeExecutionsErrorConsecutive StatKey = "gent:code_executions_error_consecutive"
)
```

### `limit.go` — add to DefaultLimits()

```go
// Stop after 3 consecutive code execution errors
{
    Type:     LimitExactKey,
    Key:      SGCodeExecutionsErrorConsecutive,
    MaxValue: 3,
},
```

## Phase 5: JsToolChainWrapper — `toolchain/js_wrapper.go`

### Construction

```go
func NewJsToolChainWrapper(
    wrapped gent.ToolChain,
) *JsToolChainWrapper

func (w *JsToolChainWrapper) WithCodeTimeout(
    d time.Duration,
) *JsToolChainWrapper

func (w *JsToolChainWrapper) WithCodeGuidance(
    guidance string,
) *JsToolChainWrapper

func (w *JsToolChainWrapper) WithInnerFormat(
    f gent.TextFormat,
) *JsToolChainWrapper
```

Default inner format: `format.NewXML()` with `direct_call` and `code` sections registered at construction time (via lightweight TextSection implementations that return the section name and empty guidance).

### Interface delegation

| Method | Behavior |
|---|---|
| `Name()` | Delegate to `wrapped.Name()` |
| `RegisterTool(tool)` | Delegate to `wrapped.RegisterTool(tool)` |
| `AvailableToolsPrompt()` | Delegate to `wrapped.AvailableToolsPrompt()`, append JS environment description |

### `Guidance()` — combined guidance

Returns text showing both modes:

```
You can call tools in two ways:

1. Direct call — for simple single or parallel tool calls:
<direct_call>
{wrapped ToolChain's Guidance() here}
</direct_call>

2. Programmatic — for multi-step orchestration with logic:
<code>
// Sequential calls (use result of first in second):
const customer = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
const orders = tool.call(
  {tool: "get_orders",
   args: {customer_id: customer.output.id}}
);

// Parallel calls:
const results = tool.parallelCall([
  {tool: "tool1", args: {...}},
  {tool: "tool2", args: {...}},
]);

// Output results (only console.log output is returned):
console.log(JSON.stringify({customer, orders}));
</code>

Choose direct_call for simple operations. Choose code when
you need to chain results, apply conditions, or loop.
```

### `ParseSection()` — sub-section detection

1. Call `innerFormat.Parse(nil, content)` (nil execCtx — no stats pollution)
2. Check for `direct_call` → delegate to `wrapped.ParseSection(execCtx, directContent)`
3. Check for `code` → return code content as `string` (executed in `Execute()`)
4. If both present → use `direct_call` (simpler, preferred path)
5. If neither → fallback: try `wrapped.ParseSection(execCtx, content)` (graceful degradation when LLM omits sub-section tags)
6. On parse error → `execCtx.PublishParseError(ParseErrorTypeToolchain, content, err)`

### `Execute()` — main routing

```
1. Call innerFormat.Parse(nil, content) to detect mode
2. If direct_call:
   → Call wrapped.Execute(execCtx, directContent, textFormat)
   → Return result as-is
3. If code:
   a. IncrCounter(SCCodeExecutions, 1)
   b. Create jsruntime.Runtime with configured timeout
   c. Create ToolCallFn closure:
      func(jsonContent string) (*ToolChainResult, error) {
          return w.wrapped.Execute(
              execCtx, jsonContent, textFormat,
          )
      }
   d. RegisterToolBridge(vm, callFn)
   e. Execute code via runtime.Execute(ctx, codeContent)
   f. On success:
      - ResetGauge(SGCodeExecutionsErrorConsecutive)
      - Build ToolChainResult from console.log output
      - Collect all Media from accumulated tool results
   g. On error:
      - IncrCounter(SCCodeExecutionsError, 1)
      - IncrGauge(SGCodeExecutionsErrorConsecutive, 1)
      - Format error as observation section
      - Return ToolChainResult with error text
4. If neither (fallback):
   → Same as direct_call path
```

### Result collection for code path

The `ToolCallFn` closure accumulates results. After code execution:
- `Text`: console.log output, joined by newlines. If empty, use "Code executed successfully."
- `Media`: merged from all `ToolChainResult.Media` across all sub-calls
- `Raw`: merged `ToolChainResult.Raw` from all sub-calls (all Calls, Results, Errors concatenated)

### Thread safety

No mutex needed. The wrapper itself has no mutable state after construction. The wrapped ToolChain handles its own synchronization (SearchJSON uses RWMutex). Sobek runtimes are created per-execution and not shared.

## Phase 6: Tests — `toolchain/js_wrapper_test.go`

All tests wrap `SearchJSON` as the inner ToolChain.

### Test helper

```go
func setupJsWrapper() *JsToolChainWrapper {
    searchTC := NewSearchJSON(SearchHintSimpleList)
    searchTC.RegisterTool(/* indexable tools */)
    searchTC.RegisterEngine(/* mock engine */)
    searchTC.Initialize()
    return NewJsToolChainWrapper(searchTC)
}
```

Tools for testing:
- `lookup_customer` — returns customer JSON by ID
- `get_orders` — returns orders JSON by customer_id
- `fail_tool` — always returns error
- `slow_tool` — sleeps (for timeout tests, if needed)

### Test categories

**A. Name / Guidance / AvailableToolsPrompt:**
- `Name()` returns wrapped ToolChain's name
- `Guidance()` contains both `<direct_call>` and `<code>` sections
- `Guidance()` contains wrapped ToolChain's guidance inside direct_call
- `AvailableToolsPrompt()` contains wrapped ToolChain's prompt + JS env description
- Custom guidance via `WithCodeGuidance()`

**B. ParseSection — sub-section detection:**
- Content with `<direct_call>` only → delegates to wrapped ParseSection
- Content with `<code>` only → returns code string
- Content with both → prefers direct_call
- Content with neither → fallback to wrapped ParseSection
- Malformed sub-sections → proper error handling
- Empty content → error

**C. Execute — direct_call passthrough:**
- Single tool call passes through to SearchJSON
- Multiple parallel tool calls pass through
- Search tool call (tool_registry_search) works through wrapper
- Error from SearchJSON propagates correctly
- Stats (SCToolCalls, etc.) are tracked by wrapped ToolChain

**D. Execute — code execution:**
- Simple `tool.call()` → executes tool, console.log output in result
- Chained calls → result of first used as arg in second
- `tool.parallelCall()` → multiple tools executed, results returned
- console.log with multiple calls → all output captured
- No console.log → "Code executed successfully" fallback
- JS syntax error → LLM-friendly error with source context in result
- JS runtime error (ReferenceError) → LLM-friendly error in result
- Tool call error within code → error returned in tool.call result, code continues
- Code timeout → proper error and stats

**E. Execute — stats tracking:**
- `SCCodeExecutions` incremented for each code block
- `SCCodeExecutionsError` incremented on code error
- `SGCodeExecutionsErrorConsecutive` incremented on error
- `SGCodeExecutionsErrorConsecutive` reset on successful code execution
- Tool call stats (SCToolCalls, SCToolCallsFor) flow through wrapped ToolChain from code path
- Direct call stats flow through wrapped ToolChain unchanged
- Code execution does NOT double-count tool call stats

**F. Execute — edge cases:**
- Empty code block
- Code that calls no tools (just console.log)
- Code that calls tool.call with malformed request
- Multiple code sections in same action (only first used)
- Context cancellation during code execution

## Phase 7: Limit Tests — `agents/react/agent_executor_limits_test.go`

Following existing patterns (see existing limit tests in that file):

- **Code execution error limit exceeded at iteration 1**: Set `SGCodeExecutionsErrorConsecutive` limit to 0, verify `TerminationLimitExceeded`
- **Code execution error limit exceeded at iteration N**: Multiple iterations, code errors accumulate, limit triggers at correct iteration
- **Consecutive gauge resets on success**: Error → success → error sequence, verify gauge tracking
- **Tool call errors from code path trigger tool call limits**: Code calls a tool that errors, verify `SGToolCallsErrorConsecutive` is tracked (flows through wrapped ToolChain)

## Implementation Order

1. **Phase 0**: Assumption verification (spike tests, throwaway code)
2. **Phase 1**: `jsruntime/error.go` + `error_test.go` — zero dependencies
3. **Phase 2**: `jsruntime/runtime.go` + `runtime_test.go` — depends on Phase 1
4. **Phase 3**: `jsruntime/bridge.go` + `bridge_test.go` — depends on Phase 2
5. **Phase 4**: Stats keys + default limits — just declarations
6. **Phase 5**: `js_wrapper.go` — depends on all above
7. **Phase 6**: `js_wrapper_test.go` — wrapping SearchJSON
8. **Phase 7**: Limit tests in `agent_executor_limits_test.go`

Each phase is independently testable. Phases 1-3 have zero gent ToolChain dependencies and can be developed/reviewed in isolation.

## Verification

1. `go test ./toolchain/jsruntime/...` — runtime, error formatter, bridge tests pass
2. `go test ./toolchain/...` — wrapper tests pass (wrapping SearchJSON)
3. `go test ./agents/react/...` — limit tests pass
4. `go test ./...` — full test suite passes, no regressions
5. Existing SearchJSON tests still pass (wrapper doesn't modify wrapped ToolChain behavior)

## Key Files to Modify

| File | Change |
|---|---|
| `stats_keys.go` | Add SCCodeExecutions, SCCodeExecutionsError, SGCodeExecutionsErrorConsecutive |
| `limit.go` | Add SGCodeExecutionsErrorConsecutive to DefaultLimits() |
| `go.mod` | Add `github.com/grafana/sobek` dependency |

## Key Files to Create

| File | Purpose |
|---|---|
| `toolchain/jsruntime/error.go` | LLM-friendly Sobek error formatting |
| `toolchain/jsruntime/error_test.go` | Error formatter tests |
| `toolchain/jsruntime/runtime.go` | Sobek runtime lifecycle |
| `toolchain/jsruntime/runtime_test.go` | Runtime tests |
| `toolchain/jsruntime/bridge.go` | JS→Go tool call bridge |
| `toolchain/jsruntime/bridge_test.go` | Bridge tests |
| `toolchain/js_wrapper.go` | JsToolChainWrapper |
| `toolchain/js_wrapper_test.go` | Wrapper tests (wrapping SearchJSON) |

## Key Existing Files to Reference

| File | Why |
|---|---|
| `toolchain/search.go` | Pattern for ToolChain wrapping, Execute flow, stats |
| `toolchain/search_test.go` | Test patterns, mock tools, setupSearchJSON helper |
| `toolchain/json.go` | Simpler ToolChain pattern for reference |
| `format/xml.go` | Inner TextFormat for sub-section parsing |
| `agents/react/agent_executor_limits_test.go` | Limit test patterns |

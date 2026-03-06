# LLM-Friendly Schema Validation Errors for PTC

## Context

When the LLM writes `<code>` blocks in PTC mode, schema validation errors are noisy
(`"schema validation failed: jsonschema validation failed with 'file:///...'"`), show only
one issue at a time, and lack context about which JS line caused the error. This wastes tokens
across multiple correction iterations. Three improvements:

1. **Clean error format** — show all required fields with types + what went wrong
2. **Code context** — show which `tool.call()` line in JS triggered the error
3. **AST pre-validation** — validate ALL tool calls before executing any code

## Files to Modify

| File | Change |
|------|--------|
| `schema/schema.go` | Add `FormatForLLM()`, `DescribeFields()` |
| `schema/schema_test.go` | Tests for new methods |
| `toolchain/schema_provider.go` | New `SchemaProvider` interface |
| `toolchain/json.go` | Implement `SchemaProvider` |
| `toolchain/yaml.go` | Implement `SchemaProvider` |
| `toolchain/search.go` | Implement `SchemaProvider` |
| `toolchain/jsruntime/bridge.go` | Enhanced error with code context |
| `toolchain/jsruntime/bridge_test.go` | Tests for enhanced errors |
| `toolchain/jsruntime/prevalidate.go` | AST-based pre-validation (new) |
| `toolchain/jsruntime/prevalidate_test.go` | Tests (new) |
| `toolchain/js_wrapper.go` | Wire pre-validation + pass source to bridge |
| `toolchain/js_wrapper_test.go` | Tests for pre-validation integration |

## Phase 1: Schema — LLM-Friendly Error Formatting

### `schema/schema.go`

**New method `DescribeFields() string`:**

Reads `raw` schema to produce a list of ALL properties with types and descriptions.
For properties that are required, marks them with `(required)`.

```
- 'order_id' (required, string): The order ID related to the case
- 'details' (required, string): Description of the issue and resolution steps attempted
```

For complex types:
- array: `(required, array of string)` or `(required, array of object)`
- object with properties: `(required, object)` with a single-line JSON example
  of a minimal valid value

Implementation:
- Iterate `raw["properties"]` map
- For each, extract `type`, `description` from the property map
- Check `raw["required"]` to mark required fields
- For arrays, check `items.type`; for objects, generate a minimal example from properties

**New method `FormatForLLM(toolName string, data map[string]any) string`:**

Calls `Validate(data)`. If no error, returns "". Otherwise, produces:

```
Invalid args for tool 'create_case'.
Errors:
  - missing required property 'details'
  - got string for 'count', want integer
Expected fields:
  - 'order_id' (required, string): The order ID related to the case
  - 'details' (required, string): Description of the issue...
IMPORTANT: Use EXACT argument names and types from the tool schema.
```

Implementation:
- Call `s.compiled.Validate(data)` → get `*jsonschema.ValidationError`
- Use `err.BasicOutput()` to get flat list of `OutputUnit` with clean error messages
- Strip noisy prefixes (file paths, `at ''`, redundant labels)
- Append `DescribeFields()` output
- Add reinforcement text

### `schema/schema_test.go` — New tests

Table-driven with explicit `input`/`expected` structs.

**DescribeFields tests:**
- Simple types: string, integer, number, boolean — verify type names
- Required vs optional fields — verify `(required)` marker
- Array type with string items — verify `array of string`
- Array type with object items — verify `array of object`
- Object with nested properties — verify `object` with example
- Empty properties — verify empty output
- Nil schema — verify no panic

**FormatForLLM tests:**
- Missing single required field — verify field listed in errors + full expected fields shown
- Missing multiple required fields — verify ALL listed
- Wrong type — verify type mismatch shown
- Extra unknown fields — verify listed (if detectable)
- Valid data — verify returns ""
- Nil data — verify helpful message
- Complex schema (array + nested object) — verify clean output
- Verify no file paths or noisy prefixes in output

## Phase 2: SchemaProvider Interface

### `toolchain/schema_provider.go` (new file)

```go
// SchemaProvider allows access to tool schemas by name.
// Implemented by toolchains that support schema validation.
type SchemaProvider interface {
    GetToolSchema(name string) *schema.Schema
}
```

### Implement on existing toolchains

**`toolchain/json.go`:**
```go
func (c *JSON) GetToolSchema(name string) *schema.Schema {
    return c.schemaMap[name]
}
```

**`toolchain/yaml.go`:**
```go
func (c *YAML) GetToolSchema(name string) *schema.Schema {
    return c.schemaMap[name]
}
```

**`toolchain/search.go`:**
```go
func (c *SearchJSON) GetToolSchema(name string) *schema.Schema {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.schemaMap[name]
}
```

No new tests needed for these — they're trivial accessors tested indirectly
through the pre-validation tests.

## Phase 3: Bridge — Enhanced Error with Code Context

### `toolchain/jsruntime/bridge.go`

**Change `RegisterToolBridge` signature:**

```go
func RegisterToolBridge(
    rt *Runtime,
    callFn ToolCallFn,
    source string,             // JS source for error context
    schemaFn SchemaLookupFn,   // optional, for enhanced errors
)

// SchemaLookupFn returns the schema for a tool name.
// Returns nil if no schema exists.
type SchemaLookupFn func(name string) *schema.Schema
```

**In `toolCall()` — enhance schema validation errors:**

After calling `callFn` and getting back a result with a schema validation error:
1. Type-assert `raw.Errors[0]` to `*schema.ValidationError`
2. If match, call `schema.FormatForLLM(toolName, data)` to get clean message
3. Call `vm.CaptureCallStack(1, nil)` to get JS line number
4. Use existing `extractSourceContext()` to build code snippet
5. Combine: code context + clean error + reinforcement
6. Put enhanced error string in `jsResult["error"]`

Same enhancement in `buildParallelResults()` for each failed call.

**Result format for JS:**
```
tool.call() error at line 3:

  2 | const caseResult = tool.call({
  3 |   tool: "create_case",
    |   ^ schema validation error
  4 |   args: { customer_id: "C001", order_id: "ORD-1007", description: "..." }

Invalid args for tool 'create_case'.
Errors:
  - missing required property 'details'
Expected fields:
  - 'order_id' (required, string): The order ID related to the case
  - 'details' (required, string): Description of the issue...

IMPORTANT: Use EXACT argument names and types from the tool schema.
```

### `toolchain/jsruntime/bridge_test.go` — New tests

- Schema error returns enhanced format with field descriptions
- Schema error includes code context with correct line number
- Schema error for parallel calls shows per-call enhanced errors
- Non-schema errors are unchanged (no enhanced formatting)
- Nil schemaFn does not crash (graceful degradation)
- Multiple missing fields all shown in single error

## Phase 4: AST Pre-Validation

### `toolchain/jsruntime/prevalidate.go` (new file)

**Types:**

```go
// ToolCallSite represents a tool.call() found in JS source.
type ToolCallSite struct {
    ToolName string         // extracted from literal
    Args     map[string]any // extracted from literal object (nil if dynamic)
    Line     int            // JS source line
    Column   int            // JS source column
    IsDynamic bool          // true if args couldn't be extracted statically
}

// PreValidationError represents a schema validation failure
// found during pre-validation.
type PreValidationError struct {
    Site         ToolCallSite
    ErrorMessage string // LLM-friendly error from FormatForLLM
}
```

**Functions:**

```go
// FindToolCalls parses JS source and extracts all
// tool.call() and tool.parallelCall() invocations.
// Returns found call sites. Calls with dynamic args
// (variable references) have IsDynamic=true and nil Args.
func FindToolCalls(source string) ([]ToolCallSite, error)

// PreValidate finds all tool.call() in source and validates
// literal args against schemas. Returns errors for ALL
// calls that fail validation. Skips calls with dynamic args.
func PreValidate(
    source string,
    schemaFn SchemaLookupFn,
) ([]PreValidationError, error)

// FormatPreValidationErrors formats all pre-validation
// errors into a single LLM-friendly message with code
// context for each failing call.
func FormatPreValidationErrors(
    source string,
    errors []PreValidationError,
) string
```

**AST Walking (internal):**

- `sobek.Parse("code.js", source)` → `*ast.Program`
- Walk `Program.Body` recursively
- For each `*ast.CallExpression`:
  - Check if callee is `tool.call` or `tool.parallelCall` via DotExpression
  - Extract first argument
  - If `*ast.ObjectLiteral`: extract `tool` (StringLiteral) and `args` (ObjectLiteral)
  - If args is ObjectLiteral: recursively extract literal values → `map[string]any`
  - If any part is dynamic (Identifier, CallExpression, etc): mark `IsDynamic=true`
- For `tool.parallelCall`: argument is ArrayLiteral of ObjectLiterals
- Use `Program.File.Position(node.Idx0())` for line/column

**AST literal extraction (internal):**

```go
// extractLiteralValue converts an AST Expression to a Go
// value if it's a literal. Returns (value, true) for
// literals, (nil, false) for dynamic expressions.
func extractLiteralValue(expr ast.Expression) (any, bool)
```

Handles: StringLiteral, NumberLiteral, BooleanLiteral, NullLiteral,
ObjectLiteral (recursive), ArrayLiteral (recursive).
Returns false for Identifier, CallExpression, etc. (dynamic).

### `toolchain/jsruntime/prevalidate_test.go` — Tests

**FindToolCalls tests:**
- Single `tool.call()` with literal object → extracts name, args, line
- `tool.call()` with dynamic args (variable) → IsDynamic=true, Args=nil
- `tool.call()` with mixed literal/dynamic args → IsDynamic=true
- `tool.parallelCall()` with array of literals → extracts all calls
- `tool.parallelCall()` with dynamic array → IsDynamic=true
- Multiple `tool.call()` in sequence → finds all
- `tool.call()` inside if/for blocks → still found
- No tool calls → empty result
- Syntax error in source → returns parse error
- Nested `tool.call()` (call inside callback arg) → found
- String literal with escaped quotes → extracted correctly
- Number literal (int and float) → extracted correctly
- Boolean and null literals → extracted correctly
- Object literal as args value → extracted recursively
- Array literal as args value → extracted correctly
- Tool name from variable (not literal) → IsDynamic=true

**PreValidate tests:**
- Single call with missing required field → returns 1 error with clean message
- Multiple calls with different errors → returns all errors
- Call with valid args → no error for that call
- Call with dynamic args → skipped (no error, no validation)
- Mixed static/dynamic calls → only static ones validated
- Nil schemaFn → returns empty (no validation possible)
- Tool with no schema → skipped
- Empty source → no errors
- All calls valid → empty errors

**FormatPreValidationErrors tests:**
- Single error → code context + error message
- Multiple errors → each with own code context, all in one message
- Verify code context shows correct lines
- Verify reinforcement text at end

## Phase 5: Wire It All Up

### `toolchain/js_wrapper.go`

**In `executeCode()`:**

Before creating the runtime and executing:

1. Check if wrapped toolchain implements `SchemaProvider`
2. If yes, call `jsruntime.PreValidate(code, provider.GetToolSchema)`
3. If pre-validation returns errors, format with `FormatPreValidationErrors()`
4. Return error result immediately (no code execution)
5. Still increment `SCCodeExecutions` and error counters

If pre-validation passes (or SchemaProvider not available):
- Continue with existing execution flow
- Pass `source` and `schemaFn` to `RegisterToolBridge()` for runtime error enhancement

### `toolchain/js_wrapper_test.go` — New tests

**Pre-validation integration tests:**
- Code with invalid literal args → pre-validation catches, no tool executed
- Code with valid literal args → pre-validation passes, tools execute
- Code with dynamic args → pre-validation skips, normal execution
- Code with mix of invalid literal + dynamic → invalid caught, dynamic skipped
- Multiple invalid calls → ALL errors reported in one response
- Wrapped toolchain without SchemaProvider → pre-validation skipped, normal execution
- Pre-validation error increments code execution error stats
- Pre-validation error resets consecutive gauge on next success

## Verification

1. `go test ./schema/...` — new DescribeFields + FormatForLLM tests
2. `go test ./toolchain/jsruntime/...` — bridge + prevalidate tests
3. `go test ./toolchain/...` — wrapper integration tests
4. `go test ./...` — full suite, no regressions
5. Manual: run integration CLI with PTC enabled, verify enhanced error messages

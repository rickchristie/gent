<codebase_architecture>
# Gent - Go LLM Agent Framework

## Overview
Gent is a flexible framework for building LLM agents in Go. It provides core interfaces
for custom agent patterns and a few standard implementations that you can use in production. The
goal is to make it easy to write and experiment with custom agent loops.

## File Organization
- **Root `.go` files**: Interface/type definitions (e.g., `agent.go`, `model.go`)
- **Subdirectories**: Implementations (e.g., `executor/`, `agents/react/`, `models/`)

## High-Level Features & Responsibilities

### Executor + AgentLoop
- Interface: `agent.go` (AgentLoop, LoopData)
- Implementation: `executor/executor.go`
- Executor runs the loop: BeforeExecution → [BeforeIteration → AgentLoop.Next() → AfterIteration]* → AfterExecution
- AgentLoop.Next() returns `LAContinue` (keep looping) or `LATerminate` (stop with result)
- ReAct agent: `agents/react/agent.go` - parses LLM output → executes tools OR validates answer

### LoopData
- Defined in: `agent.go`
- Stores Task (user input) and iteration history for the agent loop
- BasicLoopData is the default; embed it in custom struct for additional fields
- Scratchpad: iterations used in next LLM call (can be compacted)
- IterationHistory: full history (never compacted, for observability)
- SIDE EFFECT: AddIterationHistory/SetScratchPad publish CommonDiffEvent for change tracking

### Model
- Interface: `model.go`
- Implementation: `models/` (e.g., `models/lcg.go` for LangChainGo wrapper)
- Wraps LLM provider, normalizes token count stats across OpenAI/Anthropic/Google/etc.
- SIDE EFFECT: AfterModelCallEvent auto-increments input_tokens, output_tokens stats
- SIDE EFFECT: Must emit chunks via execCtx.EmitChunk() for streaming subscribers

### ToolChain
- Interface: `toolchain.go`
- Implementations: `toolchain/yaml.go`
- Parses tool calls from LLM output (YAML or JSON format)
- Validates args against JSON Schema, transforms to typed input, executes Tool.Call()
- AvailableToolsPrompt(): generates tool catalog with schemas for system prompt
- Guidance(): instructions on tool call syntax (inherited from TextSection)
- SIDE EFFECT: BeforeToolCallEvent auto-increments tool_calls, tool_calls:<name>
- SIDE EFFECT: AfterToolCallEvent with error increments error counters/gauges
- SIDE EFFECT: Success resets consecutive error gauges

### Termination + Validator
- Interface: `termination.go`
- Implementations: `termination/text.go`, `termination/json.go`
- Parses answer section, runs optional AnswerValidator
- Returns: Continue (no answer), AnswerRejected (with feedback), AnswerAccepted
- SIDE EFFECT: ValidatorResultEvent with rejection increments answer_rejected counter

### TextFormat + TextSection
- Interfaces: `format.go` (TextFormat), `section/` (TextSection)
- Implementations: `format/xml.go`, `format/markdown.go`, `section/yaml.go`, `section/json.go`
- TextFormat: envelope parsing (<tags> or # headers), section extraction
- TextSection: content parsing within a section (text passthrough, JSON, YAML)
- DescribeStructure(): generates output format instructions for system prompt
- SIDE EFFECT: ParseErrorEvent increments parse error counters/gauges by type

### Stats + Limits
- Defined in: `stats.go`, `stats_keys.go`, `limit.go`
- `StatKey` type with `Self()` and `IsSelf()` methods
- SC prefix = Counter (monotonic, always propagated, has $self: counterpart)
- SG prefix = Gauge (can go up/down, never propagated, local-only)
- $self:-prefixed keys track per-context only (no children)
- Counters: IncrCounter only (no Set/Reset, panics on negative)
- Gauges: IncrGauge, SetGauge, ResetGauge (used for consecutive errors)
- Limits: checked on EVERY stats update, cancels context when exceeded
- SIDE EFFECT: LimitExceededEvent published, then context.CancelCause() called

### ExecutionContext
- Defined in: `context.go`
- Central hub: holds LoopData, Stats, Events, Limits, streaming subscriptions
- SpawnChild() creates nested context with shared stats propagation
- All PublishXXX() methods: record event → update stats → check limits → notify subscribers

## Data Flow (ReAct Agent)
1. Executor.Run() → creates ExecutionContext with LoopData, Stats, Limits
2. BeforeExecution hook → agent builds system prompt (tools, format instructions)
3. Loop iteration:
   - Model.Call() → LLM response (streams chunks, increments token stats)
   - TextFormat.Parse() → extracts sections (thought, action, answer)
   - If action: ToolChain.Process() → validate, execute Tool.Call(), append result to scratchpad
   - If answer: Termination.Process() → validate, run AnswerValidator → accept/reject
4. On limit exceeded: context canceled, TerminationLimitExceeded returned
5. On accepted answer: TerminationSuccess with result

## Stats Keys (gent: prefix)
### Counters (SC*, propagated, $self: counterpart)
- SCIterations (PROTECTED from user IncrCounter)
- SCInputTokens, SCInputTokensFor (+ model)
- SCOutputTokens, SCOutputTokensFor (+ model)
- SCToolCalls, SCToolCallsFor (+ tool)
- SCToolCallsErrorTotal, SCToolCallsErrorFor (+ tool)
- SCFormatParseErrorTotal
- SCToolchainParseErrorTotal
- SCTerminationParseErrorTotal
- SCSectionParseErrorTotal
- SCAnswerRejectedTotal, SCAnswerRejectedBy (+ validator)

### Gauges (SG*, local-only, never propagated)
- SGFormatParseErrorConsecutive
- SGToolchainParseErrorConsecutive
- SGSectionParseErrorConsecutive
- SGTerminationParseErrorConsecutive
- SGToolCallsErrorConsecutive
- SGToolCallsErrorConsecutiveFor (+ tool)

## Limits
- LimitExactKey - match specific key
- LimitKeyPrefix - match any key with prefix
- Use Key.Self() for per-context limits (excludes children)
- DefaultLimits uses SCIterations.Self() for per-context iteration limit
- Executor has default limits
</codebase_architecture>

<testing_standards>
**General Standards**
- Always use table-driven tests for functions with multiple scenarios.
- For table driven tests, create explicit `input`, `mocks` (if needed), and `expected` structs.
- The expected struct should contain all expected outputs, including errors.
- Expected output must be FULL matches, for example:
  - For strings, compare entire string, not substrings.
  - For structs, compare entire struct, not just individual fields.
  - For fields that contains timestamps, assert >= expected time instead of exact match, DO NOT use "not zero" assertion for timestamps!
- The process being tested MUST be close to real-world as possible.
  Avoid changing internals to simulate scenarios, i.e. call public methods instead of changing private fields.
- No vanity tests. Tests must validate real functionality/feature that user expects.
- Use testify's `assert` package for assertions.

**Testing Stats & Limits**
- Whenever we create new stats (`*ExecutionStats`), we need to create tests on:
  - Stat field is incremented correctly.
  - If consecutive, stat field is reset correctly.
  - New limit test at `agent/agent_executor_test.go` with mocks.
- Limit tests must cover:
  - Limit is exceeded in first iteration.
  - Limit is exceeded in Nth iteration.
- If the stat is based on prefix, then the limit test must:
  - Have multiple prefixed stats, e.g. `gent:tool_calls:tool1`, `gent:tool_calls:tool2`.
  - Test must verify that when both limits are reached, only the correct one triggers the limit error.
</testing_standards>

<standards_convention>
- MAX line length: 100 characters.
- NEVER swallow errors. Errors must be either returned, or logged.
</standards_convention>

<critical_rules>
**Never commit and push without asking permission!**
</critical_rules>

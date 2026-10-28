<critical_behavior>
# Critical Behavior
- **IMPORTANT**: Gent is in development. Change APIs freely; no backward compatibility is needed.
- **ALWAYS fully finish your tasks** when executing anything. Never stop to ask "would you like to continue?" or anything similar.
  You are given tasks, fully complete them, don't waste our time. Exception: Destructive actions with irreversible consequences.
- **ALWAYS verify your work and assumptions**, don't just read the code, actually test what you're doing.
  Write tests, write scripts to test behaviors, run playwright to check console, take screenshots, run the commands.
- **NEVER blame without evidence**, don't say something like "X fail due to Y", find evidence!
  **Always investigate to find root cause**, if unable to find evidence, state why and clarify it's a hypothesis.
- **ALWAYS write proper documentation**, write *why* it was done this way, and *how* only if it's not obvious.
  Write for a human/yourself when revisiting this code in the future, what's important so futureself work faster, fewer mistakes, less tokens?
- **NEVER execute staging, unstaging, or stashing git changes**, this will cause **loss of verification work** in our collaboration!
- **NEVER remove or edit changes that are not yours**, you are not the only one working in the repository.
  When you absolutely require to touch changes that are not yours, ASK permission.
- **NEVER delete valid comments**, contextual comments are important for maintainability.
  Removing comments that contains business context (the "why") will cause **loss of context**.
  Only remove comments if they are no longer valid.
- **ALWAYS** use ASD-STE100 Simplified Technical English (STE) in ALL your outputs, e.g. when communicating with me, writing documentations, code comments, variable names, function names, entity names, tests, comments.
- **ALWAYS prioritize readability, maintainability, simplicity, elegance** of your code.
  Aim for low-cyclomatic complexity in your code, exit early whenever you can, it's okay to repeat lines if we reduce cyclomatic
  complexity or prevent interleaving conditionals.
- **DO NOT excessively nil check**. Treat pointer/interface parameters/struct fields as required unless explicitly documented optional.
  If caller sends nil, then nil pointer panic is fine. Fail fast, unless we truly need the recovery.
- **Code simplicity**: Remove vestigial code and unused interfaces when found.
- **ALWAYS make main branch a single trunk line**. No merge commits, squash and rebase when needed.
</critical_behavior>

<critical_constraints>
# Critical Constraints

Apply these constraints in priority order:

1. Ensure reliable repeated use. Prevent race conditions, data races, and memory leaks.
2. Minimize CPU and memory use without reducing reliability or performance.
3. Prevent unnecessary work and delay. Maintain good performance.
</critical_constraints>

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
- ReAct agent: `agents/react/agent.go` - reads native calls → executes tools OR validates final content

### LoopData
- Defined in: `agent.go`
- Supplies Task (user input); ExecutionContext owns history and working context
- BasicLoopData is the default; embed it in custom struct for additional fields
- Scratchpad: ordered HistoryEntry IDs used in the next model call
- History: append-only originals, summaries, compaction decisions, and context transitions
- SIDE EFFECT: UpdateHistory queues structured StateChangeEvent; checkpoints accept it atomically
- History, storage, and continuation contract: docs/history.md

### Model
- Interface: `model.go`
- Implementation: `models/` (e.g., `models/lcg.go` for LangChainGo wrapper)
- Gent owns message/tool types and opaque provider continuation items
- OpenAI/Grok use direct Responses adapters; LangChainGo is used for lossless conversions
- Managed ExecutionContext calls record requests, chunks, responses, and usage before delivery
- Adapters return a Stream; they must Complete after their producer stops, including cancellation

### ToolChain
- Interface: `toolchain.go` (native calls only)
- Implementations: Registry, SearchChain, JsToolChainWrapper in `toolchain/`
- NativeTools selects definitions; PrepareCalls saves a plan; ExecuteNext executes one step
- Validates JSON arguments, converts typed input, runs hooks, and saves results
- Search derives discovered definitions from active run history; pinned tools stay visible
- PTC exposes run_code and saves inner calls separately from the outer plan
- Tool.Recover supplies each tool's recovery decision, optional steering, and recovered output
- Interrupted PTC returns partial evidence and guidance without restarting the script
- SIDE EFFECT: BeforeToolCallEvent increments tool-call counters; AfterToolCallEvent tracks errors
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
- PublishXXX() runs mutable hooks and queues frozen evidence; stats updates enforce limits
- FlushProgress/CommitCheckpoint accept evidence before StateChange and trace observers see it

## Data Flow (ReAct Agent)
1. Executor.Run() → creates ExecutionContext with LoopData, Stats, Limits
2. BeforeExecution hook → agent prepares prompt and execution settings
3. Loop iteration:
   - Model.Call() → LLM response (streams chunks, increments token stats)
   - ToolChain.NativeTools() supplies definitions for each model request
   - If native calls: PrepareCalls → ExecuteNext → append structured call/result history
   - If final content: Termination → validate, run AnswerValidator → accept/reject
4. On limit exceeded: context canceled, TerminationLimitExceeded returned
5. On accepted answer: TerminationSuccess with result

## Stats Keys (gent: prefix)
### Counters (SC*, propagated, $self: counterpart)
- SCIterations (PROTECTED from user IncrCounter)
- SCInputTokens, SCInputTokensFor (+ model)
- SCOutputTokens, SCOutputTokensFor (+ model)
- SCTotalTokens, SCTotalTokensFor (+ model)
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
- SGScratchpadLength
- SGInputTokensLastIteration, SGInputTokensLastIterationFor (+ model)
- SGOutputTokensLastIteration, SGOutputTokensLastIterationFor (+ model)
- SGTotalTokensLastIteration, SGTotalTokensLastIterationFor (+ model)

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
- Define `input`, `expected`, etc. as named types inside the test function — never use inline anonymous structs in table-driven tests.

**String Testing**
- For multi-line string inputs and expected outputs, ALWAYS use backticks (raw string literals) with real line breaks. NEVER use `"" + ""` concatenation or `\n` escapes for multi-line strings.
- Assert exact string matches. Do NOT use `Contains`, `HasPrefix`, or other partial match shortcuts. If the full expected output is known, assert it exactly.

**Running Tests**
- Always run full test suites over `./...` after the final changes, including native, CGO-disabled,
  and race configurations. Targeted checks never replace a full suite.
- Run tests without `-v` and without piping to `grep`. Go test already prints full output for failing packages — no filtering needed.
- Use 300s timeout to accommodate ONNX embedder index builds in policy/search tests.
- Example: `go test ./... -count=1 -timeout 300s`
- Portable suite: `CGO_ENABLED=0 go test ./... -count=1 -timeout 300s`
- Race suite: `go test -race ./... -count=1 -timeout 300s`

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
- MAX line length: 100 characters, DO NOT break line before 100 chars.
- NEVER swallow errors. Errors must be either returned, or logged.
</standards_convention>

<critical_rules>
**Never commit and push without asking permission!**
</critical_rules>

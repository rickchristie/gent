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

### Runner + Loop[T]
- Interface: `agent.go` (`Loop[T]`, `LoopData`)
- Implementation: `executor/executor.go` (`Runner[T]`)
- Runner manages lifecycle, limits, checkpoints, and resource release
- `Loop[T].Next` returns `LAContinue` or `LATerminate`; `Output()` defines the accepted type
- `Execute` and `Resume` return `(T, *ExecutionResult, error)`
- ReAct: `agents/react/` reads native completion, runs tools, or processes a final candidate
- Accepted output is saved before publication; terminal restore does not run processing again

### LoopData and Task
- `Task.Messages` contains `[]*ModelMessage`; each message carries ordered `[]ContentPart`
- Default examples use a caller-authored system message followed by a user message
- `NewTask(parts...)` supplies a convenient single user message
- ExecutionContext freezes the Task and owns history and working context
- The Task stays outside compaction; a request hook cannot change its saved content
- BasicLoopData is the default; embed it for additional application fields
- Scratchpad: ordered HistoryEntry IDs used in the next model call
- History: append-only originals, summaries, compaction decisions, and context transitions
- UpdateHistory queues StateChangeEvent; checkpoints accept evidence atomically
- See `docs/history.md` for storage and continuation contracts

### Model
- Interface: `model.go`; native implementations: `models/`
- Supported: OpenAI, Anthropic, Gemini, xAI, DeepSeek, vLLM, Ollama, OpenRouter, llama.cpp, SGLang
- OpenAI uses Responses. Each adapter owns its native request and response mapping
- Use native API controls when the endpoint can enforce the declared contract
- Unsupported input types return an error; adapters do not convert input files
- Preserve native completion, refusal, tool IDs, and opaque continuation items
- Managed calls record requests, chunks, responses, and usage before delivery
- Adapters return a Stream and Complete only after producer cleanup, including cancellation
- Content and MetaContent retain ordered text and binary parts; only accepted output is Content
- See `docs/models.md` for endpoint-specific support and limitations

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

### Output[T]
- Interface: `output.go`; implementations: `output/`
- Built-ins: TextOutput, ImageOutput, JsonOutput[T], SchemaOutput[T]
- Instructions() supplies optional prompt text; only the caller decides to include it
- Configure requests native controls; Process validates and processes ordered content parts
- Always run local validation, then the optional OutputCustomValidator callback
- OutputResult exposes Accepted, Value, Content, and Steering; error means operation failure
- Retain a rejected candidate and its steering together through the next main model call
- Corrections use ordinary run limits; they have no separate retry counter
- OutputValidationEvent distinguishes local, custom, and overall processing decisions
- See `docs/output.md` for typed values, images, validation, and saved restoration

### TextFormat
- Interface: `format.go`; implementations: `format/xml.go`, `format/markdown.go`
- FormatSections renders tool-result text; it does not parse answers or build agent prompts

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
1. Runner.Execute configures a typed output contract on the caller's ExecutionContext
2. Each iteration builds a request from the frozen Task and active history
3. Native completion selects tool execution, continuation, refusal, failure, or output processing
4. Tool calls use PrepareCalls and ExecuteNext; results remain in structured history
5. Rejected output and steering stay provisional; accepted output saves Value and Content together
6. Limits cancel execution with their own termination reason
7. A successful terminal save permits typed return and accepted Content publication

## Stats Keys (gent: prefix)
### Counters (SC*, propagated, $self: counterpart)
- SCIterations (PROTECTED from user IncrCounter)
- SCInputTokens, SCInputTokensFor (+ model)
- SCOutputTokens, SCOutputTokensFor (+ model)
- SCTotalTokens, SCTotalTokensFor (+ model)
- SCToolCalls, SCToolCallsFor (+ tool)
- SCToolCallsErrorTotal, SCToolCallsErrorFor (+ tool)
- SCToolchainParseErrorTotal
- SCOutputRejectedTotal, SCOutputLocalRejectedTotal, SCOutputCustomRejectedTotal
- SCOutputCustomRejectedBy (+ named custom validator)

### Gauges (SG*, local-only, never propagated)
- SGOutputRejectedConsecutive, SGOutputLocalRejectedConsecutive, SGOutputCustomRejectedConsecutive
- SGToolchainParseErrorConsecutive
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
- Runner has default limits
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

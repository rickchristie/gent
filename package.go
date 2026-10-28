// Package gent provides a flexible framework for building LLM agents in Go.
//
// The library provides default implementations for common patterns like ReAct. However, the
// interfaces are generic enough to allow experimentation with different agent loop patterns,
// toolchains, termination strategies, or even custom graph/multi-agent patterns.
//
// # Quick Start: Customer Service Agent
//
// Here's a complete example of building a customer service agent:
//
//	package main
//
//	import (
//	    "context"
//	    "fmt"
//
//	    "github.com/rickchristie/gent"
//	    "github.com/rickchristie/gent/agents/react"
//	    "github.com/rickchristie/gent/executor"
//	    "github.com/rickchristie/gent/models"
//	    "github.com/rickchristie/gent/termination"
//	    "github.com/rickchristie/gent/toolchain"
//	)
//
//	func main() {
//	    // 1. Create a model
//	    model := models.NewOpenAI(apiKey, modelName)
//
//	    // 2. Create tools
//	    lookupOrder := gent.DefineTool("lookup_order", lookupOrderFromDB).
//	        Description("Look up order details by order ID").MustBuild()
//
//	    // 3. Create toolchain and register tools
//	    tc := toolchain.New().RegisterTool(lookupOrder)
//
//	    // 4. Create termination with optional validator
//	    term := termination.NewText("answer").
//	        WithGuidance("Provide a helpful response to the customer.")
//	    term.SetValidator(myAnswerValidator) // optional
//
//	    // 5. Build the agent
//	    agent := react.NewAgent(model).
//	        WithBehaviorAndContext("You are a helpful customer service agent.").
//	        WithToolChain(tc).
//	        WithTermination(term)
//
//	    // 6. Configure limits (optional - defaults are applied automatically)
//	    limits := []gent.Limit{
//	        {Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 20},
//	        {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 50000},
//	    }
//
//	    // 7. Create execution context with task
//	    task := &gent.Task{Text: "What's the status of order #12345?"}
//	    data := gent.NewBasicLoopData(task)
//	    execCtx := gent.NewExecutionContext(context.Background(), "customer-service", data)
//	    execCtx.SetLimits(limits)
//
//	    // 8. Run the agent
//	    exec := executor.NewRunner(agent, executor.DefaultConfig())
//	    exec.Execute(execCtx)
//
//	    // 9. Check results
//	    if execCtx.TerminationReason() == gent.TerminationSuccess {
//	        fmt.Println("Response:", execCtx.FinalResult())
//	    }
//	}
//
// # Tool & ToolChain
//
// Tools are the actions your agent can take. Each tool has typed input/output and focuses
// purely on business logic - formatting is handled by the ToolChain.
//
//	// Define a tool with typed I/O
//	tool := gent.DefineTool("search", searchFunc).
//	    Description("Search for information").
//	    Policy("Use the query supplied by the user.").MustBuild()
//
// ToolChain exposes native definitions, validates structured calls, and executes saved plans.
// Registry, SearchChain, and JsToolChainWrapper use the same hooks and recovery contract:
//
//	// Native registry
//	tc := toolchain.New().
//	    RegisterTool(searchTool).
//	    RegisterTool(emailTool)
//
// See [Tool], [ToolChain], and the toolchain package for details.
//
// # Termination
//
// Termination defines when and how the agent produces its final answer. The termination
// checks final assistant content and can validate answers before accepting.
//
//	// Simple text termination
//	term := termination.NewText("answer").
//	    WithGuidance("Write your final answer here.")
//
//	// JSON termination with typed output
//	term := termination.NewJSON[ResponseSchema]("answer").
//	    WithGuidance("Respond with the required JSON format.")
//
// Termination supports validators that can reject answers and provide feedback:
//
//	term.SetValidator(&MyValidator{})
//
// When rejected, the feedback is shown to the LLM and it continues iterating.
// Stats are tracked: [SCAnswerRejectedTotal], [SCAnswerRejectedBy].
//
// See [Termination], [AnswerValidator], and the termination package for details.
//
// # TextFormat
//
// TextFormat renders prompt and result sections. Independent parsing helpers remain available
// for application documents; the native agent does not parse tool calls with TextFormat.
//
//	// XML format (default)
//	format := format.NewXML()
//
//	// Parse LLM output into sections
//	sections, err := format.Parse(execCtx, llmOutput)
//	// sections["thinking"] = ["Let me analyze..."]
//	// sections["report"] = ["Five records were found."]
//
//	// Format sections for prompts
//	text := format.FormatSections([]gent.FormattedSection{
//	    {Name: "observation", Content: "Search returned 5 results"},
//	})
//
// See [TextFormat] and the format package for implementations.
//
// # TextSection
//
// TextSection defines individual text sections. Termination and section parsers implement
// this contract. ToolChain is independent and accepts only native calls.
//
//	// Sections have a name and guidance
//	type TextSection interface {
//	    Name() string     // e.g., "thinking", "action", "answer"
//	    Guidance() string // Instructions shown to the LLM
//	    ParseSection(execCtx *ExecutionContext, content string) (any, error)
//	}
//
// The section package provides ready-to-use implementations:
//   - section.Text: Simple passthrough
//   - section.JSON[T]: Parse JSON into typed struct
//   - section.YAML[T]: Parse YAML into typed struct
//
// See [TextSection] and the section package for details.
//
// # Events & Subscribers
//
// Subscribers allow observing and intercepting execution at various points. Implement the
// appropriate subscriber interface and register with the executor:
//
//	type LoggingSubscriber struct{}
//
//	func (s *LoggingSubscriber) OnBeforeIteration(
//	    execCtx *ExecutionContext,
//	    e *BeforeIterationEvent,
//	) {
//	    log.Printf("Starting iteration %d", e.Iteration)
//	}
//
//	func (s *LoggingSubscriber) OnAfterModelCall(
//	    execCtx *ExecutionContext,
//	    e *AfterModelCallEvent,
//	) {
//	    log.Printf(
//	        "Model call took %v, used %d tokens",
//	        e.Duration,
//	        e.Response.Info.InputTokens,
//	    )
//	}
//
//	// Register subscribers
//	registry := events.NewRegistry()
//	registry.Subscribe(&LoggingSubscriber{})
//	exec := executor.New(agent, executor.Config{Events: registry})
//
// Available subscriber interfaces:
//   - [BeforeExecutionSubscriber], [AfterExecutionSubscriber]: Execution lifecycle
//   - [BeforeIterationSubscriber], [AfterIterationSubscriber]: Iteration lifecycle
//   - [BeforeModelCallSubscriber], [AfterModelCallSubscriber]: Model API calls
//   - [BeforeToolCallSubscriber], [AfterToolCallSubscriber]: Tool executions
//   - [ErrorSubscriber]: Error handling
//
// See subscribers.go for subscriber interfaces and events.go for event types.
//
// # Events
//
// Events are automatically recorded during execution for debugging and analysis.
// Access them via ExecutionContext:
//
//	events := execCtx.Events()
//	for _, event := range events {
//	    switch e := event.(type) {
//	    case *gent.AfterModelCallEvent:
//	        fmt.Printf("Model %s: %d input, %d output tokens\n",
//	            e.Model, e.InputTokens, e.OutputTokens)
//	    case *gent.AfterToolCallEvent:
//	        fmt.Printf("Tool %s called with %v\n", e.ToolName, e.Args)
//	    case *gent.ParseErrorEvent:
//	        fmt.Printf("Parse error (%s): %v\n", e.ErrorType, e.Error)
//	    }
//	}
//
// Event types include:
//   - [AfterModelCallEvent]: LLM API calls with token counts
//   - [AfterToolCallEvent]: Tool executions with inputs/outputs
//   - [ParseErrorEvent]: Format/toolchain/termination parse errors
//   - [BeforeIterationEvent], [AfterIterationEvent]: Iteration boundaries
//   - [CommonEvent]: User-defined events
//
// # Stats and Limits
//
// ExecutionStats tracks counters and gauges during execution. Stats propagate
// hierarchically from child to parent contexts, enabling limits on nested loops.
//
//	stats := execCtx.Stats()
//	fmt.Printf("Iterations: %d\n", stats.GetIterations())
//	fmt.Printf("Total tokens: %d\n", stats.GetTotalInputTokens() + stats.GetTotalOutputTokens())
//	fmt.Printf("Tool calls: %d\n", stats.GetToolCallCount())
//
// Limits automatically terminate execution when thresholds are exceeded:
//
//	limits := []gent.Limit{
//	    // Stop after 50 iterations
//	    {Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 50},
//	    // Stop after 100k total input tokens (across all models)
//	    {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 100000},
//	    // Stop after 10k tokens for specific model
//	    {Type: gent.LimitExactKey, Key: gent.SCInputTokensFor + "gpt-4", MaxValue: 10000},
//	    // Stop any tool from being called more than 20 times
//	    {Type: gent.LimitKeyPrefix, Key: gent.SCToolCallsFor, MaxValue: 20},
//	}
//	execCtx.SetLimits(limits)
//
// Default limits are applied automatically (see [DefaultLimits]).
// Check which limit was exceeded:
//
//	if execCtx.TerminationReason() == gent.TerminationLimitExceeded {
//	    limit := execCtx.ExceededLimit()
//	    fmt.Printf("Exceeded %s (max: %.0f)\n", limit.Key, limit.MaxValue)
//	}
//
// See [ExecutionStats], [Limit], and stats_keys.go for available keys.
//
// # ExecutionContext
//
// ExecutionContext is the central context passed through all framework components.
// It provides tracing, stats, limits, and cancellation support.
//
//	// Access in your components
//	func (t *MyTool) Call(ctx context.Context, input Input) (*gent.ToolResult[Output], error) {
//	    // Use the standard context for external calls
//	    result, err := externalAPI.Call(ctx, input)
//	    return &gent.ToolResult[Output]{Text: result}, err
//	}
//
// Key methods:
//   - Context(): Get the underlying context.Context for cancellation
//   - Data(): Access your LoopData
//   - Stats(): Access execution statistics
//   - Trace(): Record custom trace events
//   - Iteration(): Current iteration number
//
// See [ExecutionContext] for full documentation.
//
// # TimeProvider
//
// TimeProvider allows injecting time into prompts and enables deterministic testing.
// The agent can reference time in its behavior context:
//
//	agent := react.NewAgent(model).
//	    WithBehaviorAndContext("Today is {{.Time.Today}} ({{.Time.Weekday}})")
//
// For testing, use MockTimeProvider to control time:
//
//	// In tests
//	mockTime := gent.NewMockTimeProvider(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC))
//	agent.WithTimeProvider(mockTime)
//
//	// Now the agent will always see "2025-06-15" and "Sunday"
//
// For a custom time source with standard prompt formats, supply one function:
//
//	agent.WithClock(func() time.Time { return time.Now().In(userLocation) })
//
// Use WithTimeProvider when the formats must also change.
// See [TimeProvider], [TimeProviderFunc], [DefaultTimeProvider], and [MockTimeProvider].
//
// # Writing Your Own Loop
//
// To create a custom agent loop, implement [Loop] and [LoopData]:
//
//	type MyLoopData struct {
//	    *gent.BasicLoopData
//	    // Add application fields and implement CheckpointState to save mutable state.
//	}
//
// Next builds a request from ExecutionContext.Task and Scratchpad, calls the model through
// ExecutionContext.GenerateContentStream, consumes its chunks, and awaits Response. It then
// records observations with AppendRecord or UpdateHistory and returns LAContinue or LATerminate
// with the final Result. Each operation returns errors that the loop must handle.
//
// The executor calls Next() repeatedly until LATerminate is returned or a limit is exceeded.
// Custom continuation requires an executor RecoveryVersion and explicit CheckpointState; see
// docs/history.md. The agents/react package provides a complete reference implementation, and
// examples/history shows interruption, restoration, and tool reconciliation with public APIs.
package gent

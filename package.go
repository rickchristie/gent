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
//	    model := models.NewLangChainGoModel(llm, "gpt-4")
//
//	    // 2. Create tools
//	    lookupOrder := gent.NewToolFunc(
//	        "lookup_order",
//	        "Look up order details by order ID",
//	        map[string]any{
//	            "type": "object",
//	            "properties": map[string]any{
//	                "order_id": map[string]any{"type": "string"},
//	            },
//	            "required": []string{"order_id"},
//	        },
//	        func(ctx context.Context, input OrderLookupInput) (OrderDetails, error) {
//	            return lookupOrderFromDB(input.OrderID)
//	        },
//	    )
//
//	    // 3. Create toolchain and register tools
//	    tc := toolchain.NewYAML().RegisterTool(lookupOrder)
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
//	        {Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 20},
//	        {Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 50000},
//	    }
//
//	    // 7. Create execution context with task
//	    task := &gent.Task{Text: "What's the status of order #12345?"}
//	    data := react.NewLoopData(task)
//	    execCtx := gent.NewExecutionContext(context.Background(), "customer-service", data)
//	    execCtx.SetLimits(limits)
//
//	    // 8. Run the agent
//	    exec := executor.New[*react.LoopData](agent, executor.DefaultConfig())
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
//	tool := gent.NewToolFunc(
//	    "search",
//	    "Search for information",
//	    schema, // JSON Schema for parameters
//	    func(ctx context.Context, input SearchInput) (SearchResult, error) {
//	        return doSearch(input.Query)
//	    },
//	)
//
// ToolChain manages tool registration, parsing tool calls from LLM output, executing tools,
// and formatting results. Different implementations (YAML, JSON) parse different formats:
//
//	// YAML toolchain (recommended for readability)
//	tc := toolchain.NewYAML().
//	    RegisterTool(searchTool).
//	    RegisterTool(emailTool)
//
// See [Tool], [ToolChain], and the toolchain package for details.
//
// # Termination
//
// Termination defines when and how the agent produces its final answer. The termination
// checks each iteration for an answer section and can validate answers before accepting.
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
// Stats are tracked: [KeyAnswerRejectedTotal], [KeyAnswerRejectedBy].
//
// See [Termination], [AnswerValidator], and the termination package for details.
//
// # TextFormat
//
// TextFormat defines how sections are structured in LLM output. It handles parsing
// LLM responses into sections and formatting sections for prompts.
//
//	// XML format (default)
//	format := format.NewXML()
//
//	// Parse LLM output into sections
//	sections, err := format.Parse(execCtx, llmOutput)
//	// sections["thinking"] = ["Let me analyze..."]
//	// sections["action"] = ["tool: search\nquery: foo"]
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
// TextSection defines individual sections within LLM output. Both ToolChain and
// Termination implement TextSection, allowing them to be registered with TextFormat.
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
// # Hooks & Events
//
// Hooks allow observing and intercepting execution at various points. Implement the
// appropriate hook interface and register with the executor:
//
//	type LoggingHook struct{}
//
//	func (h *LoggingHook) OnBeforeIteration(ctx context.Context, execCtx *ExecutionContext, e BeforeIterationEvent) {
//	    log.Printf("Starting iteration %d", e.Iteration)
//	}
//
//	func (h *LoggingHook) OnAfterModelCall(ctx context.Context, execCtx *ExecutionContext, e AfterModelCallEvent) {
//	    log.Printf("Model call took %v, used %d tokens", e.Duration, e.Response.Info.InputTokens)
//	}
//
//	// Register hooks
//	registry := hooks.NewRegistry()
//	registry.Register(&LoggingHook{})
//	exec := executor.New(agent, executor.Config{Hooks: registry})
//
// Available hook interfaces:
//   - [BeforeExecutionHook], [AfterExecutionHook]: Execution lifecycle
//   - [BeforeIterationHook], [AfterIterationHook]: Iteration lifecycle
//   - [BeforeModelCallHook], [AfterModelCallHook]: Model API calls
//   - [BeforeToolCallHook], [AfterToolCallHook]: Tool executions
//   - [ErrorHook]: Error handling
//
// See hooks.go for hook interfaces and events.go for event types.
//
// # Traces
//
// Traces are automatically recorded during execution for debugging and analysis.
// Access them via ExecutionContext:
//
//	events := execCtx.Events()
//	for _, event := range events {
//	    switch e := event.(type) {
//	    case gent.ModelCallTrace:
//	        fmt.Printf("Model %s: %d input, %d output tokens\n",
//	            e.Model, e.InputTokens, e.OutputTokens)
//	    case gent.ToolCallTrace:
//	        fmt.Printf("Tool %s called with %v\n", e.ToolName, e.Input)
//	    case gent.ParseErrorTrace:
//	        fmt.Printf("Parse error (%s): %v\n", e.ErrorType, e.Error)
//	    }
//	}
//
// Trace types include:
//   - [ModelCallTrace]: LLM API calls with token counts
//   - [ToolCallTrace]: Tool executions with inputs/outputs
//   - [ParseErrorTrace]: Format/toolchain/termination parse errors
//   - [IterationStartTrace], [IterationEndTrace]: Iteration boundaries
//   - [ChildSpawnTrace], [ChildCompleteTrace]: Nested context lifecycle
//   - [CustomTrace]: Custom trace data for your implementations
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
//	    {Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 50},
//	    // Stop after 100k total input tokens (across all models)
//	    {Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 100000},
//	    // Stop after 10k tokens for specific model
//	    {Type: gent.LimitExactKey, Key: gent.KeyInputTokensFor + "gpt-4", MaxValue: 10000},
//	    // Stop any tool from being called more than 20 times
//	    {Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsFor, MaxValue: 20},
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
// See [TimeProvider], [DefaultTimeProvider], and [MockTimeProvider].
//
// # Writing Your Own Loop
//
// To create a custom agent loop, implement [AgentLoop] and [LoopData]:
//
//	type MyLoopData struct {
//	    task       *gent.Task
//	    iterations []*gent.Iteration
//	    scratchpad []*gent.Iteration
//	}
//
//	func (d *MyLoopData) GetTask() *gent.Task { return d.task }
//	// ... implement other LoopData methods
//
//	type MyLoop struct {
//	    model gent.Model
//	    // your configuration
//	}
//
//	func (l *MyLoop) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
//	    data := execCtx.Data().(*MyLoopData)
//
//	    // 1. Build your prompt
//	    messages := l.buildMessages(data)
//
//	    // 2. Call the model
//	    response, err := l.model.GenerateContent(execCtx, "model-name", systemPrompt, messages)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    // 3. Process output and decide: continue or terminate?
//	    if shouldTerminate(response) {
//	        return &gent.AgentLoopResult{
//	            Action: gent.LATerminate,
//	            Result: []gent.ContentPart{llms.TextContent{Text: extractAnswer(response)}},
//	        }, nil
//	    }
//
//	    // 4. Continue with observation
//	    return &gent.AgentLoopResult{
//	        Action:     gent.LAContinue,
//	        NextPrompt: processAndGetObservation(response),
//	    }, nil
//	}
//
// The executor calls Next() repeatedly until LATerminate is returned or a limit is exceeded.
// See the agents/react package for a complete reference implementation.
package gent

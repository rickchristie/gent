package airline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/hooks"
	"github.com/rickchristie/gent/integrationtest/loggers"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// AirlineTestConfig configures how airline test output is displayed.
type AirlineTestConfig struct {
	// UseStreaming enables streaming mode for LLM calls.
	UseStreaming bool
	// ShowIterationHistory prints full iteration history at the end.
	ShowIterationHistory bool
	// ShowTraceEvents prints all trace events at the end.
	ShowTraceEvents bool
}

// TestConfig returns a config suitable for go test.
func TestConfig() AirlineTestConfig {
	return AirlineTestConfig{
		UseStreaming:         false,
		ShowIterationHistory: true,
		ShowTraceEvents:      true,
	}
}

// InteractiveConfig returns a config for interactive CLI with streaming.
func InteractiveConfig() AirlineTestConfig {
	return AirlineTestConfig{
		UseStreaming:         true,
		ShowIterationHistory: false,
		ShowTraceEvents:      false,
	}
}

// AirlineTestCase represents an airline test that can be run.
type AirlineTestCase struct {
	Name        string
	Description string
	Run         func(ctx context.Context, w io.Writer, config AirlineTestConfig) error
}

// GetAirlineTestCases returns all available airline test cases.
func GetAirlineTestCases() []AirlineTestCase {
	return []AirlineTestCase{
		{
			Name:        "Reschedule",
			Description: "Customer requests flight reschedule to a later time",
			Run:         RunRescheduleScenario,
		},
	}
}

// createModel creates an xAI model for testing.
func createModel() (gent.StreamingModel, error) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GENT_TEST_XAI_KEY environment variable not set")
	}

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create xAI LLM: %w", err)
	}

	return models.NewLCGWrapper(llm).WithModelName("grok-4-1-fast"), nil
}

// RunRescheduleScenario runs the flight reschedule scenario.
func RunRescheduleScenario(ctx context.Context, w io.Writer, config AirlineTestConfig) error {
	model, err := createModel()
	if err != nil {
		return err
	}

	// Create the airline fixture with dynamic dates
	fixture := NewAirlineFixture(nil)

	// Create toolchain and register all airline tools
	tc := toolchain.NewYAML()
	fixture.RegisterAllTools(tc)

	// Create the ReAct loop
	loop := agents.NewReactLoop(model).
		WithToolChain(tc).
		WithTimeProvider(fixture.TimeProvider()).
		WithStreaming(config.UseStreaming).
		WithSystemPrompt(`## Task Description

You are a helpful airline customer service agent for SkyWings Airlines.
Your role is to assist customers with their flight bookings, including checking flight information,
rescheduling flights, and answering policy questions.

Today is {{.Time.Today}} ({{.Time.Weekday}}).

Always be polite and professional. When rescheduling, make sure to:
1. Verify the customer's identity and booking
2. Check the airline's change policy
3. Search for available alternative flights
4. Inform the customer of any fees before making changes
5. Confirm the change and provide updated booking details`).
		WithThinking("Think step by step about how to help the customer.")

	// Customer request
	customerRequest := `Hi, I'm John Smith and my email is john.smith@email.com.
I have a flight booked for tomorrow (flight AA100 from JFK to LAX) but my meeting is running late.
Can you help me reschedule to a later flight on the same day? I'd prefer an evening flight if possible.`

	data := agents.NewReactLoopData(llms.TextContent{Text: customerRequest})

	// Create ExecutionContext
	execCtx := gent.NewExecutionContext("airline-reschedule", data)

	// Create hook registry
	hookRegistry := hooks.NewRegistry()

	// Set up hooks based on mode
	var streamWg sync.WaitGroup
	if config.UseStreaming {
		// CLI mode: use streaming output hook
		streamingHook := newStreamingOutputHook(w)
		hookRegistry.Register(streamingHook)

		// Subscribe to LLM response stream
		chunks, unsubscribe := execCtx.SubscribeToTopic("llm-response")

		streamWg.Add(1)
		go func() {
			defer streamWg.Done()
			streamConsumer(chunks, w, streamingHook)
		}()

		defer func() {
			unsubscribe()
			streamWg.Wait()
		}()
	} else {
		// Test mode: use logger hook for full debugging output
		loggerHook := loggers.NewLoggerHookWithWriter(w)
		hookRegistry.Register(loggerHook)
	}

	// Create executor
	exec := executor.New[*agents.ReactLoopData](loop, executor.Config{MaxIterations: 15}).
		WithHooks(hookRegistry)

	// Print header
	printHeader(w, "AIRLINE RESCHEDULE SCENARIO")
	fmt.Fprintln(w)

	// Print customer request
	printSection(w, "Customer Request")
	fmt.Fprintln(w, customerRequest)
	fmt.Fprintln(w)

	// Execute
	printSection(w, "Agent Execution")
	fmt.Fprintln(w)

	result := exec.ExecuteWithContext(ctx, execCtx)

	// Print final summary
	fmt.Fprintln(w)
	printHeader(w, "EXECUTION COMPLETE")
	fmt.Fprintln(w)

	// Print final result
	if result.Error != nil {
		fmt.Fprintf(w, "Error: %v\n", result.Error)
	} else {
		printSection(w, "Final Response to Customer")
		for _, part := range result.Result {
			if tc, ok := part.(llms.TextContent); ok {
				fmt.Fprintln(w, tc.Text)
			}
		}
	}

	// Print stats
	fmt.Fprintln(w)
	printSection(w, "Execution Stats")
	stats := execCtx.Stats()
	fmt.Fprintf(w, "Total iterations: %d\n", execCtx.Iteration())
	fmt.Fprintf(w, "Total input tokens: %d\n", stats.TotalInputTokens)
	fmt.Fprintf(w, "Total output tokens: %d\n", stats.TotalOutputTokens)
	fmt.Fprintf(w, "Total tool calls: %d\n", stats.ToolCallCount)
	fmt.Fprintf(w, "Duration: %v\n", execCtx.Duration())

	// Print iteration history if configured
	if config.ShowIterationHistory {
		fmt.Fprintln(w)
		printHeader(w, "FULL ITERATION HISTORY")

		for i, iter := range data.GetIterationHistory() {
			fmt.Fprintf(w, "\n--- Iteration %d ---\n", i+1)
			for _, msg := range iter.Messages {
				fmt.Fprintf(w, "[%s]\n", msg.Role)
				for _, part := range msg.Parts {
					if tc, ok := part.(llms.TextContent); ok {
						text := tc.Text
						if len(text) > 3000 {
							text = text[:3000] + "\n... (truncated)"
						}
						fmt.Fprintln(w, text)
					}
				}
				fmt.Fprintln(w)
			}
		}
	}

	// Print trace events if configured
	if config.ShowTraceEvents {
		fmt.Fprintln(w)
		printHeader(w, "ALL TRACE EVENTS")

		for i, event := range execCtx.Events() {
			fmt.Fprintf(w, "\n[%d] ", i+1)
			switch e := event.(type) {
			case gent.IterationStartTrace:
				fmt.Fprintf(w, "IterationStart: iteration=%d\n", e.Iteration)
			case gent.IterationEndTrace:
				fmt.Fprintf(w, "IterationEnd: iteration=%d, action=%s, duration=%s\n",
					e.Iteration, e.Action, e.Duration)
			case gent.ModelCallTrace:
				fmt.Fprintf(w, "ModelCall: model=%s, input=%d, output=%d, duration=%s\n",
					e.Model, e.InputTokens, e.OutputTokens, e.Duration)
			case gent.ToolCallTrace:
				outputJSON, _ := json.Marshal(e.Output)
				outputStr := string(outputJSON)
				if len(outputStr) > 200 {
					outputStr = outputStr[:200] + "..."
				}
				fmt.Fprintf(w, "ToolCall: tool=%s, duration=%s\n", e.ToolName, e.Duration)
				fmt.Fprintf(w, "          input=%v\n", e.Input)
				fmt.Fprintf(w, "          output=%s\n", outputStr)
				if e.Error != nil {
					fmt.Fprintf(w, "          error=%v\n", e.Error)
				}
			default:
				fmt.Fprintf(w, "Unknown event type: %T\n", event)
			}
		}
	}

	fmt.Fprintln(w)
	printHeader(w, "TEST COMPLETE")

	return nil
}

// streamingOutputHook handles iteration and tool call output for streaming mode.
type streamingOutputHook struct {
	mu              sync.Mutex
	w               io.Writer
	currentIter     int
	iterHeaderShown bool
}

func newStreamingOutputHook(w io.Writer) *streamingOutputHook {
	return &streamingOutputHook{w: w}
}

// OnBeforeIteration is called before each iteration.
func (h *streamingOutputHook) OnBeforeIteration(
	_ context.Context,
	_ *gent.ExecutionContext,
	event gent.BeforeIterationEvent,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.currentIter = event.Iteration
	h.iterHeaderShown = false
	return nil
}

// OnAfterToolCall is called after each tool execution.
func (h *streamingOutputHook) OnAfterToolCall(
	_ context.Context,
	_ *gent.ExecutionContext,
	event gent.AfterToolCallEvent,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Print tool call result with line break before
	fmt.Fprintf(h.w, "\n\n  [Tool: %s]\n", event.ToolName)

	if event.Args != nil {
		inputJSON, _ := json.MarshalIndent(event.Args, "    ", "  ")
		fmt.Fprintf(h.w, "    Args: %s\n", string(inputJSON))
	}

	if event.Error != nil {
		fmt.Fprintf(h.w, "    Error: %v\n", event.Error)
	} else if event.Output != nil {
		outputJSON, _ := json.MarshalIndent(event.Output, "    ", "  ")
		fmt.Fprintf(h.w, "    Output: %s\n", string(outputJSON))
	}
	fmt.Fprintf(h.w, "    Duration: %v\n", event.Duration)
}

func (h *streamingOutputHook) getCurrentIter() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentIter
}

func (h *streamingOutputHook) markIterHeaderShown() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wasShown := h.iterHeaderShown
	h.iterHeaderShown = true
	return wasShown
}

// streamConsumer processes streaming chunks and displays them.
func streamConsumer(chunks <-chan gent.StreamChunk, w io.Writer, hook *streamingOutputHook) {
	var lastIter int
	var hasContent bool

	for chunk := range chunks {
		currentIter := hook.getCurrentIter()
		if currentIter != lastIter && currentIter > 0 {
			if hasContent {
				fmt.Fprintln(w)
			}
			if !hook.markIterHeaderShown() {
				// Line break before new iteration
				fmt.Fprintf(w, "\n--- Iteration %d ---\n", currentIter)
				fmt.Fprint(w, "  LLM: ")
			}
			lastIter = currentIter
			hasContent = false
		}

		if chunk.Content != "" {
			fmt.Fprint(w, chunk.Content)
			hasContent = true
		}

		if chunk.ReasoningContent != "" {
			fmt.Fprint(w, chunk.ReasoningContent)
			hasContent = true
		}

		if chunk.Err != nil {
			if hasContent {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "  [Stream Error: %v]\n", chunk.Err)
			hasContent = false
		}
	}

	if hasContent {
		fmt.Fprintln(w)
	}
}

func printHeader(w io.Writer, title string) {
	line := strings.Repeat("=", 80)
	fmt.Fprintln(w, line)
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, line)
}

func printSection(w io.Writer, title string) {
	fmt.Fprintf(w, "--- %s ---\n", title)
}

// Package testutil provides shared test infrastructure for integration
// test scenarios.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/integrationtest/loggers"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// ToolChainType specifies which toolchain format to use.
type ToolChainType string

const (
	ToolChainYAML ToolChainType = "yaml"
	ToolChainJSON ToolChainType = "json"
)

// CompactionType specifies the scratchpad context management strategy.
type CompactionType string

const (
	CompactionNone          CompactionType = "none"
	CompactionSlidingWindow CompactionType = "sliding_window"
	CompactionSummarization CompactionType = "summarization"
)

// CompactionConfig configures scratchpad context management.
type CompactionConfig struct {
	// Type selects the compaction strategy.
	Type CompactionType
	// TriggerIterations: compact every N iterations.
	TriggerIterations int64
	// WindowSize: for sliding window, how many recent iterations to keep.
	WindowSize int
	// KeepRecent: for summarization, how many recent iterations
	// to preserve.
	KeepRecent int
}

// TestConfig configures how integration test output is displayed.
type TestConfig struct {
	// ToolChain specifies which toolchain format to use.
	ToolChain ToolChainType
	// UseStreaming enables streaming mode for LLM calls.
	UseStreaming bool
	// ShowIterationHistory prints full iteration history at the end.
	ShowIterationHistory bool
	// ShowEvents prints all events at the end.
	ShowEvents bool
	// LogWriter is an optional writer for full debug logging.
	LogWriter io.Writer
	// Compaction configures scratchpad context management.
	Compaction CompactionConfig
}

// DefaultTestConfig returns a config suitable for go test with YAML
// toolchain.
func DefaultTestConfig() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainYAML,
		UseStreaming:         false,
		ShowIterationHistory: true,
		ShowEvents:           true,
	}
}

// DefaultTestConfigJSON returns a config suitable for go test with
// JSON toolchain.
func DefaultTestConfigJSON() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainJSON,
		UseStreaming:         false,
		ShowIterationHistory: true,
		ShowEvents:           true,
	}
}

// InteractiveConfig returns a config for interactive CLI with streaming
// and YAML toolchain.
func InteractiveConfig() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainYAML,
		UseStreaming:         true,
		ShowIterationHistory: false,
		ShowEvents:           false,
	}
}

// InteractiveConfigJSON returns a config for interactive CLI with
// streaming and JSON toolchain.
func InteractiveConfigJSON() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainJSON,
		UseStreaming:         true,
		ShowIterationHistory: false,
		ShowEvents:           false,
	}
}

// TestCase represents a test that can be run.
type TestCase struct {
	Name        string
	Description string
	Run         func(
		ctx context.Context,
		w io.Writer,
		config TestConfig,
	) error
}

// ConversationMessage represents a message in conversation history.
type ConversationMessage struct {
	Role    string // "user" or "agent"
	Content string
}

// CreateModel creates an xAI model for testing.
func CreateModel() (gent.StreamingModel, error) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf(
			"GENT_TEST_XAI_KEY environment variable not set",
		)
	}

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast"),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create xAI LLM: %w", err,
		)
	}

	return models.NewLCGWrapper(llm).
		WithModelName("grok-4-1-fast"), nil
}

// CreateToolChain creates the appropriate toolchain based on config.
func CreateToolChain(config TestConfig) gent.ToolChain {
	switch config.ToolChain {
	case ToolChainJSON:
		return toolchain.NewJSON()
	default:
		return toolchain.NewYAML()
	}
}

// ConfigureCompaction sets up compaction on the execution context
// based on the config. The model is needed for summarization strategy.
func ConfigureCompaction(
	execCtx *gent.ExecutionContext,
	config CompactionConfig,
	model gent.Model,
) {
	if config.Type == CompactionNone || config.Type == "" {
		return
	}

	trigger := compaction.NewStatThresholdTrigger().
		OnCounter(
			gent.SCIterations,
			config.TriggerIterations,
		)

	var strategy gent.CompactionStrategy
	switch config.Type {
	case CompactionSlidingWindow:
		strategy = compaction.NewSlidingWindow(config.WindowSize)
	case CompactionSummarization:
		strategy = compaction.NewSummarization(model).
			WithKeepRecent(config.KeepRecent)
	default:
		return
	}

	execCtx.SetCompaction(trigger, strategy)
}

// PrintHeader prints a header line.
func PrintHeader(w io.Writer, title string) {
	line := strings.Repeat("=", 80)
	fmt.Fprintln(w, line)
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, line)
}

// PrintSection prints a section header.
func PrintSection(w io.Writer, title string) {
	fmt.Fprintf(w, "--- %s ---\n", title)
}

// ContainsIgnoreCase checks if s contains substr, case-insensitive.
func ContainsIgnoreCase(s, substr string) bool {
	sLower := make([]byte, len(s))
	substrLower := make([]byte, len(substr))
	for i := range len(s) {
		if s[i] >= 'A' && s[i] <= 'Z' {
			sLower[i] = s[i] + 32
		} else {
			sLower[i] = s[i]
		}
	}
	for i := range len(substr) {
		if substr[i] >= 'A' && substr[i] <= 'Z' {
			substrLower[i] = substr[i] + 32
		} else {
			substrLower[i] = substr[i]
		}
	}

	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		match := true
		for j := range len(substrLower) {
			if sLower[i+j] != substrLower[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------
// ScenarioConfig + RunScenario
// -------------------------------------------------------------------------

// ScenarioConfig defines parameters for a test scenario.
type ScenarioConfig struct {
	Name            string
	HeaderTitle     string
	CustomerRequest string
	MaxIterations   float64
	TimeProvider    gent.TimeProvider
	RegisterTools   func(tc gent.ToolChain)
	SystemPrompt    string
	CriticalRules   string
	ThinkingPrompt  string
}

// RunScenario executes a test scenario with the given configuration.
func RunScenario(
	ctx context.Context,
	w io.Writer,
	testCfg TestConfig,
	scenario ScenarioConfig,
) error {
	model, err := CreateModel()
	if err != nil {
		return err
	}

	tc := CreateToolChain(testCfg)
	scenario.RegisterTools(tc)

	loop := react.NewAgent(model).
		WithToolChain(tc).
		WithTimeProvider(scenario.TimeProvider).
		WithStreaming(testCfg.UseStreaming).
		WithBehaviorAndContext(scenario.SystemPrompt).
		WithCriticalRules(scenario.CriticalRules).
		WithThinking(scenario.ThinkingPrompt)

	data := gent.NewBasicLoopData(
		&gent.Task{Text: scenario.CustomerRequest},
	)

	execCtx := gent.NewExecutionContext(
		ctx, scenario.Name, data,
	)
	execCtx.SetLimits([]gent.Limit{
		{
			Type:     gent.LimitExactKey,
			Key:      gent.SCIterations,
			MaxValue: scenario.MaxIterations,
		},
	})

	ConfigureCompaction(
		execCtx, testCfg.Compaction, model,
	)

	registry := events.NewRegistry()

	var streamWg sync.WaitGroup
	if testCfg.UseStreaming {
		streamingHook := NewStreamingOutputHook(w)
		registry.Subscribe(streamingHook)

		chunks, unsubscribe := execCtx.SubscribeToTopic(
			"llm-response",
		)

		streamWg.Add(1)
		go func() {
			defer streamWg.Done()
			StreamConsumer(chunks, w, streamingHook)
		}()

		defer func() {
			unsubscribe()
			streamWg.Wait()
		}()
	} else {
		loggerSubscriber := loggers.NewSubscriberWithWriter(w)
		registry.Subscribe(loggerSubscriber)
	}

	if testCfg.LogWriter != nil {
		fileLogger := loggers.NewSubscriberWithWriter(
			testCfg.LogWriter,
		)
		registry.Subscribe(fileLogger)
	}

	exec := executor.New[*gent.BasicLoopData](
		loop, executor.Config{},
	).WithEvents(registry)

	PrintHeader(w, scenario.HeaderTitle)
	fmt.Fprintln(w)

	if testCfg.Compaction.Type != CompactionNone &&
		testCfg.Compaction.Type != "" {
		PrintSection(w, "Compaction Config")
		fmt.Fprintf(w, "Strategy: %s\n",
			testCfg.Compaction.Type)
		fmt.Fprintf(w, "Trigger: every %d iterations\n",
			testCfg.Compaction.TriggerIterations)
		switch testCfg.Compaction.Type {
		case CompactionSlidingWindow:
			fmt.Fprintf(w, "Window size: %d\n",
				testCfg.Compaction.WindowSize)
		case CompactionSummarization:
			fmt.Fprintf(w, "Keep recent: %d\n",
				testCfg.Compaction.KeepRecent)
		}
		fmt.Fprintln(w)
	}

	PrintSection(w, "Customer Request")
	fmt.Fprintln(w, scenario.CustomerRequest)
	fmt.Fprintln(w)

	PrintSection(w, "Agent Execution")
	fmt.Fprintln(w)

	exec.Execute(execCtx)
	result := execCtx.Result()

	fmt.Fprintln(w)
	PrintHeader(w, "EXECUTION COMPLETE")
	fmt.Fprintln(w)

	if result.Error != nil {
		fmt.Fprintf(w, "Error: %v\n", result.Error)
	} else {
		PrintSection(w, "Final Response to Customer")
		for _, part := range result.Output {
			if tc, ok := part.(llms.TextContent); ok {
				fmt.Fprintln(w, tc.Text)
			}
		}
	}

	fmt.Fprintln(w)
	PrintSection(w, "Execution Stats")
	stats := execCtx.Stats()
	fmt.Fprintf(w, "Total iterations: %d\n",
		execCtx.Iteration())
	fmt.Fprintf(w, "Total input tokens: %d\n",
		stats.GetTotalInputTokens())
	fmt.Fprintf(w, "Total output tokens: %d\n",
		stats.GetTotalOutputTokens())
	fmt.Fprintf(w, "Total tool calls: %d\n",
		stats.GetToolCallCount())
	fmt.Fprintf(w, "Total compactions: %d\n",
		stats.GetCounter(gent.SCCompactions))
	fmt.Fprintf(w, "Duration: %v\n", execCtx.Duration())

	if testCfg.ShowIterationHistory {
		fmt.Fprintln(w)
		PrintHeader(w, "FULL ITERATION HISTORY")

		for i, iter := range data.GetIterationHistory() {
			fmt.Fprintf(w, "\n--- Iteration %d ---\n", i+1)
			for _, msg := range iter.Messages {
				fmt.Fprintf(w, "[%s]\n", msg.Role)
				for _, part := range msg.Parts {
					if tc, ok := part.(llms.TextContent); ok {
						text := tc.Text
						if len(text) > 3000 {
							text = text[:3000] +
								"\n... (truncated)"
						}
						fmt.Fprintln(w, text)
					}
				}
				fmt.Fprintln(w)
			}
		}
	}

	if testCfg.ShowEvents {
		fmt.Fprintln(w)
		PrintHeader(w, "ALL EVENTS")
		printEvents(w, execCtx)
	}

	fmt.Fprintln(w)
	PrintHeader(w, "TEST COMPLETE")

	return result.Error
}

// printEvents prints all events from the execution context.
func printEvents(w io.Writer, execCtx *gent.ExecutionContext) {
	for i, event := range execCtx.Events() {
		fmt.Fprintf(w, "\n[%d] ", i+1)
		switch e := event.(type) {
		case *gent.BeforeIterationEvent:
			fmt.Fprintf(w,
				"BeforeIteration: iteration=%d\n",
				e.Iteration)
		case *gent.AfterIterationEvent:
			fmt.Fprintf(w,
				"AfterIteration: iteration=%d, "+
					"duration=%s\n",
				e.Iteration, e.Duration)
		case *gent.AfterModelCallEvent:
			fmt.Fprintf(w,
				"AfterModelCall: model=%s, input=%d, "+
					"output=%d, duration=%s\n",
				e.Model, e.InputTokens,
				e.OutputTokens, e.Duration)
		case *gent.AfterToolCallEvent:
			outputJSON, _ := json.Marshal(e.Output)
			outputStr := string(outputJSON)
			if len(outputStr) > 200 {
				outputStr = outputStr[:200] + "..."
			}
			fmt.Fprintf(w,
				"AfterToolCall: tool=%s, "+
					"duration=%s\n",
				e.ToolName, e.Duration)
			fmt.Fprintf(w,
				"               args=%v\n", e.Args)
			fmt.Fprintf(w,
				"               output=%s\n",
				outputStr)
			if e.Error != nil {
				fmt.Fprintf(w,
					"               error=%v\n",
					e.Error)
			}
		case *gent.CompactionEvent:
			fmt.Fprintf(w,
				"Compaction: %d -> %d iterations"+
					" (removed %d, duration=%s)\n",
				e.ScratchpadLengthBefore,
				e.ScratchpadLengthAfter,
				e.ScratchpadLengthBefore-
					e.ScratchpadLengthAfter,
				e.Duration)
		case *gent.LimitExceededEvent:
			fmt.Fprintf(w,
				"LimitExceeded: key=%s, "+
					"value=%.0f, max=%.0f\n",
				e.MatchedKey,
				e.CurrentValue,
				e.Limit.MaxValue)
		default:
			fmt.Fprintf(w, "%T\n", event)
		}
	}
}

// -------------------------------------------------------------------------
// Streaming Infrastructure
// -------------------------------------------------------------------------

// StreamingOutputHook handles iteration and tool call output for
// streaming mode.
type StreamingOutputHook struct {
	mu              sync.Mutex
	w               io.Writer
	currentIter     int
	iterHeaderShown bool
}

// NewStreamingOutputHook creates a new streaming output hook.
func NewStreamingOutputHook(w io.Writer) *StreamingOutputHook {
	return &StreamingOutputHook{w: w}
}

// OnBeforeIteration is called before each iteration.
func (h *StreamingOutputHook) OnBeforeIteration(
	_ *gent.ExecutionContext,
	event *gent.BeforeIterationEvent,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.currentIter = event.Iteration
	h.iterHeaderShown = false
}

// OnAfterToolCall is called after each tool execution.
func (h *StreamingOutputHook) OnAfterToolCall(
	_ *gent.ExecutionContext,
	event *gent.AfterToolCallEvent,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	fmt.Fprintf(h.w, "\n\n  [Tool: %s]\n", event.ToolName)

	if event.Args != nil {
		inputJSON, _ := json.MarshalIndent(
			event.Args, "    ", "  ",
		)
		fmt.Fprintf(h.w, "    Args: %s\n",
			string(inputJSON))
	}

	if event.Error != nil {
		fmt.Fprintf(h.w, "    Error: %v\n", event.Error)
	} else if event.Output != nil {
		outputJSON, _ := json.MarshalIndent(
			event.Output, "    ", "  ",
		)
		fmt.Fprintf(h.w, "    Output: %s\n",
			string(outputJSON))
	}
	fmt.Fprintf(h.w, "    Duration: %v\n", event.Duration)
}

// OnCompaction prints compaction events in real-time.
func (h *StreamingOutputHook) OnCompaction(
	_ *gent.ExecutionContext,
	event *gent.CompactionEvent,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	fmt.Fprintf(h.w,
		"\n\n  [Compaction: %d → %d iterations "+
			"(removed %d, took %v)]\n",
		event.ScratchpadLengthBefore,
		event.ScratchpadLengthAfter,
		event.ScratchpadLengthBefore-
			event.ScratchpadLengthAfter,
		event.Duration,
	)
}

// OnLimitExceeded prints limit exceeded events.
func (h *StreamingOutputHook) OnLimitExceeded(
	_ *gent.ExecutionContext,
	event *gent.LimitExceededEvent,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	fmt.Fprintf(h.w,
		"\n\n  [Limit Exceeded: %s = %.0f (max: %.0f)]\n",
		event.MatchedKey,
		event.CurrentValue,
		event.Limit.MaxValue,
	)
}

// GetCurrentIter returns the current iteration number.
func (h *StreamingOutputHook) GetCurrentIter() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentIter
}

// MarkIterHeaderShown marks the iteration header as shown
// and returns whether it was already shown.
func (h *StreamingOutputHook) MarkIterHeaderShown() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wasShown := h.iterHeaderShown
	h.iterHeaderShown = true
	return wasShown
}

// StreamConsumer processes streaming chunks and displays them.
func StreamConsumer(
	chunks <-chan gent.StreamChunk,
	w io.Writer,
	hook *StreamingOutputHook,
) {
	var lastIter int
	var hasContent bool

	for chunk := range chunks {
		currentIter := hook.GetCurrentIter()
		if currentIter != lastIter && currentIter > 0 {
			if hasContent {
				fmt.Fprintln(w)
			}
			if !hook.MarkIterHeaderShown() {
				fmt.Fprintf(w,
					"\n--- Iteration %d ---\n",
					currentIter)
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
			fmt.Fprintf(w,
				"  [Stream Error: %v]\n", chunk.Err)
			hasContent = false
		}
	}

	if hasContent {
		fmt.Fprintln(w)
	}
}

// -------------------------------------------------------------------------
// Interactive Chat
// -------------------------------------------------------------------------

// ChatConfig defines domain-specific parameters for interactive chat.
type ChatConfig struct {
	Name           string
	SystemPrompt   string
	CriticalRules  string
	ThinkingPrompt string
	MaxIterations  float64
	RegisterTools  func(tc gent.ToolChain)
	TimeProvider   gent.TimeProvider
}

// InteractiveChat holds state for an interactive chat session.
type InteractiveChat struct {
	History []ConversationMessage
	Model   gent.StreamingModel
	Config  TestConfig
	Writer  io.Writer
	ChatCfg ChatConfig
}

// NewInteractiveChat creates a new interactive chat session.
func NewInteractiveChat(
	w io.Writer,
	config TestConfig,
	chatCfg ChatConfig,
) (*InteractiveChat, error) {
	model, err := CreateModel()
	if err != nil {
		return nil, err
	}

	return &InteractiveChat{
		History: make([]ConversationMessage, 0),
		Model:   model,
		Config:  config,
		Writer:  w,
		ChatCfg: chatCfg,
	}, nil
}

// formatMessageHistory formats the conversation history for the
// task template.
func (s *InteractiveChat) formatMessageHistory() string {
	if len(s.History) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<message_history>\n")
	for i, msg := range s.History {
		if msg.Role == "user" && i == len(s.History)-1 {
			sb.WriteString("user(most_recent):\n")
		} else {
			sb.WriteString(msg.Role + ":\n")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}
	sb.WriteString("</message_history>\n")
	sb.WriteString("\nAssist and reply to the customer!")
	return sb.String()
}

// SendMessage sends a user message and gets the agent response.
func (s *InteractiveChat) SendMessage(
	ctx context.Context, userMessage string,
) error {
	s.History = append(s.History, ConversationMessage{
		Role:    "user",
		Content: userMessage,
	})

	tc := CreateToolChain(s.Config)
	s.ChatCfg.RegisterTools(tc)

	loop := react.NewAgent(s.Model).
		WithToolChain(tc).
		WithTimeProvider(s.ChatCfg.TimeProvider).
		WithStreaming(s.Config.UseStreaming).
		WithBehaviorAndContext(s.ChatCfg.SystemPrompt).
		WithCriticalRules(s.ChatCfg.CriticalRules).
		WithThinking(s.ChatCfg.ThinkingPrompt)

	taskContent := s.formatMessageHistory()
	data := gent.NewBasicLoopData(
		&gent.Task{Text: taskContent},
	)

	execCtx := gent.NewExecutionContext(
		ctx, s.ChatCfg.Name+"-chat", data,
	)
	execCtx.SetLimits([]gent.Limit{
		{
			Type:     gent.LimitExactKey,
			Key:      gent.SCIterations,
			MaxValue: s.ChatCfg.MaxIterations,
		},
	})

	ConfigureCompaction(
		execCtx, s.Config.Compaction, s.Model,
	)

	registry := events.NewRegistry()

	var streamWg sync.WaitGroup
	streamingHook := NewStreamingOutputHook(s.Writer)
	registry.Subscribe(streamingHook)

	if s.Config.LogWriter != nil {
		fileLogger := loggers.NewSubscriberWithWriter(
			s.Config.LogWriter,
		)
		registry.Subscribe(fileLogger)
	}

	chunks, unsubscribe := execCtx.SubscribeToTopic(
		"llm-response",
	)

	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		StreamConsumer(chunks, s.Writer, streamingHook)
	}()

	defer func() {
		unsubscribe()
		streamWg.Wait()
	}()

	exec := executor.New[*gent.BasicLoopData](
		loop, executor.Config{},
	).WithEvents(registry)

	fmt.Fprintln(s.Writer)
	PrintSection(s.Writer, "Your Input")
	fmt.Fprintln(s.Writer, userMessage)

	fmt.Fprintln(s.Writer)
	PrintSection(s.Writer, "Agent Processing")
	fmt.Fprintln(s.Writer)

	exec.Execute(execCtx)
	result := execCtx.Result()

	fmt.Fprintln(s.Writer)
	stats := execCtx.Stats()
	fmt.Fprintf(s.Writer,
		"[Stats: %d iterations, %d input tokens, "+
			"%d output tokens, %v]\n",
		execCtx.Iteration(),
		stats.GetTotalInputTokens(),
		stats.GetTotalOutputTokens(),
		execCtx.Duration())

	if result.Error != nil {
		fmt.Fprintf(s.Writer, "\nError: %v\n", result.Error)
		return result.Error
	}

	var responseText string
	for _, part := range result.Output {
		if tc, ok := part.(llms.TextContent); ok {
			responseText = tc.Text
		}
	}

	if responseText != "" {
		s.History = append(s.History, ConversationMessage{
			Role:    "agent",
			Content: responseText,
		})

		fmt.Fprintln(s.Writer)
		PrintSection(s.Writer, "Agent Response")
		fmt.Fprintln(s.Writer, responseText)
	}

	return nil
}

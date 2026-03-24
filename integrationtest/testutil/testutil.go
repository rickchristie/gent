// Package testutil provides shared test infrastructure for integration
// test scenarios.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"path/filepath"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/integrationtest/loggers"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/search"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// ToolChainType specifies which toolchain format to use.
type ToolChainType string

const (
	ToolChainYAML   ToolChainType = "yaml"
	ToolChainJSON   ToolChainType = "json"
	ToolChainSearch ToolChainType = "search"
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
	// SearchHintType controls how the tool summary is
	// shown in SearchJSON prompts. Only used when
	// ToolChain is ToolChainSearch.
	SearchHintType toolchain.SearchHintType
	// PinTools lists tool names to pin in SearchJSON.
	// Only used when ToolChain is ToolChainSearch.
	PinTools []string
	// WrapPTC wraps the toolchain with
	// JsToolChainWrapper for programmatic tool calling.
	WrapPTC bool
	// PTCCodeOnly disables direct_call in the PTC
	// wrapper, forcing all tool calls through code
	// execution. Only used when WrapPTC is true.
	PTCCodeOnly bool
	// ShowIterationHistory prints full iteration history at the end.
	ShowIterationHistory bool
	// ShowEvents prints all events at the end.
	ShowEvents bool
	// LogWriter is an optional writer for full debug logging.
	LogWriter io.Writer
	// Compaction configures scratchpad context management.
	Compaction CompactionConfig
	// Embedder for hybrid BM25+semantic search. Used by both tool search (SearchJSON)
	// and policy search (PolicySearchTool). Created by DefaultTestConfig.
	Embedder search.Embedder
	// ModelName overrides the LLM model. Default: "grok-4-1-fast".
	ModelName string
}

// DefaultTestConfig returns a config suitable for go test with JSON toolchain.
// Creates an ONNX embedder (multilingual-e5-small) for hybrid search. Panics if
// the model is not downloaded — run `gent setup onnx` first.
func DefaultTestConfig() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainJSON,
		ShowIterationHistory: true,
		ShowEvents:           true,
		Embedder:             createEmbedder(),
	}
}

// createEmbedder creates an ONNX embedder using multilingual-e5-small. This is a
// requirement for all integration tests — both tool search and policy search use
// hybrid BM25+semantic search.
func createEmbedder() search.Embedder {
	cfg := common.ConfigsForModel("multilingual-e5-small")[0]
	if !common.ModelDownloaded(&cfg.Model) {
		panic(
			"integration tests require multilingual-e5-small model. " +
				"Run: go run ./cmd/gent setup onnx",
		)
	}
	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		panic("failed to get model dir: " + err.Error())
	}
	embedder, err := search.NewOnnxEmbedder(cfg, search.OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     2,
		MaxConcurrency: 2,
	})
	if err != nil {
		panic("failed to create ONNX embedder: " + err.Error())
	}
	return embedder
}

// InteractiveConfig returns a config for interactive CLI with streaming
// and YAML toolchain.
func InteractiveConfig() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainYAML,
		ShowIterationHistory: false,
		ShowEvents:           false,
		Embedder:             createEmbedder(),
	}
}

// InteractiveConfigJSON returns a config for interactive CLI with
// streaming and JSON toolchain.
func InteractiveConfigJSON() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainJSON,
		ShowIterationHistory: false,
		ShowEvents:           false,
		Embedder:             createEmbedder(),
	}
}

// InteractiveConfigSearch returns a config for interactive
// CLI with streaming and SearchJSON toolchain.
func InteractiveConfigSearch() TestConfig {
	return TestConfig{
		ToolChain:            ToolChainSearch,
		ShowIterationHistory: false,
		ShowEvents:           false,
		Embedder:             createEmbedder(),
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

// DefaultModelName is the default LLM model for integration tests.
const DefaultModelName = gent.ModelXAIGrok41Fast

// ModelOption defines an LLM model available for integration tests.
type ModelOption struct {
	Label   string // display name in CLI menu
	Name    string // model ID for API calls
	EnvKey  string // env var for API key
	BaseURL string // API base URL (empty = OpenAI default)
}

// API base URLs for model providers.
const (
	baseURLXAI    = "https://api.x.ai/v1"
	baseURLGemini = "https://generativelanguage.googleapis.com/v1beta/openai/"
)

// Environment variable keys for API authentication.
const (
	envKeyXAI    = "GENT_TEST_XAI_KEY"
	envKeyOpenAI = "GENT_TEST_OPENAI_KEY"
	envKeyGemini = "GENT_TEST_GEMINI_KEY"
)

// AvailableModels lists all models the CLI can select from.
var AvailableModels = []ModelOption{
	{
		Label: "xAI grok-4-1-fast", Name: gent.ModelXAIGrok41Fast,
		EnvKey: envKeyXAI, BaseURL: baseURLXAI,
	},
	{
		Label: "xAI grok-4.20-0309-non-reasoning",
		Name: gent.ModelXAIGrok420NonReasoning,
		EnvKey: envKeyXAI, BaseURL: baseURLXAI,
	},
	{
		Label: "OpenAI o3", Name: gent.ModelOpenAIO3,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI o4-mini", Name: gent.ModelOpenAIO4Mini,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-4.1", Name: gent.ModelOpenAIGPT41,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-4.1-mini", Name: gent.ModelOpenAIGPT41Mini,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5", Name: gent.ModelOpenAIGPT5,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5-mini", Name: gent.ModelOpenAIGPT5Mini,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5-nano", Name: gent.ModelOpenAIGPT5Nano,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5.4", Name: gent.ModelOpenAIGPT54,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5.4-mini", Name: gent.ModelOpenAIGPT54Mini,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "OpenAI gpt-5.4-nano", Name: gent.ModelOpenAIGPT54Nano,
		EnvKey: envKeyOpenAI,
	},
	{
		Label: "Google gemini-2.5-pro", Name: gent.ModelGoogleGemini25Pro,
		EnvKey: envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label: "Google gemini-2.5-flash", Name: gent.ModelGoogleGemini25Flash,
		EnvKey: envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label:   "Google gemini-2.5-flash-lite",
		Name:    gent.ModelGoogleGemini25FlashLite,
		EnvKey:  envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label:   "Google gemini-3-pro-preview",
		Name:    gent.ModelGoogleGemini3Pro,
		EnvKey:  envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label:   "Google gemini-3-flash-preview",
		Name:    gent.ModelGoogleGemini3Flash,
		EnvKey:  envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label:   "Google gemini-3.1-pro-preview",
		Name:    gent.ModelGoogleGemini31Pro,
		EnvKey:  envKeyGemini, BaseURL: baseURLGemini,
	},
	{
		Label:   "Google gemini-3.1-flash-lite-preview",
		Name:    gent.ModelGoogleGemini31FlashLite,
		EnvKey:  envKeyGemini, BaseURL: baseURLGemini,
	},
}

// CreateModel creates an LLM model for testing. If modelName is
// empty, uses [DefaultModelName]. Looks up the model in
// [AvailableModels] to determine the API key env var and base URL.
func CreateModel(
	modelName string,
) (gent.StreamingModel, error) {
	if modelName == "" {
		modelName = DefaultModelName
	}

	// Find model config.
	var opt ModelOption
	for _, m := range AvailableModels {
		if m.Name == modelName {
			opt = m
			break
		}
	}
	if opt.Name == "" {
		// Unknown model — assume xAI.
		opt = ModelOption{
			Name: modelName, EnvKey: envKeyXAI,
			BaseURL: baseURLXAI,
		}
	}

	apiKey := os.Getenv(opt.EnvKey)
	if apiKey == "" {
		return nil, fmt.Errorf(
			"%s environment variable not set", opt.EnvKey,
		)
	}

	httpClient := &http.Client{
		Transport: &models.ErrorCaptureTransport{},
	}

	opts := []openai.Option{
		openai.WithToken(apiKey),
		openai.WithModel(modelName),
		openai.WithHTTPClient(httpClient),
	}
	if opt.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(opt.BaseURL))
	}

	llm, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create LLM: %w", err,
		)
	}

	return models.NewLCGWrapper(llm).
		WithModelName(modelName), nil
}

// CreateToolChain creates the appropriate toolchain based on config. For
// ToolChainSearch, returns a SearchJSON with hybrid BM25+semantic search.
// When WrapPTC is enabled, output schema printing is turned on.
func CreateToolChain(config TestConfig) gent.ToolChain {
	switch config.ToolChain {
	case ToolChainJSON:
		tc := toolchain.NewJSON()
		if config.WrapPTC {
			tc.WithOutputSchema(true)
		}
		return tc
	case ToolChainSearch:
		tc := toolchain.NewSearchJSON(config.SearchHintType).
			WithSearchType(toolchain.SearchGet).
			RegisterEngine(
				toolchain.NewFusedToolSearcher(config.Embedder),
			)
		if config.WrapPTC {
			tc.WithOutputSchema(true)
		}
		return tc
	default:
		tc := toolchain.NewYAML()
		if config.WrapPTC {
			tc.WithOutputSchema(true)
		}
		return tc
	}
}

// InitializeToolChain applies pinned tools and calls
// Initialize() on a SearchJSON toolchain. No-op for
// other toolchain types.
func InitializeToolChain(
	tc gent.ToolChain,
	config TestConfig,
) error {
	if stc, ok := tc.(*toolchain.SearchJSON); ok {
		for _, name := range config.PinTools {
			stc.Pin(name)
		}
		return stc.Initialize()
	}
	return nil
}

// WrapToolChain wraps the toolchain with
// JsToolChainWrapper if WrapPTC is enabled. Must be
// called after InitializeToolChain.
func WrapToolChain(
	tc gent.ToolChain,
	config TestConfig,
) gent.ToolChain {
	if config.WrapPTC {
		w := toolchain.NewJsToolChainWrapper(tc)
		if config.PTCCodeOnly {
			w.WithDirectCallDisabled()
		}
		return w
	}
	return tc
}

// ConfigureCompaction sets up compaction on the execution context
// based on the config. The model and textFormat are needed for summarization strategy.
func ConfigureCompaction(
	execCtx *gent.ExecutionContext, config CompactionConfig,
	model gent.Model, textFormat gent.TextFormat,
) {
	if config.Type == CompactionNone || config.Type == "" {
		return
	}

	trigger := compaction.NewStatThresholdTrigger().
		OnCounter(gent.SCIterations, config.TriggerIterations)

	var strategy gent.CompactionStrategy
	switch config.Type {
	case CompactionSlidingWindow:
		strategy = compaction.NewSlidingWindow(config.WindowSize)
	case CompactionSummarization:
		strategy = compaction.NewSummarization(model, textFormat).
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

// CriticalRules returns the standard critical rules for integration test
// scenarios. Extra rules are appended after the standard set. When the
// config uses SearchJSON or PTC, a tool schema discovery rule is added.
func CriticalRules(
	config TestConfig, extra ...string,
) string {
	rules := `DO NOT HALLUCINATE
- ALWAYS search and read the relevant policy BEFORE taking any action
- Every claim in your answer MUST come from tool outputs or user-provided information
- NEVER invent specific data (IDs, prices, times, availability)
- If information is missing, say so explicitly`
	for _, r := range extra {
		rules += "\n- " + r
	}
	if config.ToolChain == ToolChainSearch || config.WrapPTC {
		rules += "\n- You must have the tool schemas in " +
			"scratchpad before calling any tool. " +
			"Use tool_registry_search and get_tool_schema."
	}
	return rules
}

// PolicySuggestionHook implements BeforeIterationSubscriber to inject
// policy suggestions into the scratchpad on the first iteration. It
// chunks the conversation text, searches for relevant policies, and
// adds an auto_suggestion iteration with the top-5 policy IDs.
type PolicySuggestionHook struct {
	suggester func(ctx context.Context, text string) string
	text      string
	done      bool
}

func (h *PolicySuggestionHook) OnBeforeIteration(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeIterationEvent,
) {
	if h.done {
		return
	}
	h.done = true

	suggestion := h.suggester(execCtx.Context(), h.text)
	if suggestion == "" {
		return
	}

	// Format as XML TextSection.
	xmlFormat := format.NewXML()
	content := xmlFormat.FormatSections(
		[]gent.FormattedSection{
			{Name: "policy_suggestion", Content: suggestion},
		},
	)

	iter := &gent.Iteration{
		Origin:               gent.IterationSystemInjected,
		ExpireAfterIteration: 3,
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{
					llms.TextContent{Text: content},
				},
			},
		},
	}

	scratchpad := execCtx.Data().GetScratchPad()
	scratchpad = append(scratchpad, iter)
	execCtx.Data().SetScratchPad(scratchpad)
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
	// PolicySuggester generates a policy suggestion section for
	// the system prompt based on the conversation text. Called
	// once before the agent loop starts. Returns empty string
	// if no policies are relevant.
	PolicySuggester func(ctx context.Context, text string) string
}

// RunScenario executes a test scenario with the given configuration.
func RunScenario(
	ctx context.Context,
	w io.Writer,
	testCfg TestConfig,
	scenario ScenarioConfig,
) error {
	model, err := CreateModel(testCfg.ModelName)
	if err != nil {
		return err
	}

	tc := CreateToolChain(testCfg)
	scenario.RegisterTools(tc)
	if err := InitializeToolChain(tc, testCfg); err != nil {
		return fmt.Errorf(
			"failed to initialize toolchain: %w", err,
		)
	}
	tc = WrapToolChain(tc, testCfg)

	loop := react.NewAgent(model).
		WithToolChain(tc).
		WithTimeProvider(scenario.TimeProvider).
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
		format.NewXML(),
	)

	registry := events.NewRegistry()

	if scenario.PolicySuggester != nil {
		registry.Subscribe(&PolicySuggestionHook{
			suggester: scenario.PolicySuggester,
			text:      scenario.CustomerRequest,
		})
	}

	var streamWg sync.WaitGroup
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
	// PolicySuggester generates a policy suggestion section based
	// on conversation history. Called on each SendMessage with
	// the last 10 messages formatted as text. Returns empty string
	// if no policies are relevant.
	PolicySuggester func(ctx context.Context, text string) string
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
	model, err := CreateModel(config.ModelName)
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

// recentHistoryText formats the last N messages as text for policy suggestion.
func (s *InteractiveChat) recentHistoryText(maxMessages int) string {
	history := s.History
	if len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	var sb strings.Builder
	for _, msg := range history {
		fmt.Fprintf(&sb, "- %s: %s\n", msg.Role, msg.Content)
	}
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
	if err := InitializeToolChain(
		tc, s.Config,
	); err != nil {
		return fmt.Errorf(
			"failed to initialize toolchain: %w", err,
		)
	}
	tc = WrapToolChain(tc, s.Config)

	loop := react.NewAgent(s.Model).
		WithToolChain(tc).
		WithTimeProvider(s.ChatCfg.TimeProvider).
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
		format.NewXML(),
	)

	registry := events.NewRegistry()

	if s.ChatCfg.PolicySuggester != nil {
		registry.Subscribe(&PolicySuggestionHook{
			suggester: s.ChatCfg.PolicySuggester,
			text:      s.recentHistoryText(10),
		})
	}

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

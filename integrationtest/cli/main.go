// Package main provides a unified interactive CLI for testing
// integration scenarios with real-time streaming output.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/rickchristie/gent/integrationtest/airline"
	"github.com/rickchristie/gent/integrationtest/ecommerce"
	"github.com/rickchristie/gent/integrationtest/testutil"
	"github.com/rickchristie/gent/toolchain"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr,
			"%sError: %v%s\n",
			colorRed, err, colorReset)
		os.Exit(1)
	}
}

func run() error {
	// Create log directory
	logDir := ".logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf(
			"failed to create log directory: %w", err)
	}

	// Create readline instance for menu
	rl, err := readline.New(
		colorCyan +
			"Enter selection (or 'q' to quit): " +
			colorReset)
	if err != nil {
		return fmt.Errorf(
			"failed to create readline: %w", err)
	}
	defer rl.Close()

	// Check if API key is set
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		fmt.Fprintf(os.Stderr,
			"%sWARNING: GENT_TEST_XAI_KEY "+
				"environment variable is not set!%s\n",
			colorYellow, colorReset)
		fmt.Fprintf(os.Stderr,
			"%sTests will fail. Please set the API "+
				"key or source your .env file.%s\n",
			colorYellow, colorReset)
		fmt.Fprintln(os.Stderr)
	}

	for {
		// Step 1: Scenario
		scenario, err := promptMenu(rl,
			"Test Scenario",
			[]menuOption{
				{label: "Airline", value: "airline"},
				{label: "E-commerce", value: "ecommerce"},
			},
		)
		if err != nil {
			return handleMenuErr(err)
		}

		// Step 2: Mode
		mode, err := promptMenu(rl,
			"Mode",
			[]menuOption{
				{label: "One shot", value: "oneshot"},
				{
					label: "Interactive chat",
					value: "chat",
				},
			},
		)
		if err != nil {
			return handleMenuErr(err)
		}

		// Step 3: Programmatic Tool Calling
		ptcChoice, err := promptMenu(rl,
			"Programmatic Tool Calling (PTC)",
			[]menuOption{
				{
					label: "No  — standard tool calls",
					value: "no",
				},
				{
					label: "Yes — wrap in " +
						"JsToolChainWrapper",
					value: "yes",
				},
			},
		)
		if err != nil {
			return handleMenuErr(err)
		}

		// Step 4: ToolChain
		tcType, err := promptMenu(rl,
			"ToolChain",
			[]menuOption{
				{label: "YAML", value: "yaml"},
				{label: "JSON", value: "json"},
				{
					label: "ToolSearchToolChain",
					value: "search",
				},
			},
		)
		if err != nil {
			return handleMenuErr(err)
		}

		// Step 4b: Search hint type (only for search)
		var hintType string
		if tcType == "search" {
			hintType, err = promptMenu(rl,
				"Tool Summary Style",
				[]menuOption{
					{
						label: "Simple list " +
							"(all tool names)",
						value: "simple",
					},
					{
						label: "Domain categories",
						value: "domain",
					},
				},
			)
			if err != nil {
				return handleMenuErr(err)
			}
		}

		// Step 5: Compaction
		compactionCfg, err := promptCompaction(rl)
		if err != nil {
			if err == readline.ErrInterrupt {
				continue
			}
			return err
		}

		// Build config
		config := buildConfig(tcType, hintType)
		config.WrapPTC = ptcChoice == "yes"
		config.Compaction = compactionCfg

		// Create per-run log file
		ptcSuffix := ""
		if config.WrapPTC {
			ptcSuffix = "_ptc"
		}
		logFileName := fmt.Sprintf(
			"%s_%s_%s_%s%s.log",
			time.Now().Format("20060102_150405"),
			scenario, mode, tcType, ptcSuffix,
		)
		logPath := filepath.Join(
			logDir, logFileName,
		)
		logFile, logErr := os.Create(logPath)
		if logErr != nil {
			fmt.Fprintf(os.Stderr,
				"%sFailed to create log: %v%s\n",
				colorRed, logErr, colorReset)
		} else {
			config.LogWriter = logFile
			fmt.Printf(
				"%sLog file: %s%s\n",
				colorDim, logPath, colorReset,
			)
		}

		// Run
		ctx, cancel := context.WithCancel(
			context.Background())

		sigCh := make(chan os.Signal, 1)
		signal.Notify(
			sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Printf(
				"\n%sReceived interrupt, "+
					"cancelling...%s\n",
				colorYellow, colorReset)
			cancel()
		}()

		runErr := execute(
			ctx, scenario, mode, tcType,
			config,
		)
		if runErr != nil {
			fmt.Fprintf(os.Stderr,
				"%sError: %v%s\n",
				colorRed, runErr, colorReset)
		}

		if logFile != nil {
			logFile.Close()
		}

		signal.Stop(sigCh)
		cancel()

		fmt.Printf("\n%s%s%s\n\n",
			colorDim,
			strings.Repeat("-", 60),
			colorReset)
	}
}

// menuOption represents a single menu choice.
type menuOption struct {
	label string
	value string
}

// promptMenu displays a numbered menu and returns the
// selected value. Returns readline.ErrInterrupt on Ctrl-C.
func promptMenu(
	rl *readline.Instance,
	title string,
	options []menuOption,
) (string, error) {
	fmt.Println()
	fmt.Printf(
		"%s%s%s:%s\n",
		colorBold, colorYellow, title, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", len(title)+1),
		colorReset)
	for i, opt := range options {
		fmt.Printf("  %s%d.%s %s\n",
			colorCyan, i+1, colorReset, opt.label)
	}
	fmt.Println()

	for {
		oldPrompt := rl.Config.Prompt
		rl.SetPrompt(
			colorCyan + "Select [1]: " + colorReset)
		input, err := rl.Readline()
		rl.SetPrompt(oldPrompt)
		if err != nil {
			return "", err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			return options[0].value, nil
		}
		if input == "q" || input == "Q" {
			fmt.Printf(
				"%sGoodbye!%s\n",
				colorGreen, colorReset)
			os.Exit(0)
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 ||
			num > len(options) {
			fmt.Printf(
				"%sInvalid. Enter 1-%d.%s\n",
				colorRed, len(options), colorReset)
			continue
		}
		return options[num-1].value, nil
	}
}

// handleMenuErr handles errors from menu prompts.
func handleMenuErr(err error) error {
	if err == readline.ErrInterrupt {
		fmt.Printf(
			"\n%sGoodbye!%s\n",
			colorGreen, colorReset)
		return nil
	}
	return err
}

// buildConfig creates a TestConfig from the selected
// toolchain type and hint type.
func buildConfig(
	tcType, hintType string,
) testutil.TestConfig {
	var config testutil.TestConfig
	switch tcType {
	case "yaml":
		config = testutil.InteractiveConfig()
	case "json":
		config = testutil.InteractiveConfigJSON()
	case "search":
		config = testutil.InteractiveConfigSearch()
	default:
		config = testutil.InteractiveConfig()
	}

	if hintType == "simple" {
		config.SearchHintType =
			toolchain.SearchHintSimpleList
	}
	return config
}

// execute runs the selected scenario/mode combination.
func execute(
	ctx context.Context,
	scenario, mode, tcType string,
	config testutil.TestConfig,
) error {
	if mode == "chat" {
		return runChat(ctx, scenario, tcType, config)
	}
	return runOneShot(ctx, scenario, tcType, config)
}

// runOneShot runs a one-shot test scenario.
func runOneShot(
	ctx context.Context,
	scenario, tcType string,
	config testutil.TestConfig,
) error {
	name := fmt.Sprintf(
		"%s — One shot (%s)", scenario, tcType)
	fmt.Printf("\n%sRunning: %s%s\n",
		colorGreen, name, colorReset)

	switch scenario {
	case "airline":
		if tcType == "search" {
			return airline.RunRescheduleScenarioSearch(
				ctx, os.Stdout, config,
			)
		}
		return airline.RunRescheduleScenario(
			ctx, os.Stdout, config,
		)
	case "ecommerce":
		if tcType == "search" {
			return ecommerce.RunDoubleChargeScenarioSearch(
				ctx, os.Stdout, config,
			)
		}
		return ecommerce.RunDoubleChargeScenario(
			ctx, os.Stdout, config,
		)
	default:
		return fmt.Errorf(
			"unknown scenario: %s", scenario)
	}
}

// runChat starts an interactive chat session.
func runChat(
	ctx context.Context,
	scenario, tcType string,
	config testutil.TestConfig,
) error {
	type chatFactory func(
		io.Writer, testutil.TestConfig,
	) (*testutil.InteractiveChat, error)

	var newChat chatFactory
	switch scenario {
	case "airline":
		if tcType == "search" {
			newChat = airline.NewAirlineInteractiveChatSearch
		} else {
			newChat = airline.NewAirlineInteractiveChat
		}
	case "ecommerce":
		if tcType == "search" {
			newChat = ecommerce.NewEcommerceInteractiveChatSearch
		} else {
			newChat = ecommerce.NewEcommerceInteractiveChat
		}
	default:
		return fmt.Errorf(
			"unknown scenario: %s", scenario)
	}

	return runInteractiveChat(ctx, config, newChat)
}

// promptCompaction presents the compaction strategy selection
// menu and returns the user's configuration.
func promptCompaction(
	rl *readline.Instance,
) (testutil.CompactionConfig, error) {
	fmt.Println()
	fmt.Printf(
		"%s%sScratchpad Context Management:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", 30),
		colorReset)
	fmt.Printf(
		"  %s1.%s None            - "+
			"No context management (default)\n",
		colorCyan, colorReset)
	fmt.Printf(
		"  %s2.%s Sliding Window  - "+
			"Keep last N iterations, discard older\n",
		colorCyan, colorReset)
	fmt.Printf(
		"  %s3.%s Summarization   - "+
			"Summarize older iterations into a "+
			"synopsis\n",
		colorCyan, colorReset)
	fmt.Println()

	for {
		oldPrompt := rl.Config.Prompt
		rl.SetPrompt(
			colorCyan +
				"Select strategy [1]: " +
				colorReset)
		input, err := rl.Readline()
		rl.SetPrompt(oldPrompt)
		if err != nil {
			return testutil.CompactionConfig{}, err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			input = "1"
		}

		switch input {
		case "1":
			return testutil.CompactionConfig{
				Type: testutil.CompactionNone,
			}, nil
		case "2":
			return promptSlidingWindow(rl)
		case "3":
			return promptSummarization(rl)
		default:
			fmt.Printf(
				"%sInvalid. Enter 1, 2, or 3.%s\n",
				colorRed, colorReset)
		}
	}
}

// promptSlidingWindow configures sliding window compaction.
func promptSlidingWindow(
	rl *readline.Instance,
) (testutil.CompactionConfig, error) {
	fmt.Println()
	fmt.Printf(
		"%s%sConfigure Sliding Window:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", 25),
		colorReset)

	windowSize, err := promptInt(rl,
		"Window size (recent iterations to keep)",
		5, 1, 100)
	if err != nil {
		return testutil.CompactionConfig{}, err
	}

	triggerIter, err := promptInt(rl,
		"Trigger every N iterations",
		3, 1, 50)
	if err != nil {
		return testutil.CompactionConfig{}, err
	}

	cfg := testutil.CompactionConfig{
		Type: testutil.CompactionSlidingWindow,
		TriggerIterations: int64(triggerIter),
		WindowSize:        windowSize,
	}

	fmt.Printf(
		"\n%sSliding Window: window=%d, "+
			"trigger every %d iterations%s\n",
		colorGreen, windowSize, triggerIter, colorReset)

	return cfg, nil
}

// promptSummarization configures summarization compaction.
func promptSummarization(
	rl *readline.Instance,
) (testutil.CompactionConfig, error) {
	fmt.Println()
	fmt.Printf(
		"%s%sConfigure Summarization:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", 24),
		colorReset)

	keepRecent, err := promptInt(rl,
		"Keep recent (iterations to preserve "+
			"unsummarized)",
		3, 0, 50)
	if err != nil {
		return testutil.CompactionConfig{}, err
	}

	triggerIter, err := promptInt(rl,
		"Trigger every N iterations",
		3, 1, 50)
	if err != nil {
		return testutil.CompactionConfig{}, err
	}

	cfg := testutil.CompactionConfig{
		Type: testutil.CompactionSummarization,
		TriggerIterations: int64(triggerIter),
		KeepRecent:        keepRecent,
	}

	fmt.Printf(
		"\n%sSummarization: keepRecent=%d, "+
			"trigger every %d iterations%s\n",
		colorGreen, keepRecent, triggerIter, colorReset)

	return cfg, nil
}

// promptInt prompts for an integer value with a default,
// minimum, and maximum.
func promptInt(
	rl *readline.Instance,
	label string,
	defaultVal, minVal, maxVal int,
) (int, error) {
	for {
		oldPrompt := rl.Config.Prompt
		prompt := fmt.Sprintf(
			"%s  %s [%d]: %s",
			colorCyan, label, defaultVal, colorReset)
		rl.SetPrompt(prompt)
		input, err := rl.Readline()
		rl.SetPrompt(oldPrompt)
		if err != nil {
			return 0, err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			return defaultVal, nil
		}

		val, err := strconv.Atoi(input)
		if err != nil || val < minVal || val > maxVal {
			fmt.Printf(
				"%sEnter a number between %d "+
					"and %d.%s\n",
				colorRed, minVal, maxVal, colorReset)
			continue
		}
		return val, nil
	}
}

func runInteractiveChat(
	ctx context.Context,
	config testutil.TestConfig,
	newChat func(
		io.Writer, testutil.TestConfig,
	) (*testutil.InteractiveChat, error),
) error {
	fmt.Println()
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("=", 80),
		colorReset)
	fmt.Printf("%s%sINTERACTIVE CHAT%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("=", 80),
		colorReset)
	fmt.Println()
	fmt.Printf(
		"%sYou are now chatting with the "+
			"customer service agent.%s\n",
		colorWhite, colorReset)
	fmt.Printf(
		"%sType your message and press Enter. "+
			"Type 'exit' to end the chat.%s\n",
		colorDim, colorReset)
	fmt.Printf(
		"%sUse arrow keys to edit your input.%s\n",
		colorDim, colorReset)

	if config.Compaction.Type != testutil.CompactionNone &&
		config.Compaction.Type != "" {
		fmt.Printf(
			"%s[Compaction: %s, trigger every %d "+
				"iterations]%s\n",
			colorDim,
			config.Compaction.Type,
			config.Compaction.TriggerIterations,
			colorReset)
	}

	fmt.Println()

	rl, err := readline.New(
		colorCyan + colorBold + "You: " + colorReset)
	if err != nil {
		return fmt.Errorf(
			"failed to create readline: %w", err)
	}
	defer rl.Close()

	coloredWriter := &ColoredWriter{w: os.Stdout}

	chat, err := newChat(coloredWriter, config)
	if err != nil {
		return fmt.Errorf(
			"failed to create chat session: %w", err)
	}

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Printf(
					"\n%sChat cancelled.%s\n",
					colorYellow, colorReset)
				return nil
			}
			return fmt.Errorf(
				"failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Printf(
				"\n%sEnding chat session. "+
					"Goodbye!%s\n",
				colorGreen, colorReset)
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Printf(
				"\n%sChat cancelled.%s\n",
				colorYellow, colorReset)
			return ctx.Err()
		default:
		}

		err = chat.SendMessage(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"\n%sError processing message: "+
					"%v%s\n",
				colorRed, err, colorReset)
		}
	}
}

// ColoredWriter wraps an io.Writer and adds colors
// based on content patterns.
type ColoredWriter struct {
	w               *os.File
	inAgentResponse bool
}

func (c *ColoredWriter) Write(
	p []byte,
) (n int, err error) {
	text := string(p)
	trimmed := strings.TrimSpace(text)

	switch {
	case strings.HasPrefix(text, "--- Your Input ---"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorCyan, text, colorReset)

	case strings.HasPrefix(
		text, "--- Agent Response ---"):
		c.inAgentResponse = true
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorGreen, text, colorReset)

	case strings.HasPrefix(
		text, "--- Agent Processing ---"):
		c.inAgentResponse = false
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorYellow, text, colorReset)

	case strings.HasPrefix(text, "--- ") &&
		strings.HasSuffix(trimmed, " ---"):
		c.inAgentResponse = false
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorYellow, text, colorReset)

	case c.inAgentResponse && trimmed != "":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorGreen, text, colorReset)

	case strings.HasPrefix(text, "[Tool:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorBlue, text, colorReset)

	case strings.HasPrefix(text, "    Args:") ||
		strings.HasPrefix(text, "    Output:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Duration:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Error:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorRed, text, colorReset)

	case strings.HasPrefix(text, "[Stats:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorDim, text, colorReset)

	case strings.HasPrefix(text, "--- Iteration"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorMagenta, text, colorReset)

	case strings.HasPrefix(text, "  LLM: "):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorCyan, text, colorReset)

	case strings.HasPrefix(text, "  [Compaction:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorMagenta, text, colorReset)

	case strings.HasPrefix(
		text, "  [Limit Exceeded:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorRed, text, colorReset)

	case trimmed == "<thinking>" ||
		trimmed == "</thinking>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorDim, text, colorReset)

	case trimmed == "<action>" ||
		trimmed == "</action>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorBlue, text, colorReset)

	case trimmed == "<answer>" ||
		trimmed == "</answer>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s",
			colorGreen, text, colorReset)

	default:
		return os.Stdout.Write(p)
	}
}

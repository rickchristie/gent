// Package main provides an interactive CLI for testing airline scenarios
// with real-time streaming output.
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

	"github.com/chzyer/readline"
	"github.com/rickchristie/gent/integrationtest/airline"
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

	// Background colors
	bgBlack = "\033[40m"
	bgBlue  = "\033[44m"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr,
			"%sError: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
}

func run() error {
	// Create log directory and file
	logDir := ".logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf(
			"failed to create log directory: %w", err)
	}

	logFile, err := os.Create(
		filepath.Join(logDir, "cli_airline.log"))
	if err != nil {
		return fmt.Errorf(
			"failed to create log file: %w", err)
	}
	defer logFile.Close()

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
			"%sTests will fail. Please set the API key"+
				" or source your .env file.%s\n",
			colorYellow, colorReset)
		fmt.Fprintln(os.Stderr)
	}

	// Build menu items
	type menuItem struct {
		name        string
		description string
		run         func(
			ctx context.Context,
			w io.Writer,
			config airline.AirlineTestConfig,
		) error
		configFn func() airline.AirlineTestConfig
		isChat   bool
	}

	var menuItems []menuItem

	// Add YAML test cases
	for _, tc := range airline.GetAirlineTestCases() {
		menuItems = append(menuItems, menuItem{
			name:        tc.Name,
			description: tc.Description,
			run:         tc.Run,
			configFn:    airline.InteractiveConfig,
		})
	}

	// Add JSON test cases
	for _, tc := range airline.GetAirlineTestCasesJSON() {
		menuItems = append(menuItems, menuItem{
			name:        tc.Name,
			description: tc.Description,
			run:         tc.Run,
			configFn:    airline.InteractiveConfigJSON,
		})
	}

	// Add interactive chat options
	menuItems = append(menuItems, menuItem{
		name: "Interactive Chat (YAML)",
		description: "Chat with the airline agent " +
			"using YAML toolchain",
		configFn: airline.InteractiveConfig,
		isChat:   true,
	})
	menuItems = append(menuItems, menuItem{
		name: "Interactive Chat (JSON)",
		description: "Chat with the airline agent " +
			"using JSON toolchain",
		configFn: airline.InteractiveConfigJSON,
		isChat:   true,
	})

	fmt.Printf("%s%sAvailable Airline Tests:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow, strings.Repeat("=", 24), colorReset)
	for i, item := range menuItems {
		fmt.Printf("  %s%d.%s %s%s%s - %s\n",
			colorCyan, i+1, colorReset,
			colorWhite, item.name, colorReset,
			item.description)
	}
	fmt.Println()

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Printf(
					"\n%sGoodbye!%s\n",
					colorGreen, colorReset)
				return nil
			}
			return fmt.Errorf(
				"failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "q" || input == "Q" {
			fmt.Printf(
				"%sGoodbye!%s\n", colorGreen, colorReset)
			return nil
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > len(menuItems) {
			fmt.Printf(
				"%sInvalid selection. "+
					"Please enter 1-%d.%s\n\n",
				colorRed, len(menuItems), colorReset)
			continue
		}

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

		item := menuItems[num-1]
		config := item.configFn()
		config.LogWriter = logFile

		// Prompt for compaction configuration
		compactionCfg, err := promptCompaction(rl)
		if err != nil {
			signal.Stop(sigCh)
			cancel()
			if err == readline.ErrInterrupt {
				continue
			}
			return err
		}
		config.Compaction = compactionCfg

		if item.isChat {
			err = runInteractiveChat(ctx, config)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"%sError: %v%s\n",
					colorRed, err, colorReset)
			}
		} else {
			fmt.Printf("\n%sRunning test: %s%s\n",
				colorGreen, item.name, colorReset)
			err = item.run(ctx, os.Stdout, config)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"%sError: %v%s\n",
					colorRed, err, colorReset)
			}
		}

		signal.Stop(sigCh)
		cancel()

		fmt.Printf("\n%s%s%s\n\n",
			colorDim, strings.Repeat("-", 60), colorReset)
	}
}

// promptCompaction presents the compaction strategy selection
// menu and returns the user's configuration.
func promptCompaction(
	rl *readline.Instance,
) (airline.CompactionConfig, error) {
	fmt.Println()
	fmt.Printf(
		"%s%sScratchpad Context Management:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", 30),
		colorReset)
	fmt.Printf(
		"  %s1.%s Sliding Window  - "+
			"Keep last N iterations, discard older\n",
		colorCyan, colorReset)
	fmt.Printf(
		"  %s2.%s Summarization   - "+
			"Summarize older iterations into a synopsis\n",
		colorCyan, colorReset)
	fmt.Printf(
		"  %s3.%s None            - "+
			"No context management (default)\n",
		colorCyan, colorReset)
	fmt.Println()

	for {
		oldPrompt := rl.Config.Prompt
		rl.SetPrompt(
			colorCyan + "Select strategy [3]: " + colorReset)
		input, err := rl.Readline()
		rl.SetPrompt(oldPrompt)
		if err != nil {
			return airline.CompactionConfig{}, err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			input = "3"
		}

		switch input {
		case "1":
			return promptSlidingWindow(rl)
		case "2":
			return promptSummarization(rl)
		case "3":
			return airline.CompactionConfig{
				Type: airline.CompactionNone,
			}, nil
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
) (airline.CompactionConfig, error) {
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
		return airline.CompactionConfig{}, err
	}

	triggerIter, err := promptInt(rl,
		"Trigger every N iterations",
		3, 1, 50)
	if err != nil {
		return airline.CompactionConfig{}, err
	}

	cfg := airline.CompactionConfig{
		Type:              airline.CompactionSlidingWindow,
		TriggerIterations: int64(triggerIter),
		WindowSize:        windowSize,
	}

	fmt.Printf("\n%sSliding Window: window=%d, "+
		"trigger every %d iterations%s\n",
		colorGreen, windowSize, triggerIter, colorReset)

	return cfg, nil
}

// promptSummarization configures summarization compaction.
func promptSummarization(
	rl *readline.Instance,
) (airline.CompactionConfig, error) {
	fmt.Println()
	fmt.Printf(
		"%s%sConfigure Summarization:%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("-", 24),
		colorReset)

	keepRecent, err := promptInt(rl,
		"Keep recent (iterations to preserve unsummarized)",
		3, 0, 50)
	if err != nil {
		return airline.CompactionConfig{}, err
	}

	triggerIter, err := promptInt(rl,
		"Trigger every N iterations",
		3, 1, 50)
	if err != nil {
		return airline.CompactionConfig{}, err
	}

	cfg := airline.CompactionConfig{
		Type:              airline.CompactionSummarization,
		TriggerIterations: int64(triggerIter),
		KeepRecent:        keepRecent,
	}

	fmt.Printf("\n%sSummarization: keepRecent=%d, "+
		"trigger every %d iterations%s\n",
		colorGreen, keepRecent, triggerIter, colorReset)

	return cfg, nil
}

// promptInt prompts the user for an integer value with a
// default, minimum, and maximum.
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
				"%sEnter a number between %d and %d.%s\n",
				colorRed, minVal, maxVal, colorReset)
			continue
		}
		return val, nil
	}
}

func runInteractiveChat(
	ctx context.Context,
	config airline.AirlineTestConfig,
) error {
	fmt.Println()
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("=", 80),
		colorReset)
	fmt.Printf("%s%sINTERACTIVE AIRLINE CHAT%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n",
		colorYellow,
		strings.Repeat("=", 80),
		colorReset)
	fmt.Println()
	fmt.Printf(
		"%sYou are now chatting with the "+
			"SkyWings Airlines customer service agent.%s\n",
		colorWhite, colorReset)
	fmt.Printf(
		"%sType your message and press Enter. "+
			"Type 'exit' to end the chat.%s\n",
		colorDim, colorReset)
	fmt.Printf(
		"%sUse arrow keys to edit your input.%s\n",
		colorDim, colorReset)

	// Print compaction config reminder if enabled
	if config.Compaction.Type != airline.CompactionNone &&
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

	// Create readline instance for chat with custom prompt
	rl, err := readline.New(
		colorCyan + colorBold + "You: " + colorReset)
	if err != nil {
		return fmt.Errorf(
			"failed to create readline: %w", err)
	}
	defer rl.Close()

	// Create colored writer for the chat
	coloredWriter := &ColoredWriter{w: os.Stdout}

	// Create chat session with colored output
	chat, err := airline.NewInteractiveChat(
		coloredWriter, config)
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
				"\n%sEnding chat session. Goodbye!%s\n",
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
				"\n%sError processing message: %v%s\n",
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

	// Color code different types of output
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
			"%s%s%s", colorYellow, text, colorReset)

	case strings.HasPrefix(text, "--- ") &&
		strings.HasSuffix(trimmed, " ---"):
		c.inAgentResponse = false
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorYellow, text, colorReset)

	case c.inAgentResponse && trimmed != "":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorGreen, text, colorReset)

	case strings.HasPrefix(text, "[Tool:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorBlue, text, colorReset)

	case strings.HasPrefix(text, "    Args:") ||
		strings.HasPrefix(text, "    Output:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Duration:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Error:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorRed, text, colorReset)

	case strings.HasPrefix(text, "[Stats:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "--- Iteration"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorMagenta, text, colorReset)

	case strings.HasPrefix(text, "  LLM: "):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorCyan, text, colorReset)

	case strings.HasPrefix(text, "  [Compaction:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorMagenta, text, colorReset)

	case strings.HasPrefix(text, "  [Limit Exceeded:"):
		return fmt.Fprintf(os.Stdout,
			"%s%s%s%s",
			colorBold, colorRed, text, colorReset)

	case trimmed == "<thinking>" ||
		trimmed == "</thinking>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorDim, text, colorReset)

	case trimmed == "<action>" ||
		trimmed == "</action>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorBlue, text, colorReset)

	case trimmed == "<answer>" ||
		trimmed == "</answer>":
		return fmt.Fprintf(os.Stdout,
			"%s%s%s", colorGreen, text, colorReset)

	default:
		return os.Stdout.Write(p)
	}
}

// Package main provides an interactive CLI for testing airline scenarios
// with real-time streaming output.
package main

import (
	"context"
	"fmt"
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
		fmt.Fprintf(os.Stderr, "%sError: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
}

func run() error {
	testCases := airline.GetAirlineTestCases()

	// Create log directory and file
	logDir := ".logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile, err := os.Create(filepath.Join(logDir, "cli_airline.log"))
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	// Create readline instance for menu
	rl, err := readline.New(colorCyan + "Enter test number (or 'q' to quit): " + colorReset)
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	// Check if API key is set
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		fmt.Fprintf(os.Stderr, "%sWARNING: GENT_TEST_XAI_KEY environment variable is not set!%s\n",
			colorYellow, colorReset)
		fmt.Fprintf(os.Stderr, "%sTests will fail. Please set the API key or source your .env file.%s\n",
			colorYellow, colorReset)
		fmt.Fprintln(os.Stderr)
	}

	fmt.Printf("%s%sAvailable Airline Tests:%s\n", colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n", colorYellow, strings.Repeat("=", 24), colorReset)
	for i, tc := range testCases {
		fmt.Printf("  %s%d.%s %s%s%s - %s\n",
			colorCyan, i+1, colorReset,
			colorWhite, tc.Name, colorReset,
			tc.Description)
	}
	fmt.Printf("  %s%d.%s %s%s%s - %s\n",
		colorCyan, len(testCases)+1, colorReset,
		colorWhite, "Interactive Chat", colorReset,
		"Chat with the airline agent")
	fmt.Println()

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Printf("\n%sGoodbye!%s\n", colorGreen, colorReset)
				return nil
			}
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "q" || input == "Q" {
			fmt.Printf("%sGoodbye!%s\n", colorGreen, colorReset)
			return nil
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > len(testCases)+1 {
			fmt.Printf("%sInvalid selection. Please enter a number between 1 and %d.%s\n\n",
				colorRed, len(testCases)+1, colorReset)
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Printf("\n%sReceived interrupt, cancelling...%s\n", colorYellow, colorReset)
			cancel()
		}()

		config := airline.InteractiveConfig()
		config.LogWriter = logFile
		if num == len(testCases)+1 {
			if err := runInteractiveChat(ctx, config); err != nil {
				fmt.Fprintf(os.Stderr, "%sError: %v%s\n", colorRed, err, colorReset)
			}
		} else {
			tc := testCases[num-1]
			fmt.Printf("\n%sRunning test: %s%s\n", colorGreen, tc.Name, colorReset)
			if err := tc.Run(ctx, os.Stdout, config); err != nil {
				fmt.Fprintf(os.Stderr, "%sError: %v%s\n", colorRed, err, colorReset)
			}
		}

		signal.Stop(sigCh)
		cancel()

		fmt.Printf("\n%s%s%s\n\n", colorDim, strings.Repeat("-", 60), colorReset)
	}
}

func runInteractiveChat(ctx context.Context, config airline.AirlineTestConfig) error {
	fmt.Println()
	fmt.Printf("%s%s%s\n", colorYellow, strings.Repeat("=", 80), colorReset)
	fmt.Printf("%s%sINTERACTIVE AIRLINE CHAT%s\n", colorBold, colorYellow, colorReset)
	fmt.Printf("%s%s%s\n", colorYellow, strings.Repeat("=", 80), colorReset)
	fmt.Println()
	fmt.Printf("%sYou are now chatting with the SkyWings Airlines customer service agent.%s\n",
		colorWhite, colorReset)
	fmt.Printf("%sType your message and press Enter. Type 'exit' to end the chat.%s\n",
		colorDim, colorReset)
	fmt.Printf("%sUse arrow keys to edit your input.%s\n", colorDim, colorReset)
	fmt.Println()

	// Create readline instance for chat with custom prompt
	rl, err := readline.New(colorCyan + colorBold + "You: " + colorReset)
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	// Create colored writer for the chat
	coloredWriter := &ColoredWriter{w: os.Stdout}

	// Create chat session with colored output
	chat, err := airline.NewInteractiveChat(coloredWriter, config)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Printf("\n%sChat cancelled.%s\n", colorYellow, colorReset)
				return nil
			}
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Printf("\n%sEnding chat session. Goodbye!%s\n", colorGreen, colorReset)
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Printf("\n%sChat cancelled.%s\n", colorYellow, colorReset)
			return ctx.Err()
		default:
		}

		if err := chat.SendMessage(ctx, input); err != nil {
			fmt.Fprintf(os.Stderr, "\n%sError processing message: %v%s\n", colorRed, err, colorReset)
		}
	}
}

// ColoredWriter wraps an io.Writer and adds colors based on content patterns.
type ColoredWriter struct {
	w               *os.File
	inAgentResponse bool // tracks if we're in the agent response section
}

func (c *ColoredWriter) Write(p []byte) (n int, err error) {
	text := string(p)
	trimmed := strings.TrimSpace(text)

	// Color code different types of output
	switch {
	case strings.HasPrefix(text, "--- Your Input ---"):
		// User input header
		return fmt.Fprintf(os.Stdout, "%s%s%s%s", colorBold, colorCyan, text, colorReset)

	case strings.HasPrefix(text, "--- Agent Response ---"):
		// Final response header - also set flag for green response text
		c.inAgentResponse = true
		return fmt.Fprintf(os.Stdout, "%s%s%s%s", colorBold, colorGreen, text, colorReset)

	case strings.HasPrefix(text, "--- Agent Processing ---"):
		// Processing header - end agent response section
		c.inAgentResponse = false
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorYellow, text, colorReset)

	case strings.HasPrefix(text, "--- ") && strings.HasSuffix(trimmed, " ---"):
		// Other section headers
		c.inAgentResponse = false
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorYellow, text, colorReset)

	case c.inAgentResponse && trimmed != "":
		// Content after Agent Response header - show in green
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorGreen, text, colorReset)

	case strings.HasPrefix(text, "[Tool:"):
		// Tool calls
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorBlue, text, colorReset)

	case strings.HasPrefix(text, "    Args:") || strings.HasPrefix(text, "    Output:"):
		// Tool args and output
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Duration:"):
		// Duration info
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "    Error:"):
		// Tool errors
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorRed, text, colorReset)

	case strings.HasPrefix(text, "[Stats:"):
		// Stats line
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorDim, text, colorReset)

	case strings.HasPrefix(text, "--- Iteration"):
		// Iteration headers
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorMagenta, text, colorReset)

	case strings.HasPrefix(text, "  LLM: "):
		// LLM output prefix
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorCyan, text, colorReset)

	case trimmed == "<thinking>" || trimmed == "</thinking>":
		// Thinking tags only (exact match)
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorDim, text, colorReset)

	case trimmed == "<action>" || trimmed == "</action>":
		// Action tags only (exact match)
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorBlue, text, colorReset)

	case trimmed == "<answer>" || trimmed == "</answer>":
		// Answer tags only (exact match)
		return fmt.Fprintf(os.Stdout, "%s%s%s", colorGreen, text, colorReset)

	default:
		return os.Stdout.Write(p)
	}
}

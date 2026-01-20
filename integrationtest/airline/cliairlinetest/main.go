// Package main provides an interactive CLI for testing airline scenarios
// with real-time streaming output.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/rickchristie/gent/integrationtest/airline"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	testCases := airline.GetAirlineTestCases()

	// Check if API key is set
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		fmt.Fprintln(os.Stderr, "WARNING: GENT_TEST_XAI_KEY environment variable is not set!")
		fmt.Fprintln(os.Stderr, "Tests will fail. Please set the API key or source your .env file.")
		fmt.Fprintln(os.Stderr)
	}

	fmt.Println("Available Airline Tests:")
	fmt.Println("========================")
	for i, tc := range testCases {
		fmt.Printf("  %d. %s - %s\n", i+1, tc.Name, tc.Description)
	}
	fmt.Printf("  %d. Run all tests\n", len(testCases)+1)
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Enter test number (or 'q' to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "q" || input == "Q" {
			fmt.Println("Goodbye!")
			return nil
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > len(testCases)+1 {
			fmt.Printf("Invalid selection. Please enter a number between 1 and %d.\n\n",
				len(testCases)+1)
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\nReceived interrupt, cancelling...")
			cancel()
		}()

		config := airline.InteractiveConfig()
		if num == len(testCases)+1 {
			if err := runAllTests(ctx, testCases, config); err != nil {
				fmt.Fprintf(os.Stderr, "Error running tests: %v\n", err)
			}
		} else {
			tc := testCases[num-1]
			fmt.Printf("\nRunning test: %s\n", tc.Name)
			if err := tc.Run(ctx, os.Stdout, config); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		}

		signal.Stop(sigCh)
		cancel()

		fmt.Println("\n" + strings.Repeat("-", 60) + "\n")
	}
}

func runAllTests(
	ctx context.Context,
	testCases []airline.AirlineTestCase,
	config airline.AirlineTestConfig,
) error {
	for i, tc := range testCases {
		select {
		case <-ctx.Done():
			fmt.Println("\nTests cancelled.")
			return ctx.Err()
		default:
		}

		fmt.Printf("\n[%d/%d] Running test: %s\n", i+1, len(testCases), tc.Name)
		if err := tc.Run(ctx, os.Stdout, config); err != nil {
			fmt.Fprintf(os.Stderr, "Test %s failed: %v\n", tc.Name, err)
		} else {
			fmt.Printf("Test %s completed.\n", tc.Name)
		}
	}
	return nil
}

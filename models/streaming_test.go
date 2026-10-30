package models

import (
	"context"
	"os"
	"testing"
)

// TestStreamingBasic tests basic streaming functionality with a simple prompt.
func TestStreamingBasic(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingBasic(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingWithReasoning tests streaming with a reasoning model.
func TestStreamingWithReasoning(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingWithReasoning(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingSlowConsumer tests that streaming works with a slow consumer.
func TestStreamingSlowConsumer(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingSlowConsumer(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingNoListener tests stream completion without reading chunks.
func TestStreamingNoListener(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingNoListener(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingCancellation tests context cancellation stops the stream.
func TestStreamingCancellation(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingCancellation(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingConcurrent tests multiple concurrent streams.
func TestStreamingConcurrent(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingConcurrent(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamingResponseInfo tests response info is correctly populated.
func TestStreamingResponseInfo(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()
	if err := RunStreamingResponseInfo(ctx, os.Stdout, TestOutputConfig()); err != nil {
		t.Fatal(err)
	}
}

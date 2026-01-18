package toolchain

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// Test Types
// -----------------------------------------------------------------------------

type TimeInput struct {
	Date      time.Time `json:"date"`
	Timestamp time.Time `json:"timestamp"`
}

type DurationInput struct {
	Duration time.Duration `json:"duration"`
	Timeout  time.Duration `json:"timeout"`
}

type MixedInput struct {
	Name      string        `json:"name"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	Count     int           `json:"count"`
}

type NestedInput struct {
	Event struct {
		Name string    `json:"name"`
		When time.Time `json:"when"`
	} `json:"event"`
}

// -----------------------------------------------------------------------------
// Tests for time.Time conversion
// -----------------------------------------------------------------------------

func TestCallToolReflect_DateStringToTime(t *testing.T) {
	tool := gent.NewToolFunc(
		"date_tool",
		"Tool with date input",
		nil,
		func(ctx context.Context, input TimeInput) (string, error) {
			return input.Date.Format("2006-01-02"), nil
		},
		reflectTextFormatter,
	)

	tests := []struct {
		name     string
		dateStr  string
		expected string
	}{
		{
			name:     "date only YYYY-MM-DD",
			dateStr:  "2026-01-20",
			expected: "2026-01-20",
		},
		{
			name:     "date only different date",
			dateStr:  "2025-12-25",
			expected: "2025-12-25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"date":      tt.dateStr,
				"timestamp": "2026-01-01T00:00:00Z", // Required field
			}

			result, err := CallToolReflect(context.Background(), tool, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := result.Output.(string)
			if output != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, output)
			}
		})
	}
}

func TestCallToolReflect_TimestampStringToTime(t *testing.T) {
	tool := gent.NewToolFunc(
		"timestamp_tool",
		"Tool with timestamp input",
		nil,
		func(ctx context.Context, input TimeInput) (string, error) {
			return input.Timestamp.Format(time.RFC3339), nil
		},
		reflectTextFormatter,
	)

	tests := []struct {
		name         string
		timestampStr string
		expected     string
	}{
		{
			name:         "RFC3339",
			timestampStr: "2026-01-20T10:30:00Z",
			expected:     "2026-01-20T10:30:00Z",
		},
		{
			name:         "RFC3339 with timezone offset",
			timestampStr: "2026-01-20T10:30:00+05:00",
			expected:     "2026-01-20T10:30:00+05:00",
		},
		{
			name:         "RFC3339 with negative offset",
			timestampStr: "2026-01-20T10:30:00-08:00",
			expected:     "2026-01-20T10:30:00-08:00",
		},
		{
			name:         "ISO without timezone",
			timestampStr: "2026-01-20T10:30:00",
			expected:     "2026-01-20T10:30:00Z", // Parsed as UTC
		},
		{
			name:         "datetime with space",
			timestampStr: "2026-01-20 10:30:00",
			expected:     "2026-01-20T10:30:00Z", // Parsed as UTC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"date":      "2026-01-01", // Required field
				"timestamp": tt.timestampStr,
			}

			result, err := CallToolReflect(context.Background(), tool, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := result.Output.(string)
			if output != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, output)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Tests for time.Duration conversion
// -----------------------------------------------------------------------------

func TestCallToolReflect_DurationStringToDuration(t *testing.T) {
	tool := gent.NewToolFunc(
		"duration_tool",
		"Tool with duration input",
		nil,
		func(ctx context.Context, input DurationInput) (string, error) {
			return input.Duration.String(), nil
		},
		reflectTextFormatter,
	)

	tests := []struct {
		name        string
		durationStr string
		expected    string
	}{
		{
			name:        "hours and minutes",
			durationStr: "1h30m",
			expected:    "1h30m0s",
		},
		{
			name:        "hours minutes seconds",
			durationStr: "2h45m30s",
			expected:    "2h45m30s",
		},
		{
			name:        "minutes only",
			durationStr: "30m",
			expected:    "30m0s",
		},
		{
			name:        "seconds only",
			durationStr: "45s",
			expected:    "45s",
		},
		{
			name:        "milliseconds",
			durationStr: "500ms",
			expected:    "500ms",
		},
		{
			name:        "complex duration",
			durationStr: "1h2m3s4ms",
			expected:    "1h2m3.004s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"duration": tt.durationStr,
				"timeout":  "1s", // Required field
			}

			result, err := CallToolReflect(context.Background(), tool, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := result.Output.(string)
			if output != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, output)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Tests for mixed types
// -----------------------------------------------------------------------------

func TestCallToolReflect_MixedTypes(t *testing.T) {
	tool := gent.NewToolFunc(
		"mixed_tool",
		"Tool with mixed input types",
		nil,
		func(ctx context.Context, input MixedInput) (string, error) {
			return input.Name + "|" +
				input.StartTime.Format("2006-01-02") + "|" +
				input.Duration.String(), nil
		},
		reflectTextFormatter,
	)

	args := map[string]any{
		"name":       "test-event",
		"start_time": "2026-06-15",
		"duration":   "2h30m",
		"count":      42,
	}

	result, err := CallToolReflect(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "test-event|2026-06-15|2h30m0s"
	output := result.Output.(string)
	if output != expected {
		t.Errorf("expected '%s', got '%s'", expected, output)
	}
}

// -----------------------------------------------------------------------------
// Tests for map[string]any input (no conversion needed)
// -----------------------------------------------------------------------------

func TestCallToolReflect_MapStringAny_NoConversion(t *testing.T) {
	tool := gent.NewToolFunc(
		"map_tool",
		"Tool with map[string]any input",
		nil,
		func(ctx context.Context, input map[string]any) (string, error) {
			// When input is map[string]any, values stay as intermediary types
			dateVal := input["date"]
			_, isString := dateVal.(string)
			if !isString {
				return "not-a-string", nil
			}
			return "is-a-string", nil
		},
		reflectTextFormatter,
	)

	args := map[string]any{
		"date": "2026-01-20",
	}

	result, err := CallToolReflect(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// For map[string]any input, date should remain as string (no conversion)
	output := result.Output.(string)
	if output != "is-a-string" {
		t.Errorf("expected date to be string when input type is map[string]any")
	}
}

// -----------------------------------------------------------------------------
// Tests for YAML-parsed time.Time values
// -----------------------------------------------------------------------------

func TestCallToolReflect_YAMLParsedTimeToTime(t *testing.T) {
	tool := gent.NewToolFunc(
		"yaml_time_tool",
		"Tool with time input",
		nil,
		func(ctx context.Context, input TimeInput) (string, error) {
			return input.Date.Format("2006-01-02"), nil
		},
		reflectTextFormatter,
	)

	// Simulate YAML-parsed time.Time value
	parsedTime := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	args := map[string]any{
		"date":      parsedTime, // Already time.Time from YAML
		"timestamp": parsedTime,
	}

	result, err := CallToolReflect(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "2026-01-20"
	output := result.Output.(string)
	if output != expected {
		t.Errorf("expected '%s', got '%s'", expected, output)
	}
}

// -----------------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------------

func reflectTextFormatter(s string) []gent.ContentPart {
	return []gent.ContentPart{llms.TextContent{Text: s}}
}

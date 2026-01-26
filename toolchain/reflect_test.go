package toolchain

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	type input struct {
		dateStr string
	}

	type expected struct {
		result string
		err    error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "date only YYYY-MM-DD",
			input:    input{dateStr: "2026-01-20"},
			expected: expected{result: "2026-01-20", err: nil},
		},
		{
			name:     "date only different date",
			input:    input{dateStr: "2025-12-25"},
			expected: expected{result: "2025-12-25", err: nil},
		},
	}

	tool := gent.NewToolFunc(
		"date_tool",
		"Tool with date input",
		nil,
		func(ctx context.Context, input TimeInput) (string, error) {
			return input.Date.Format("2006-01-02"), nil
		},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"date":      tt.input.dateStr,
				"timestamp": "2026-01-01T00:00:00Z",
			}

			result, err := CallToolReflect(context.Background(), tool, args)

			assert.ErrorIs(t, err, tt.expected.err)
			require.NotNil(t, result)
			output := result.Text.(string)
			assert.Equal(t, tt.expected.result, output)
		})
	}
}

func TestCallToolReflect_TimestampStringToTime(t *testing.T) {
	type input struct {
		timestampStr string
	}

	type expected struct {
		result string
		err    error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "RFC3339",
			input:    input{timestampStr: "2026-01-20T10:30:00Z"},
			expected: expected{result: "2026-01-20T10:30:00Z", err: nil},
		},
		{
			name:     "RFC3339 with timezone offset",
			input:    input{timestampStr: "2026-01-20T10:30:00+05:00"},
			expected: expected{result: "2026-01-20T10:30:00+05:00", err: nil},
		},
		{
			name:     "RFC3339 with negative offset",
			input:    input{timestampStr: "2026-01-20T10:30:00-08:00"},
			expected: expected{result: "2026-01-20T10:30:00-08:00", err: nil},
		},
		{
			name:     "ISO without timezone",
			input:    input{timestampStr: "2026-01-20T10:30:00"},
			expected: expected{result: "2026-01-20T10:30:00Z", err: nil},
		},
		{
			name:     "datetime with space",
			input:    input{timestampStr: "2026-01-20 10:30:00"},
			expected: expected{result: "2026-01-20T10:30:00Z", err: nil},
		},
	}

	tool := gent.NewToolFunc(
		"timestamp_tool",
		"Tool with timestamp input",
		nil,
		func(ctx context.Context, input TimeInput) (string, error) {
			return input.Timestamp.Format(time.RFC3339), nil
		},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"date":      "2026-01-01",
				"timestamp": tt.input.timestampStr,
			}

			result, err := CallToolReflect(context.Background(), tool, args)

			assert.ErrorIs(t, err, tt.expected.err)
			require.NotNil(t, result)
			output := result.Text.(string)
			assert.Equal(t, tt.expected.result, output)
		})
	}
}

// -----------------------------------------------------------------------------
// Tests for time.Duration conversion
// -----------------------------------------------------------------------------

func TestCallToolReflect_DurationStringToDuration(t *testing.T) {
	type input struct {
		durationStr string
	}

	type expected struct {
		result string
		err    error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "hours and minutes",
			input:    input{durationStr: "1h30m"},
			expected: expected{result: "1h30m0s", err: nil},
		},
		{
			name:     "hours minutes seconds",
			input:    input{durationStr: "2h45m30s"},
			expected: expected{result: "2h45m30s", err: nil},
		},
		{
			name:     "minutes only",
			input:    input{durationStr: "30m"},
			expected: expected{result: "30m0s", err: nil},
		},
		{
			name:     "seconds only",
			input:    input{durationStr: "45s"},
			expected: expected{result: "45s", err: nil},
		},
		{
			name:     "milliseconds",
			input:    input{durationStr: "500ms"},
			expected: expected{result: "500ms", err: nil},
		},
		{
			name:     "complex duration",
			input:    input{durationStr: "1h2m3s4ms"},
			expected: expected{result: "1h2m3.004s", err: nil},
		},
	}

	tool := gent.NewToolFunc(
		"duration_tool",
		"Tool with duration input",
		nil,
		func(ctx context.Context, input DurationInput) (string, error) {
			return input.Duration.String(), nil
		},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"duration": tt.input.durationStr,
				"timeout":  "1s",
			}

			result, err := CallToolReflect(context.Background(), tool, args)

			assert.ErrorIs(t, err, tt.expected.err)
			require.NotNil(t, result)
			output := result.Text.(string)
			assert.Equal(t, tt.expected.result, output)
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
	)

	args := map[string]any{
		"name":       "test-event",
		"start_time": "2026-06-15",
		"duration":   "2h30m",
		"count":      42,
	}

	result, err := CallToolReflect(context.Background(), tool, args)

	require.NoError(t, err)
	expected := "test-event|2026-06-15|2h30m0s"
	output := result.Text.(string)
	assert.Equal(t, expected, output)
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
			dateVal := input["date"]
			_, isString := dateVal.(string)
			if !isString {
				return "not-a-string", nil
			}
			return "is-a-string", nil
		},
	)

	args := map[string]any{
		"date": "2026-01-20",
	}

	result, err := CallToolReflect(context.Background(), tool, args)

	require.NoError(t, err)
	output := result.Text.(string)
	assert.Equal(t, "is-a-string", output)
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
	)

	parsedTime := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	args := map[string]any{
		"date":      parsedTime,
		"timestamp": parsedTime,
	}

	result, err := CallToolReflect(context.Background(), tool, args)

	require.NoError(t, err)
	expected := "2026-01-20"
	output := result.Text.(string)
	assert.Equal(t, expected, output)
}


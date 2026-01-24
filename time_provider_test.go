package gent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTimeProvider_Now(t *testing.T) {
	tp := NewDefaultTimeProvider()

	before := time.Now()
	result := tp.Now()
	after := time.Now()

	assert.False(t, result.Before(before), "Now() should not be before test start")
	assert.False(t, result.After(after), "Now() should not be after test end")
}

func TestDefaultTimeProvider_Today(t *testing.T) {
	tp := NewDefaultTimeProvider()
	result := tp.Today()

	expected := time.Now().Format("2006-01-02")
	assert.Equal(t, expected, result)
}

func TestDefaultTimeProvider_Weekday(t *testing.T) {
	tp := NewDefaultTimeProvider()
	result := tp.Weekday()

	expected := time.Now().Weekday().String()
	assert.Equal(t, expected, result)
}

func TestDefaultTimeProvider_Format(t *testing.T) {
	tp := NewDefaultTimeProvider()
	layout := "2006-01-02 15:04"

	before := time.Now().Format(layout)
	result := tp.Format(layout)
	after := time.Now().Format(layout)

	// Result should be between before and after (or equal to one)
	valid := result == before || result == after || (result >= before && result <= after)
	assert.True(t, valid, "Format() = %q, expected between %q and %q", result, before, after)
}

func TestRelativeDate(t *testing.T) {
	type input struct {
		now    time.Time
		target time.Time
	}

	type expected struct {
		result string
	}

	now := time.Date(2025, 2, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "today",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 15, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "today"},
		},
		{
			name: "tomorrow",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 16, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "tomorrow"},
		},
		{
			name: "yesterday",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 14, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "yesterday"},
		},
		{
			name: "in 2 days",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 17, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "in 2 days"},
		},
		{
			name: "in 1 day edge case",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 16, 23, 59, 0, 0, time.UTC),
			},
			expected: expected{result: "tomorrow"},
		},
		{
			name: "2 days ago",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 13, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "2 days ago"},
		},
		{
			name: "1 day ago edge case",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "yesterday"},
		},
		{
			name: "in 7 days",
			input: input{
				now:    now,
				target: time.Date(2025, 2, 22, 8, 0, 0, 0, time.UTC),
			},
			expected: expected{result: "in 7 days"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := relativeDate(tt.input.now, tt.input.target)
			assert.Equal(t, tt.expected.result, result)
		})
	}
}

func TestMockTimeProvider(t *testing.T) {
	type input struct {
		fixedTime time.Time
		newTime   time.Time
	}

	type expected struct {
		initialToday   string
		initialWeekday string
		updatedToday   string
		updatedWeekday string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "mock time provider returns fixed time and updates correctly",
			input: input{
				fixedTime: time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC),
				newTime:   time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC),
			},
			expected: expected{
				initialToday:   "2025-06-15",
				initialWeekday: "Sunday",
				updatedToday:   "2025-12-25",
				updatedWeekday: "Thursday",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := NewMockTimeProvider(tt.input.fixedTime)

			assert.True(t, tp.Now().Equal(tt.input.fixedTime))
			assert.Equal(t, tt.expected.initialToday, tp.Today())
			assert.Equal(t, tt.expected.initialWeekday, tp.Weekday())

			tp.SetTime(tt.input.newTime)

			assert.True(t, tp.Now().Equal(tt.input.newTime))
			assert.Equal(t, tt.expected.updatedToday, tp.Today())
			assert.Equal(t, tt.expected.updatedWeekday, tp.Weekday())
		})
	}
}

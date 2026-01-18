package gent

import (
	"testing"
	"time"
)

func TestDefaultTimeProvider_Now(t *testing.T) {
	tp := NewDefaultTimeProvider()

	before := time.Now()
	result := tp.Now()
	after := time.Now()

	if result.Before(before) || result.After(after) {
		t.Errorf("Now() returned time outside expected range")
	}
}

func TestDefaultTimeProvider_Today(t *testing.T) {
	tp := NewDefaultTimeProvider()
	result := tp.Today()

	expected := time.Now().Format("2006-01-02")
	if result != expected {
		t.Errorf("Today() = %q, want %q", result, expected)
	}
}

func TestDefaultTimeProvider_Weekday(t *testing.T) {
	tp := NewDefaultTimeProvider()
	result := tp.Weekday()

	expected := time.Now().Weekday().String()
	if result != expected {
		t.Errorf("Weekday() = %q, want %q", result, expected)
	}
}

func TestDefaultTimeProvider_Format(t *testing.T) {
	tp := NewDefaultTimeProvider()
	layout := "2006-01-02 15:04"

	before := time.Now().Format(layout)
	result := tp.Format(layout)
	after := time.Now().Format(layout)

	// Result should be between before and after (or equal to one)
	if result != before && result != after {
		// Allow for minute rollover
		if result < before || result > after {
			t.Errorf("Format() = %q, expected between %q and %q", result, before, after)
		}
	}
}

func TestRelativeDate(t *testing.T) {
	now := time.Date(2025, 2, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		target   time.Time
		expected string
	}{
		{
			name:     "today",
			target:   time.Date(2025, 2, 15, 8, 0, 0, 0, time.UTC),
			expected: "today",
		},
		{
			name:     "tomorrow",
			target:   time.Date(2025, 2, 16, 8, 0, 0, 0, time.UTC),
			expected: "tomorrow",
		},
		{
			name:     "yesterday",
			target:   time.Date(2025, 2, 14, 8, 0, 0, 0, time.UTC),
			expected: "yesterday",
		},
		{
			name:     "in 2 days",
			target:   time.Date(2025, 2, 17, 8, 0, 0, 0, time.UTC),
			expected: "in 2 days",
		},
		{
			name:     "in 1 day (edge case)",
			target:   time.Date(2025, 2, 16, 23, 59, 0, 0, time.UTC),
			expected: "tomorrow",
		},
		{
			name:     "2 days ago",
			target:   time.Date(2025, 2, 13, 8, 0, 0, 0, time.UTC),
			expected: "2 days ago",
		},
		{
			name:     "1 day ago (edge case)",
			target:   time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC),
			expected: "yesterday",
		},
		{
			name:     "in 7 days",
			target:   time.Date(2025, 2, 22, 8, 0, 0, 0, time.UTC),
			expected: "in 7 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := relativeDate(now, tt.target)
			if result != tt.expected {
				t.Errorf("relativeDate() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMockTimeProvider(t *testing.T) {
	fixedTime := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	tp := NewMockTimeProvider(fixedTime)

	if !tp.Now().Equal(fixedTime) {
		t.Errorf("Now() = %v, want %v", tp.Now(), fixedTime)
	}

	if tp.Today() != "2025-06-15" {
		t.Errorf("Today() = %q, want %q", tp.Today(), "2025-06-15")
	}

	if tp.Weekday() != "Sunday" {
		t.Errorf("Weekday() = %q, want %q", tp.Weekday(), "Sunday")
	}

	// Update time
	newTime := time.Date(2025, 12, 25, 10, 0, 0, 0, time.UTC)
	tp.SetTime(newTime)

	if !tp.Now().Equal(newTime) {
		t.Errorf("Now() after SetTime = %v, want %v", tp.Now(), newTime)
	}

	if tp.Today() != "2025-12-25" {
		t.Errorf("Today() after SetTime = %q, want %q", tp.Today(), "2025-12-25")
	}

	if tp.Weekday() != "Thursday" {
		t.Errorf("Weekday() after SetTime = %q, want %q", tp.Weekday(), "Thursday")
	}
}

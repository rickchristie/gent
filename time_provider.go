package gent

import (
	"fmt"
	"time"
)

// TimeProvider provides time-related functionality for agents.
// It allows injecting custom time sources for testing and provides
// convenient formatting methods for use in prompts.
//
// All methods are accessible in templates via the .Time field:
//
//	Today is {{.Time.Today}} ({{.Time.Weekday}})
//	Current time: {{.Time.Format "3:04 PM"}}
type TimeProvider interface {
	// Now returns the current time.
	//
	// Template: {{.Time.Now}}
	// Output: 2025-02-15 14:30:00 +0000 UTC
	Now() time.Time

	// Today returns today's date as a string (YYYY-MM-DD).
	//
	// Template: {{.Time.Today}}
	// Output: 2025-02-15
	Today() string

	// Date returns the date portion of the current time formatted as specified.
	//
	// Template: {{.Time.Date "Jan 2, 2006"}}
	// Output: Feb 15, 2025
	//
	// Template: {{.Time.Date "Monday, January 2, 2006"}}
	// Output: Saturday, February 15, 2025
	Date(layout string) string

	// Time returns the time portion of the current time formatted as specified.
	//
	// Template: {{.Time.Time "3:04 PM"}}
	// Output: 2:30 PM
	//
	// Template: {{.Time.Time "15:04:05"}}
	// Output: 14:30:00
	Time(layout string) string

	// Format returns the current time formatted with the given layout.
	// Uses Go's time layout format.
	//
	// Template: {{.Time.Format "2006-01-02 15:04:05"}}
	// Output: 2025-02-15 14:30:00
	//
	// Template: {{.Time.Format "Mon, 02 Jan 2006"}}
	// Output: Sat, 15 Feb 2025
	Format(layout string) string

	// Weekday returns the current day of the week (e.g., "Monday").
	//
	// Template: {{.Time.Weekday}}
	// Output: Saturday
	Weekday() string

	// RelativeDate returns a human-readable relative date.
	// Examples: "today", "tomorrow", "yesterday", "in 3 days", "3 days ago"
	//
	// Template: {{.Time.RelativeDate $someTime}}
	// Output: tomorrow
	RelativeDate(t time.Time) string
}

// DefaultTimeProvider is the standard TimeProvider using the system clock.
type DefaultTimeProvider struct{}

// NewDefaultTimeProvider creates a new DefaultTimeProvider.
func NewDefaultTimeProvider() *DefaultTimeProvider {
	return &DefaultTimeProvider{}
}

// Now returns the current system time.
func (p *DefaultTimeProvider) Now() time.Time {
	return time.Now()
}

// Today returns today's date as YYYY-MM-DD.
func (p *DefaultTimeProvider) Today() string {
	return p.Now().Format("2006-01-02")
}

// Date returns the date formatted with the given layout.
func (p *DefaultTimeProvider) Date(layout string) string {
	return p.Now().Format(layout)
}

// Time returns the time formatted with the given layout.
func (p *DefaultTimeProvider) Time(layout string) string {
	return p.Now().Format(layout)
}

// Format returns the current time formatted with the given layout.
func (p *DefaultTimeProvider) Format(layout string) string {
	return p.Now().Format(layout)
}

// Weekday returns the current day of the week.
func (p *DefaultTimeProvider) Weekday() string {
	return p.Now().Weekday().String()
}

// RelativeDate returns a human-readable relative date description.
func (p *DefaultTimeProvider) RelativeDate(t time.Time) string {
	return relativeDate(p.Now(), t)
}

// relativeDate computes the relative date string between now and t.
func relativeDate(now, t time.Time) string {
	// Truncate to start of day for comparison
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tDate := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	days := int(tDate.Sub(nowDate).Hours() / 24)

	switch days {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	case -1:
		return "yesterday"
	default:
		if days > 1 {
			return formatDays(days, "in %d day", "in %d days")
		}
		return formatDays(-days, "%d day ago", "%d days ago")
	}
}

func formatDays(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf(singular, n)
	}
	return fmt.Sprintf(plural, n)
}

// MockTimeProvider is a TimeProvider that returns a fixed time.
// Useful for testing time-dependent functionality.
type MockTimeProvider struct {
	fixedTime time.Time
}

// NewMockTimeProvider creates a MockTimeProvider with the given fixed time.
func NewMockTimeProvider(t time.Time) *MockTimeProvider {
	return &MockTimeProvider{fixedTime: t}
}

// SetTime updates the fixed time returned by Now().
func (m *MockTimeProvider) SetTime(t time.Time) {
	m.fixedTime = t
}

// Now returns the fixed time.
func (m *MockTimeProvider) Now() time.Time {
	return m.fixedTime
}

// Today returns the fixed date as YYYY-MM-DD.
func (m *MockTimeProvider) Today() string {
	return m.fixedTime.Format("2006-01-02")
}

// Date returns the fixed date formatted with the given layout.
func (m *MockTimeProvider) Date(layout string) string {
	return m.fixedTime.Format(layout)
}

// Time returns the fixed time formatted with the given layout.
func (m *MockTimeProvider) Time(layout string) string {
	return m.fixedTime.Format(layout)
}

// Format returns the fixed time formatted with the given layout.
func (m *MockTimeProvider) Format(layout string) string {
	return m.fixedTime.Format(layout)
}

// Weekday returns the day of the week for the fixed time.
func (m *MockTimeProvider) Weekday() string {
	return m.fixedTime.Weekday().String()
}

// RelativeDate returns a human-readable relative date description.
func (m *MockTimeProvider) RelativeDate(t time.Time) string {
	return relativeDate(m.fixedTime, t)
}

// Compile-time check that MockTimeProvider implements TimeProvider.
var _ TimeProvider = (*MockTimeProvider)(nil)

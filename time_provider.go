package gent

import (
	"fmt"
	"time"
)

// TimeProvider supplies time and standard formats to application code. Use it when scheduling,
// deadlines, or business hours need a fixed test clock or a specific time zone. The caller composes
// prompt text; Gent does not inject this interface into an agent or expand a prompt template.
//
//	clock := gent.TimeProviderFunc(func() time.Time { return time.Now().In(userLocation) })
//	task := &gent.Task{Messages: []*gent.ModelMessage{
//	    gent.TextParts(gent.ChatMessageTypeSystem, "Today is " + clock.Today() + "."),
//	    gent.TextParts(gent.ChatMessageTypeHuman, "Check tomorrow's bookings."),
//	}}
//
// Use NewMockTimeProvider with a fixed time for repeatable application tests. Implement
// TimeProvider
// when the formats must also change; TimeProviderFunc uses the standard formats.
type TimeProvider interface {
	// Now returns the current time.
	//
	// Output: 2025-02-15 14:30:00 +0000 UTC
	Now() time.Time

	// Today returns today's date as a string (YYYY-MM-DD).
	//
	// Output: 2025-02-15
	Today() string

	// Date returns the date portion of the current time formatted as specified.
	//
	// Output: Feb 15, 2025
	//
	// Output: Saturday, February 15, 2025
	Date(layout string) string

	// Time returns the time portion of the current time formatted as specified.
	//
	// Output: 2:30 PM
	//
	// Output: 14:30:00
	Time(layout string) string

	// Format returns the current time formatted with the given layout.
	// Uses Go's time layout format.
	//
	// Output: 2025-02-15 14:30:00
	//
	// Output: Sat, 15 Feb 2025
	Format(layout string) string

	// Weekday returns the current day of the week (e.g., "Monday").
	//
	// Output: Saturday
	Weekday() string

	// RelativeDate returns a human-readable relative date.
	// Examples: "today", "tomorrow", "yesterday", "in 3 days", "3 days ago"
	//
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

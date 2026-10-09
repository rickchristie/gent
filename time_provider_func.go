package gent

import "time"

// TimeProviderFunc uses a clock function with the standard prompt time formats.
// The function is required. Use TimeProvider when custom formats are also needed.
type TimeProviderFunc func() time.Time

func (clock TimeProviderFunc) Now() time.Time { return clock() }

func (clock TimeProviderFunc) Today() string { return clock().Format("2006-01-02") }

func (clock TimeProviderFunc) Date(layout string) string { return clock().Format(layout) }

func (clock TimeProviderFunc) Time(layout string) string { return clock().Format(layout) }

func (clock TimeProviderFunc) Format(layout string) string { return clock().Format(layout) }

func (clock TimeProviderFunc) Weekday() string { return clock().Weekday().String() }

func (clock TimeProviderFunc) RelativeDate(target time.Time) string {
	return relativeDate(clock(), target)
}

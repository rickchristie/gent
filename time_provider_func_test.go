package gent_test

import (
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestTimeProviderFuncKeepsTimeFormats(t *testing.T) {
	type input struct {
		now    time.Time
		target time.Time
	}
	type expected struct {
		now      time.Time
		today    string
		date     string
		clock    string
		format   string
		weekday  string
		relative string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	zone := time.FixedZone("WIB", 7*60*60)
	first := time.Date(2026, 10, 8, 23, 59, 0, 0, zone)
	next := first.Add(time.Minute)
	cases := []testCase{
		{"before midnight", input{first, next}, expected{first, "2026-10-08", "08 Oct 2026",
			"23:59 WIB", "2026-10-08T23:59:00+07:00", "Thursday", "tomorrow"}},
		{"after the clock changes", input{next, first}, expected{next, "2026-10-09", "09 Oct 2026",
			"00:00 WIB", "2026-10-09T00:00:00+07:00", "Friday", "yesterday"}},
	}
	var now time.Time
	provider := gent.TimeProviderFunc(func() time.Time { return now })
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			now = test.input.now
			actual := expected{provider.Now(), provider.Today(), provider.Date("02 Jan 2006"),
				provider.Time("15:04 MST"), provider.Format(time.RFC3339), provider.Weekday(),
				provider.RelativeDate(test.input.target)}
			assert.Equal(t, test.expected, actual)
		})
	}
}

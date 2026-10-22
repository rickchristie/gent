//go:build cgo

package search

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Keep downloads in explicit setup commands, outside normal test execution.
	os.Exit(m.Run())
}

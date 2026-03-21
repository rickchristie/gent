//go:build cgo

package search

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	ensureAllTestModels(m)
	os.Exit(m.Run())
}

//go:build gent_review && gent_review_native_crash && cgo

package gent_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// This separate tag keeps a known native process crash out of the other review checks.
func TestReviewEmbedderCloseDuringTokenCount(t *testing.T) {
	embedder, _ := reviewEmbedder(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				embedder.TokenCount(strings.Repeat("x ", 1000))
			}
		}()
	}
	close(start)
	time.Sleep(time.Millisecond)
	assert.NoError(t, embedder.Close())
	wg.Wait()
}

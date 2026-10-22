package toolchain

import (
	"context"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestRegexToolSearcherConcurrentIndexAndSearch(t *testing.T) {
	engine := NewRegexToolSearcher()
	tools := []gent.IndexableTool{&mockIndexableTool{name: "lookup", description: "Lookup a name"}}
	assert.NoError(t, engine.IndexAll(tools))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 64; i++ {
				assert.NoError(t, engine.IndexAll(tools))
				names, err := engine.Search(context.Background(), "lookup")
				assert.NoError(t, err)
				assert.Equal(t, []string{"lookup"}, names)
			}
		}()
	}
	close(start)
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	names, err := engine.Search(ctx, "lookup")
	assert.Nil(t, names)
	assert.ErrorIs(t, err, context.Canceled)
}

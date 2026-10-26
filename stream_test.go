package gent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamBufferCloseStopsBlockedDrainLoop(t *testing.T) {
	stream := newStreamBuffer()
	chunks := stream.Chunks()

	require.True(t, stream.SendMeta(&MetaContent{Type: MetaContentIntermediary, Text: "first"}))
	require.True(t, stream.SendMeta(&MetaContent{Type: MetaContentIntermediary, Text: "second"}))
	require.Eventually(t, func() bool {
		return len(chunks) == 1
	}, time.Second, 10*time.Millisecond)

	stream.Close()

	select {
	case <-stream.drainDone:
	case <-time.After(time.Second):
		t.Fatal("expected Close to stop the blocked drain goroutine")
	}

	chunk, ok := <-chunks
	assert.Equal(t, StreamChunk{
		MetaContent: []*MetaContent{
			{
				Type: MetaContentIntermediary, Text: "first",
			},
		},
	}, chunk)
	assert.True(t, ok)

	chunk, ok = <-chunks
	assert.Equal(t, StreamChunk{}, chunk)
	assert.False(t, ok)
}

package trace

import (
	"runtime"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestLiveDeliveryCapacityAfterReceive(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	type input struct{ capacity int }
	type expected struct {
		numbers []uint64
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"one slot", input{1}, expected{[]uint64{1, 2}, nil}},
		{"two slots", input{2}, expected{[]uint64{1, 2}, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			seq := newTestSequencer(t, "delivery", Config{IncludeChunkText: true})
			defer seq.Close()
			sub, err := seq.SubscribeWithLimits(gent.DeliveryConfig{MaxEvents: test.input.capacity})
			if !assert.NoError(t, err) {
				return
			}
			defer sub.Close()
			chunk := gent.StreamChunk{MetaContent: []*gent.MetaContent{{
				Type: gent.MetaContentIntermediary, Content: []gent.ContentPart{
					gent.TextContent{Text: "Provisional text."}},
			}}}
			recordTestValue(t, seq, "model_stream_chunk", chunk)
			runtime.Gosched()
			first := receiveLive(t, sub.Events())
			recordTestValue(t, seq, "model_stream_chunk", chunk)
			seq.Close()
			numbers := []uint64{first.EventNumber}
			for event := range sub.Events() {
				numbers = append(numbers, event.EventNumber)
			}
			<-sub.Done()
			assert.Equal(t, test.expected, expected{numbers, sub.Err()})
		})
	}
}

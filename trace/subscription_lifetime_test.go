package trace

import (
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionCloseAndUnsubscribe(t *testing.T) {
	type input struct {
		bufferSize int
		events     int
		order      string
	}
	type expected struct {
		event *Event
		open  bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	tests := []testCase{
		{"default close first", input{0, 8, "close"}, expected{}},
		{"default unsubscribe first", input{0, 8, "unsubscribe"}, expected{}},
		{"default simultaneous", input{0, 8, "concurrent"}, expected{}},
		{"default empty", input{0, 0, "concurrent"}, expected{}},
		{"bounded close first", input{8, 8, "close"}, expected{}},
		{"bounded unsubscribe first", input{8, 8, "unsubscribe"}, expected{}},
		{"bounded simultaneous", input{8, 8, "concurrent"}, expected{}},
		{"bounded empty", input{8, 0, "concurrent"}, expected{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seq := newTestSequencer(t, "run", Config{})
			ch, unsubscribe := seq.SubscribeWithConfig(SubscribeConfig{BufferSize: tc.input.bufferSize})
			defer unsubscribe()
			for i := 0; i < tc.input.events; i++ {
				recordTestValue(t, seq, "common", &gent.CommonEvent{})
			}
			switch tc.input.order {
			case "close":
				seq.Close()
				unsubscribe()
			case "unsubscribe":
				unsubscribe()
				seq.Close()
			case "concurrent":
				var wg sync.WaitGroup
				start := make(chan struct{})
				for _, stop := range []func(){seq.Close, unsubscribe, unsubscribe} {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						stop()
					}()
				}
				close(start)
				wg.Wait()
			}
			select {
			case event, open := <-ch:
				assert.Equal(t, tc.expected, expected{event, open})
			case <-time.After(time.Second):
				t.Fatal("unsubscribe did not close the subscription")
			}
		})
	}
}

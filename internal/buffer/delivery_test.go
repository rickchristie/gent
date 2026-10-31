package buffer_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/rickchristie/gent/internal/buffer"
	"github.com/stretchr/testify/assert"
)

func TestDeliveryCapacityAfterReceive(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	type input struct{ events, bytes int }
	type expected struct {
		values []string
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{[]string{"first", "later"}, nil}
	for _, test := range []testCase{
		{"one event", input{1, 100}, want},
		{"two events", input{2, 100}, want},
		{"one event of bytes", input{10, 5}, want},
		{"two events of bytes", input{10, 10}, want},
	} {
		t.Run(test.name, func(t *testing.T) {
			queue := buffer.NewDelivery[string](test.input.events, test.input.bytes, time.Second)
			defer func() { queue.Discard(); <-queue.Done() }()
			assert.True(t, queue.Send("first", 5))
			// Let the worker block on delivery. The consumer then sends again before the worker
			// can run the bookkeeping after that handoff.
			runtime.Gosched()
			values := []string{<-queue.Receive()}
			assert.True(t, queue.Send("later", 5))
			queue.Close()
			for value := range queue.Receive() {
				values = append(values, value)
			}
			<-queue.Done()
			assert.Equal(t, test.expected, expected{values, queue.Err()})
		})
	}
}

// Measure repeated consumer handoffs as well as queue acceptance. A second slot lets the same
// benchmark measure implementations that release the previous slot after the consumer resumes.
func BenchmarkDelivery(b *testing.B) {
	queue := buffer.NewDelivery[int](2, 16, time.Second)
	defer func() { queue.Discard(); <-queue.Done() }()
	b.ReportAllocs()
	for b.Loop() {
		if !queue.Send(1, 8) {
			b.Fatal(queue.Err())
		}
		if value := <-queue.Receive(); value != 1 {
			b.Fatalf("received %d, want 1", value)
		}
	}
}

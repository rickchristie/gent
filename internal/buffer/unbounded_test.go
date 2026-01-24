package buffer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnbounded_BasicSendReceive(t *testing.T) {
	type input struct {
		items []int
	}

	type expected struct {
		received []int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "sends and receives items in order",
			input:    input{items: []int{1, 2, 3}},
			expected: expected{received: []int{1, 2, 3}},
		},
		{
			name:     "empty buffer",
			input:    input{items: []int{}},
			expected: expected{received: []int{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewUnbounded[int]()

			for _, item := range tt.input.items {
				buf.Send(item)
			}
			buf.Close()

			var received []int
			for item := range buf.Receive() {
				received = append(received, item)
			}

			if len(tt.expected.received) == 0 {
				assert.Empty(t, received)
			} else {
				assert.Equal(t, tt.expected.received, received)
			}
		})
	}
}

func TestUnbounded_SendNeverBlocks(t *testing.T) {
	buf := NewUnbounded[int]()

	done := make(chan struct{})
	go func() {
		for i := range 10000 {
			buf.Send(i)
		}
		close(done)
	}()

	select {
	case <-done:
		// Success - Send didn't block
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked when it shouldn't")
	}

	buf.Close()

	count := 0
	for range buf.Receive() {
		count++
	}
	assert.Equal(t, 10000, count)
}

func TestUnbounded_ConcurrentSend(t *testing.T) {
	buf := NewUnbounded[int]()
	numSenders := 10
	itemsPerSender := 1000

	var wg sync.WaitGroup
	wg.Add(numSenders)

	for i := range numSenders {
		go func(senderID int) {
			defer wg.Done()
			for j := range itemsPerSender {
				buf.Send(senderID*itemsPerSender + j)
			}
		}(i)
	}

	wg.Wait()
	buf.Close()

	count := 0
	for range buf.Receive() {
		count++
	}

	expected := numSenders * itemsPerSender
	assert.Equal(t, expected, count)
}

func TestUnbounded_SendAfterClose(t *testing.T) {
	buf := NewUnbounded[int]()
	buf.Send(1)
	buf.Close()
	buf.Send(2) // Should be ignored

	var received []int
	for item := range buf.Receive() {
		received = append(received, item)
	}

	assert.Equal(t, []int{1}, received)
}

func TestUnbounded_DoubleClose(t *testing.T) {
	buf := NewUnbounded[int]()
	buf.Close()
	buf.Close() // Should not panic

	_, ok := <-buf.Receive()
	assert.False(t, ok, "expected channel to be closed")
}

func TestUnbounded_EmptyClose(t *testing.T) {
	buf := NewUnbounded[int]()
	buf.Close()

	select {
	case _, ok := <-buf.Receive():
		assert.False(t, ok, "expected channel to be closed")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestUnbounded_SlowConsumer(t *testing.T) {
	buf := NewUnbounded[int]()

	received := make(chan int, 100)
	go func() {
		for item := range buf.Receive() {
			time.Sleep(time.Millisecond) // Slow consumption
			received <- item
		}
		close(received)
	}()

	for i := range 100 {
		buf.Send(i)
	}
	buf.Close()

	count := 0
	for range received {
		count++
	}
	assert.Equal(t, 100, count)
}

func TestUnbounded_LenAndIsClosed(t *testing.T) {
	buf := NewUnbounded[int]()

	assert.False(t, buf.IsClosed(), "expected buffer to not be closed initially")

	buf.Send(1)
	buf.Send(2)

	buf.Close()

	assert.True(t, buf.IsClosed(), "expected buffer to be closed after Close()")
}

func TestUnbounded_WithStruct(t *testing.T) {
	type TestStruct struct {
		ID   int
		Name string
	}

	type input struct {
		items []TestStruct
	}

	type expected struct {
		items []TestStruct
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "struct items are sent and received correctly",
			input: input{
				items: []TestStruct{
					{ID: 1, Name: "one"},
					{ID: 2, Name: "two"},
				},
			},
			expected: expected{
				items: []TestStruct{
					{ID: 1, Name: "one"},
					{ID: 2, Name: "two"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewUnbounded[TestStruct]()

			for _, item := range tt.input.items {
				buf.Send(item)
			}
			buf.Close()

			var received []TestStruct
			for item := range buf.Receive() {
				received = append(received, item)
			}

			require.Len(t, received, len(tt.expected.items))
			for i, item := range received {
				assert.Equal(t, tt.expected.items[i], item)
			}
		})
	}
}

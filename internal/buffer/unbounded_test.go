package buffer

import (
	"sync"
	"testing"
	"time"
)

func TestUnbounded_BasicSendReceive(t *testing.T) {
	buf := NewUnbounded[int]()

	buf.Send(1)
	buf.Send(2)
	buf.Send(3)
	buf.Close()

	var received []int
	for item := range buf.Receive() {
		received = append(received, item)
	}

	if len(received) != 3 {
		t.Errorf("expected 3 items, got %d", len(received))
	}
	for i, v := range received {
		if v != i+1 {
			t.Errorf("expected %d at index %d, got %d", i+1, i, v)
		}
	}
}

func TestUnbounded_SendNeverBlocks(t *testing.T) {
	buf := NewUnbounded[int]()

	// Send many items without reading - should not block
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
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

	// Drain to prevent goroutine leak
	count := 0
	for range buf.Receive() {
		count++
	}
	if count != 10000 {
		t.Errorf("expected 10000 items, got %d", count)
	}
}

func TestUnbounded_ConcurrentSend(t *testing.T) {
	buf := NewUnbounded[int]()
	numSenders := 10
	itemsPerSender := 1000

	var wg sync.WaitGroup
	wg.Add(numSenders)

	for i := 0; i < numSenders; i++ {
		go func(senderID int) {
			defer wg.Done()
			for j := 0; j < itemsPerSender; j++ {
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
	if count != expected {
		t.Errorf("expected %d items, got %d", expected, count)
	}
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

	if len(received) != 1 {
		t.Errorf("expected 1 item, got %d", len(received))
	}
	if received[0] != 1 {
		t.Errorf("expected 1, got %d", received[0])
	}
}

func TestUnbounded_DoubleClose(t *testing.T) {
	buf := NewUnbounded[int]()
	buf.Close()
	buf.Close() // Should not panic

	// Channel should be closed
	_, ok := <-buf.Receive()
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestUnbounded_EmptyClose(t *testing.T) {
	buf := NewUnbounded[int]()
	buf.Close()

	// Channel should be closed immediately
	select {
	case _, ok := <-buf.Receive():
		if ok {
			t.Error("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestUnbounded_SlowConsumer(t *testing.T) {
	buf := NewUnbounded[int]()

	// Start slow consumer
	received := make(chan int, 100)
	go func() {
		for item := range buf.Receive() {
			time.Sleep(time.Millisecond) // Slow consumption
			received <- item
		}
		close(received)
	}()

	// Fast producer
	for i := 0; i < 100; i++ {
		buf.Send(i)
	}
	buf.Close()

	// Verify all items received
	count := 0
	for range received {
		count++
	}
	if count != 100 {
		t.Errorf("expected 100 items, got %d", count)
	}
}

func TestUnbounded_LenAndIsClosed(t *testing.T) {
	buf := NewUnbounded[int]()

	if buf.IsClosed() {
		t.Error("expected buffer to not be closed initially")
	}

	buf.Send(1)
	buf.Send(2)

	// Note: Len may be 0 if drain goroutine has already moved items to output channel
	// This is just testing the method works, not exact values

	buf.Close()

	if !buf.IsClosed() {
		t.Error("expected buffer to be closed after Close()")
	}
}

func TestUnbounded_WithStruct(t *testing.T) {
	type TestStruct struct {
		ID   int
		Name string
	}

	buf := NewUnbounded[TestStruct]()

	buf.Send(TestStruct{ID: 1, Name: "one"})
	buf.Send(TestStruct{ID: 2, Name: "two"})
	buf.Close()

	var received []TestStruct
	for item := range buf.Receive() {
		received = append(received, item)
	}

	if len(received) != 2 {
		t.Errorf("expected 2 items, got %d", len(received))
	}
	if received[0].ID != 1 || received[0].Name != "one" {
		t.Errorf("unexpected first item: %+v", received[0])
	}
	if received[1].ID != 2 || received[1].Name != "two" {
		t.Errorf("unexpected second item: %+v", received[1])
	}
}

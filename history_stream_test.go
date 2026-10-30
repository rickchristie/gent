package gent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExecutionContext_ObserversDoNotBlockRecording(t *testing.T) {
	type input struct{ child, fast bool }
	type expected struct{ count int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"held listener", input{}, expected{100}},
		{"fast and held listeners", input{fast: true}, expected{100}},
		{"held parent listener", input{child: true}, expected{100}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := NewExecutionContext(context.Background(), "root", nil)
			emitter := root
			if test.input.child {
				emitter = root.SpawnChild("child", nil)
			}
			held, unsubscribe := root.SubscribeAll()
			defer unsubscribe()
			var fastDone chan []StreamChunk
			if test.input.fast {
				fast, unsubscribe := root.SubscribeAll()
				defer unsubscribe()
				fastDone = make(chan []StreamChunk, 1)
				go func() {
					var chunks []StreamChunk
					for chunk := range fast {
						chunk.Timestamp = chunk.Timestamp.Round(0)
						chunks = append(chunks, chunk)
					}
					fastDone <- chunks
				}()
			}
			done := make(chan error, 1)
			go func() {
				for i := 0; i < test.expected.count; i++ {
					if err := emitter.EmitChunk(
						StreamChunk{
							MetaContent: []*MetaContent{
								{
									Type: MetaContentIntermediary,
									Content: []ContentPart{
										TextContent{Text: "policy evidence"},
									},
								},
							},
						},
					); err != nil {
						done <- err
						return
					}
				}
				root.CloseStreams()
				done <- nil
			}()
			// The held subscriber has consumed nothing. Completion proves independence without a
			// throughput assertion against durable serialization or the race runtime.
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("held observer blocked recording")
			}
			view, err := root.Export()
			assert.NoError(t, err)
			var want []StreamChunk
			for _, event := range view.Activity {
				if event.Type == "model_stream_chunk" {
					value, err := root.DecodeEvent(event)
					assert.NoError(t, err)
					want = append(want, value.(StreamChunk))
				}
			}
			assert.Len(t, want, test.expected.count)
			if test.input.fast {
				select {
				case got := <-fastDone:
					assert.Equal(t, want, got)
				case <-time.After(10 * time.Second):
					t.Fatal("held observer blocked fast observer")
				}
			}
			var got []StreamChunk
			for chunk := range held {
				chunk.Timestamp = chunk.Timestamp.Round(0)
				got = append(got, chunk)
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestBoundedProviderStream_CancellationReleasesBackpressure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := NewBoundedStreamWithDuration(ctx, 1, nil)
	assert.NoError(t, err)
	assert.True(t, stream.SendMeta(&MetaContent{
		Type:    MetaContentIntermediary,
		Content: []ContentPart{TextContent{Text: "first"}},
	}))
	started := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		close(started)
		done <- stream.SendMeta(&MetaContent{
			Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "second"}},
		})
	}()
	<-started
	select {
	case <-done:
		t.Fatal("bounded queue did not apply backpressure")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case sent := <-done:
		assert.False(t, sent)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release producer")
	}
	stream.Complete(nil, context.Canceled)
	_, err = stream.Response()
	assert.ErrorIs(t, err, context.Canceled)
}

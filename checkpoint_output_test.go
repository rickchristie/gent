package gent_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func TestCheckpointTypedImageWaitsForSaveAndRestores(t *testing.T) {
	type input struct{ loseAcknowledgment bool }
	type expected struct {
		published bool
		err       error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	type execution struct {
		value  []gent.BinaryContent
		result *gent.ExecutionResult
		err    error
	}
	var imageFile bytes.Buffer
	if !assert.NoError(t, png.Encode(&imageFile, image.NewRGBA(image.Rect(0, 0, 2, 2)))) {
		return
	}
	imagePart := gent.BinaryContent{MIMEType: "image/png", Data: imageFile.Bytes()}
	wantValue := []gent.BinaryContent{imagePart}
	wantContent := []gent.ContentPart{imagePart}
	wantResult := &gent.ExecutionResult{
		TerminationReason: gent.TerminationSuccess, Output: wantContent}
	failure := errors.New("terminal commit acknowledgment lost")
	for _, test := range []testCase{
		{"commit", input{}, expected{true, nil}},
		{"lost acknowledgment", input{true}, expected{false, failure}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("image")
			entered, release := make(chan struct{}, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var terminalRequests []*gent.SaveBatch
			store.before = func(batch *gent.SaveBatch) error {
				if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseTerminal {
					terminalRequests = append(terminalRequests, batch)
					if len(terminalRequests) == 1 {
						entered <- struct{}{}
						<-release
					}
				}
				return nil
			}
			store.after = func(batch *gent.SaveBatch) error {
				if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseTerminal &&
					test.input.loseAcknowledgment && len(terminalRequests) == 1 {
					return failure
				}
				return nil
			}
			ctx := gent.NewExecutionContext(context.Background(), "image",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Draw a small image."})),
				gent.WithRunId("image"), gent.WithRecorder(store))
			defer ctx.Release()
			live, err := ctx.SubscribeAllWithConfig(gent.DeliveryConfig{})
			if !assert.NoError(t, err) {
				return
			}
			defer live.Close()
			var modelCalls, processed, validated atomic.Int64
			model := checkpointModel(func(context.Context, gent.ModelCallRequest) (gent.Stream, error) {
				modelCalls.Add(1)
				stream := gent.NewStreamWithDuration()
				stream.SendMeta(&gent.MetaContent{Type: gent.MetaContentIntermediary, Content: wantContent})
				stream.Complete(&gent.ContentResponse{Choices: []*gent.ContentChoice{{
					Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
					CandidateContent: wantContent,
				}}}, nil)
				return stream, nil
			})
			out := output.NewImageOutput().SetCustomValidator("image/v1", func(
				*gent.ExecutionContext, []gent.BinaryContent,
			) (*gent.OutputValidationResult, error) {
				validated.Add(1)
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			loop := react.NewWithOutput(model, observedOutput[[]gent.BinaryContent]{
				Output: out, calls: &processed}).WithRecoveryVersion("image/v1")
			runner := executor.NewRunner(loop, executor.DefaultConfig())
			done := make(chan execution, 1)
			go func() {
				value, result, err := runner.Execute(ctx)
				done <- execution{value, result, err}
			}()
			checkpointReceive(t, entered)
			preview := checkpointReceive(t, live.Chunks())
			assert.Equal(t, wantContent, gent.MetaParts(preview.MetaContent, gent.MetaContentIntermediary))
			preview.MetaContent[0].Content[0].(gent.BinaryContent).Data[0] = 0
			select {
			case result := <-done:
				t.Fatalf("typed output returned before save completed: %v", result)
			case chunk := <-live.Chunks():
				t.Fatalf("final content published before save completed: %v", chunk)
			default:
			}
			unblock()
			completed := checkpointReceive(t, done)
			assert.ErrorIs(t, completed.err, test.expected.err)
			var published []gent.ContentPart
			for chunk := range live.Chunks() {
				published = append(published, chunk.Content...)
			}
			assert.NoError(t, live.Err())
			if test.expected.published {
				assert.Equal(t, wantValue, completed.value)
				assert.Equal(t, wantResult, completed.result)
				assert.Equal(t, wantContent, published)
				completed.value[0].Data[0] = 0
				completed.result.Output[0].(gent.BinaryContent).Data[0] = 0
			} else {
				assert.Nil(t, completed.value)
				assert.Nil(t, completed.result)
				assert.Nil(t, published)
				_, err := ctx.RetrySave(context.Background())
				assert.NoError(t, err)
				if assert.Len(t, terminalRequests, 2) {
					assert.Equal(t, terminalRequests[0], terminalRequests[1])
				}
			}
			wire, err := store.codecs.Marshal(store.view)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, gent.NewCodecRegistry().Unmarshal(wire, &saved)) {
				return
			}
			assert.NoError(t, gent.ValidateBundle(saved))
			value, result, err := runner.Resume(context.Background(), saved, gent.RestoreBindings{})
			assert.NoError(t, err)
			assert.Equal(t, wantValue, value)
			assert.Equal(t, wantResult, result)
			assert.Equal(t, int64(1), modelCalls.Load())
			assert.Equal(t, int64(1), processed.Load())
			assert.Equal(t, int64(1), validated.Load())
		})
	}
}

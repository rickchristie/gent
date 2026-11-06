package gent_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/stretchr/testify/assert"
)

type mediaRequestHook struct{}

func (*mediaRequestHook) OnBeforeModelCall(_ *gent.ExecutionContext,
	event *gent.BeforeModelCallEvent,
) {
	event.Request.Messages[0].Parts[0].(gent.BinaryContent).Data[0] = 9
}

type mediaRequestObserver struct{}

func (*mediaRequestObserver) OnAfterModelCall(*gent.ExecutionContext, *gent.AfterModelCallEvent) {}

func TestManagedRequestCopiesMediaOnlyForMutableHooks(t *testing.T) {
	type input struct{ publisher gent.EventPublisher }
	type expected struct {
		shared bool
		bytes  []byte
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"no dispatcher", input{}, expected{true, []byte{1, 2, 3}}},
		{"empty registry", input{events.NewRegistry()}, expected{true, []byte{1, 2, 3}}},
		{"after hook only", input{events.NewRegistry().Subscribe(&mediaRequestObserver{})},
			expected{true, []byte{1, 2, 3}}},
		{"mutable request hook", input{events.NewRegistry().Subscribe(&mediaRequestHook{})},
			expected{false, []byte{9, 2, 3}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := []byte{1, 2, 3}
			ctx := gent.NewExecutionContext(context.Background(), "media", nil)
			defer ctx.Release()
			ctx.SetEventPublisher(test.input.publisher)
			var actual expected
			model := checkpointModel(func(_ context.Context, request *gent.ModelCallRequest) (
				gent.Stream, error,
			) {
				received := request.Messages[0].Parts[0].(gent.BinaryContent).Data
				actual = expected{shared: &received[0] == &data[0], bytes: received}
				return gent.NewCompletedStream(&gent.ModelCallResponse{}, nil), nil
			})
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{gent.PDFPart(data)}},
			})
			stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, request)
			if !assert.NoError(t, err) {
				return
			}
			_, err = stream.Response()
			stream.Close()
			assert.NoError(t, err)
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, []byte{1, 2, 3}, data)
		})
	}
}

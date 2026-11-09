package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// Count body ownership at the real HTTP boundary. A completed cancellation check must not leave
// a response body open. This also checks cleanup before the adapter reports its final result.
type cancellationTransport struct {
	base     http.RoundTripper
	opened   atomic.Int32
	closed   atomic.Int32
	finished atomic.Int32
}

func (tr *cancellationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := tr.base.RoundTrip(request)
	tr.finished.Add(1)
	if err != nil {
		return response, err
	}
	tr.opened.Add(1)
	response.Body = &cancellationTrackedBody{ReadCloser: response.Body, closed: &tr.closed}
	return response, nil
}

type cancellationTrackedBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (body *cancellationTrackedBody) Close() error {
	err := body.ReadCloser.Close()
	body.closed.Add(1)
	return err
}

type cancellationInputBody struct {
	reader *strings.Reader
	ctx    context.Context
}

func (body *cancellationInputBody) Read(data []byte) (int, error) {
	if body.reader.Len() > 0 {
		return body.reader.Read(data)
	}
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (*cancellationInputBody) Close() error { return nil }

func TestStreamingCancellationRejectsFailure(t *testing.T) {
	type input struct {
		status int
		stream bool
	}
	type expected struct {
		err      string
		opened   int32
		closed   int32
		finished int32
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"invalid key", input{401, false},
			expected{"unexpected stream error: model HTTP 401: rejected", 1, 1, 1}},
		{"rate limit", input{429, false},
			expected{"unexpected stream error: model HTTP 429: rejected", 1, 1, 1}},
		{"early answer", input{200, false}, expected{
			"stream ended before cancellation (1 chunks): cancellation not verified", 1, 1, 1}},
		{"cancellation and cleanup", input{200, true}, expected{"", 1, 1, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tr := &cancellationTransport{base: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if test.input.stream {
					const delta = `data: {"type":"response.output_text.delta","delta":"word ","output_index":0}

`
					return &http.Response{StatusCode: 200,
						Header: http.Header{"Content-Type": {"text/event-stream"}},
						Body: &cancellationInputBody{strings.NewReader(strings.Repeat(delta, 5)),
							request.Context()}}, nil
				}
				body := "rejected"
				if test.input.status == 200 {
					body = providerResponse("xai", 3)
				}
				response := fixtureHTTP(body, "application/json")
				response.StatusCode = test.input.status
				return response, nil
			})}
			model := fixtureModel("xai", &http.Client{Transport: tr})
			err := runStreamingCancellation(context.Background(), io.Discard, TestOutputConfig(), model)
			actual := expected{opened: tr.opened.Load(), closed: tr.closed.Load(),
				finished: tr.finished.Load()}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestLiveProviderCancellation(t *testing.T) {
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			tr := &cancellationTransport{base: http.DefaultTransport}
			config.HTTPClient = &http.Client{Transport: tr}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := runStreamingCancellation(ctx, io.Discard, TestOutputConfig(),
				newLiveModel(t, provider, &config))
			assert.NoError(t, err)
			assert.Positive(t, tr.opened.Load())
			assert.Equal(t, tr.opened.Load(), tr.closed.Load(), "all HTTP bodies must be closed")
			assert.Equal(t, tr.opened.Load(), tr.finished.Load(), "all HTTP requests must finish")
		})
	}
}

func TestStreamingCancellationRejectsCompletionCause(t *testing.T) {
	type input struct{ cause error }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"completed before cancellation", input{nil},
			expected{"expected context cancellation, got <nil>"}},
		{"request deadline", input{context.DeadlineExceeded},
			expected{"unexpected stream error: context deadline exceeded"}},
		{"joined cleanup failure", input{errors.Join(context.Canceled, io.ErrUnexpectedEOF)},
			expected{`unexpected stream error: context canceled
unexpected EOF`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var chunks []gent.StreamChunk
			for range 5 {
				chunks = append(chunks, gent.StreamChunk{MetaContent: []*gent.MetaContent{
					{Type: gent.MetaContentIntermediary, Content: []gent.ContentPart{gent.TextPart("word")}},
				}})
			}
			err := runStreamingCancellation(context.Background(), io.Discard, TestOutputConfig(),
				liveCheckModel{chunks: chunks, err: test.input.cause})
			actual := expected{}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

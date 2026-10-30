package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type providerBody struct {
	reader io.Reader
	closed atomic.Int32
	err    error
}

func (body *providerBody) Read(data []byte) (int, error) { return body.reader.Read(data) }
func (body *providerBody) Close() error {
	body.closed.Add(1)
	return body.err
}

func TestProvidersCancellationStopsProducer(t *testing.T) {
	type input struct {
		provider     string
		closeStream  bool
		beforeHeader bool
	}
	type expected struct {
		err    error
		closed bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		cases = append(cases,
			testCase{provider + "/context", input{provider: provider},
				expected{context.Canceled, true}},
			testCase{provider + "/concurrent close", input{provider: provider, closeStream: true},
				expected{context.Canceled, true}},
			testCase{provider + "/before headers", input{provider: provider, beforeHeader: true},
				expected{context.Canceled, false}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan *cancelResponseBody, 1)
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				body := &cancelResponseBody{ctx: request.Context(), closed: make(chan struct{})}
				started <- body
				if test.input.beforeHeader {
					<-request.Context().Done()
					return nil, request.Context().Err()
				}
				mime := "text/event-stream"
				if test.input.provider == "ollama" {
					mime = "application/x-ndjson"
				}
				return &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {mime}}, Body: body}, nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(ctx,
				gent.NewModelCallRequest([]*gent.ModelMessage{
					gent.TextParts(gent.ChatMessageTypeHuman, "Read.")}))
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			var body *cancelResponseBody
			select {
			case body = <-started:
			case <-ctx.Done():
				t.Fatal("provider did not start")
			}
			if test.input.closeStream {
				var group sync.WaitGroup
				for range 8 {
					group.Add(1)
					go func() {
						defer group.Done()
						stream.Close()
					}()
				}
				group.Wait()
			} else {
				cancel()
			}
			response, err := stream.Response()
			assert.Nil(t, response)
			assert.ErrorIs(t, err, test.expected.err)
			closed := false
			select {
			case <-body.closed:
				closed = true
			default:
			}
			assert.Equal(t, test.expected.closed, closed,
				"Response must wait for HTTP cleanup")
		})
	}
}

func TestProvidersOperationFailures(t *testing.T) {
	type input struct {
		provider, mode string
	}
	type expected struct {
		errText string
		err     error
		closed  int32
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	callbackErr := errors.New("consumer failed")
	closeErr := errors.New("response close failed")
	var cases []testCase
	for _, provider := range testProviders {
		for _, mode := range []string{"callback", "close", "http", "truncated", "trailing JSON"} {
			want := expected{closed: 1}
			switch mode {
			case "callback":
				want.err = callbackErr
			case "close":
				want.err = closeErr
			case "http":
				want.errText = "model HTTP 429: busy"
			case "truncated":
				want.errText = "model stream ended without a terminal event"
				if provider == "ollama" {
					want.errText = "Ollama stream ended without done"
				}
			case "trailing JSON":
				want.errText = "model returned more than one JSON value"
			}
			cases = append(cases, testCase{provider + "/" + mode, input{provider, mode}, want})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data, mime := providerResponse(test.input.provider, 3), "application/json"
			body := &providerBody{}
			status := http.StatusOK
			switch test.input.mode {
			case "close":
				body.err = closeErr
			case "http":
				status, data = http.StatusTooManyRequests, "busy"
			case "truncated":
				data, mime = streamFixture(t, test.input.provider, "answer")
				separator := `

`
				if test.input.provider == "ollama" {
					separator = `
`
				}
				data = data[:strings.Index(data, separator)+len(separator)]
			case "trailing JSON":
				data += `{}`
			}
			body.reader = strings.NewReader(data)
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				return &http.Response{StatusCode: status,
					Header: http.Header{"Content-Type": {mime}}, Body: body}, nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read.")})
			if test.input.mode == "callback" {
				request.Options.StreamingFunc = func(context.Context, []byte) error { return callbackErr }
			}
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			for chunk := range stream.Chunks() {
				assert.Empty(t, chunk.Content)
			}
			_, err = stream.Response()
			if test.expected.err != nil {
				assert.ErrorIs(t, err, test.expected.err)
			} else {
				assert.EqualError(t, err, test.expected.errText)
			}
			assert.Equal(t, test.expected.closed, body.closed.Load())
		})
	}
}

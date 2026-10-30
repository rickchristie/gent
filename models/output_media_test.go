package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestOutputImageDownloadFailuresReleaseBody(t *testing.T) {
	type input struct {
		mime   string
		status int
		reader io.Reader
		close  error
	}
	type expected struct {
		text string
		err  error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	closeErr := errors.New("image close failed")
	cases := []testCase{
		{"expired URL", input{status: 404, reader: strings.NewReader("expired")},
			expected{text: "model HTTP 404: expired"}},
		{"wrong media", input{status: 200, mime: "text/plain", reader: strings.NewReader("wrong")},
			expected{text: `output reference returned "text/plain"`}},
		{"file bound", input{status: 200, mime: "image/png", reader: zeroReader{}},
			expected{text: "output image exceeds 32 MiB"}},
		{"close error", input{status: 200, mime: "image/png",
			reader: strings.NewReader("image"), close: closeErr}, expected{err: closeErr}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := &providerBody{reader: test.input.reader, err: test.input.close}
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				assert.Equal(t, http.MethodGet, request.Method)
				assert.Equal(t, "", request.Header.Get("Authorization"))
				return &http.Response{StatusCode: test.input.status, Body: body,
					Header: http.Header{"Content-Type": {test.input.mime}}}, nil
			})}
			image, err := resolveOutputImage(context.Background(), client, "https://image.test/a")
			assert.Equal(t, gent.BinaryContent{}, image)
			if test.expected.err != nil {
				assert.ErrorIs(t, err, test.expected.err)
			} else {
				assert.EqualError(t, err, test.expected.text)
			}
			assert.Equal(t, int32(1), body.closed.Load())
		})
	}
}

func TestOutputImageDownloadCancellationStopsReader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan *cancelResponseBody, 1)
	client := &http.Client{Transport: responseTransport(func(request *http.Request) (
		*http.Response, error,
	) {
		body := &cancelResponseBody{ctx: request.Context(), closed: make(chan struct{})}
		started <- body
		return &http.Response{StatusCode: 200, Body: body,
			Header: http.Header{"Content-Type": {"image/png"}}}, nil
	})}
	finished := make(chan error, 1)
	go func() {
		_, err := resolveOutputImage(ctx, client, "https://image.test/a")
		finished <- err
	}()
	var body *cancelResponseBody
	select {
	case body = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	assert.ErrorIs(t, <-finished, context.Canceled)
	select {
	case <-body.closed:
	default:
		t.Fatal("download returned before closing its body")
	}
}

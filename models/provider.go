package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rickchristie/gent"
)

// Config selects an endpoint and model. BaseURL and HTTPClient are optional. The adapter supplies
// its public endpoint, or the runtime's default local endpoint, when BaseURL is empty.
type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func providerConfig(config *Config, defaultURL string) Config {
	copy := *config
	if copy.BaseURL == "" {
		copy.BaseURL = defaultURL
	}
	copy.BaseURL = strings.TrimRight(copy.BaseURL, "/")
	if copy.HTTPClient == nil {
		copy.HTTPClient = http.DefaultClient
	}
	return copy
}

// A response can contain large images. Bound individual events and JSON bodies without retaining
// all stream events. Binary output gets a separate decoded-byte bound before allocation.
const maxProviderBytes = 64 << 20

type providerReader func(context.Context, *http.Response, *gent.StreamWithDuration) (
	*gent.ModelCallResponse, error,
)

func startProviderStream(base context.Context, client *http.Client, url string,
	headers http.Header, body any, read providerReader, steering ...gent.SteeringMessages,
) (gent.Stream, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(base)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header = headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	stream, err := gent.NewBoundedStreamWithDuration(ctx, 64, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer cancel()
		var response *http.Response
		var result *gent.ModelCallResponse
		for attempt := 0; attempt < 2; attempt++ {
			response, err = client.Do(request)
			if err != nil {
				stream.Complete(nil, err)
				return
			}
			err = checkHTTPResponse(response)
			if err == nil {
				result, err = read(ctx, response, stream)
				break
			}
			if attempt != 0 || len(steering) == 0 {
				break
			}
			corrected, retry, prepareErr := replaceFailedImageURL(ctx, data, err, steering[0])
			if prepareErr != nil {
				err = errors.Join(err, prepareErr)
				break
			}
			if !retry {
				break
			}
			if closeErr := response.Body.Close(); closeErr != nil {
				stream.Complete(nil, closeErr)
				return
			}
			request = request.Clone(ctx)
			request.Body = io.NopCloser(bytes.NewReader(corrected))
			request.ContentLength = int64(len(corrected))
			request.GetBody = nil
		}
		closeErr := response.Body.Close()
		if result != nil {
			result.Info.Duration = stream.Duration()
		}
		stream.Complete(result, errors.Join(err, closeErr))
	}()
	return stream, nil
}

func checkHTTPResponse(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return err
	}
	failure := &modelHTTPError{status: response.StatusCode, body: strings.TrimSpace(string(data))}
	var envelope struct {
		Error struct {
			Code    string
			Type    string
			Message string
		}
	}
	if json.Unmarshal(data, &envelope) == nil {
		failure.code, failure.message = envelope.Error.Code, envelope.Error.Message
		if failure.code == "" {
			failure.code = envelope.Error.Type
		}
	}
	return failure
}

func decodeProviderJSON(reader io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxProviderBytes+1))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.InputOffset() > maxProviderBytes {
		return errors.New("model response exceeds 64 MiB")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("model returned more than one JSON value")
	}
	return nil
}

// readSSE accepts multi-line data and comments. A consumer returns done only for a terminal event.
// EOF alone cannot establish a complete model answer.
func readSSE(reader io.Reader, consume func([]byte) (bool, error)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxProviderBytes)
	var data bytes.Buffer
	flush := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		done, err := consume(data.Bytes())
		data.Reset()
		return done, err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			done, err := flush()
			if done || err != nil {
				return err
			}
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		line = bytes.TrimPrefix(line[5:], []byte(" "))
		if data.Len()+len(line)+1 > maxProviderBytes {
			return errors.New("model stream event exceeds 64 MiB")
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.Write(line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	done, err := flush()
	if done || err != nil {
		return err
	}
	return errors.New("model stream ended without a terminal event")
}

func sendProviderText(ctx context.Context, stream *gent.StreamWithDuration,
	options *gent.CallOptions, kind, id, value string,
) error {
	if value == "" {
		return nil
	}
	if kind == gent.MetaContentReasoning && options.StreamingReasoningFunc != nil {
		if err := options.StreamingReasoningFunc(ctx, []byte(value), nil); err != nil {
			return err
		}
	}
	if kind == gent.MetaContentIntermediary && options.StreamingFunc != nil {
		if err := options.StreamingFunc(ctx, []byte(value)); err != nil {
			return err
		}
	}
	if !stream.SendMeta(&gent.MetaContent{Type: kind, MessageId: id,
		Content: []gent.ContentPart{gent.TextContent{Text: value}}}) {
		return ctx.Err()
	}
	return nil
}

// A server can return one JSON body even when streaming was requested. Deliver the same
// provisional metadata and callbacks in that case; only the agent can accept output.
func previewProviderResponse(ctx context.Context, stream *gent.StreamWithDuration,
	options *gent.CallOptions, response *gent.ModelCallResponse,
) (*gent.ModelCallResponse, error) {
	for _, choice := range response.Choices {
		for _, meta := range choice.MetaContent {
			text := gent.ContentText(meta.Content)
			if text != "" && meta.Type == gent.MetaContentReasoning &&
				options.StreamingReasoningFunc != nil {
				if err := options.StreamingReasoningFunc(ctx, []byte(text), nil); err != nil {
					return nil, err
				}
			}
			if text != "" && meta.Type == gent.MetaContentIntermediary && options.StreamingFunc != nil {
				if err := options.StreamingFunc(ctx, []byte(text)); err != nil {
					return nil, err
				}
			}
			if !stream.SendMeta(meta) {
				return nil, ctx.Err()
			}
		}
	}
	return response, nil
}

func partValue(part gent.ContentPart) gent.ContentPart {
	switch p := part.(type) {
	case *gent.DataContent:
		return *p
	case *gent.TextContent:
		return *p
	case *gent.BinaryContent:
		return *p
	case *gent.ImageURLContent:
		return *p
	case *gent.ModelToolCallRequest:
		return *p
	case *gent.ModelToolCallResponse:
		return p
	case *gent.CachedContent:
		return *p
	default:
		return part
	}
}

func messageRole(role gent.ChatMessageType) (string, error) {
	switch role {
	case gent.ChatMessageTypeSystem:
		return "system", nil
	case gent.ChatMessageTypeHuman:
		return "user", nil
	case gent.ChatMessageTypeAI:
		return "assistant", nil
	case gent.ChatMessageTypeTool:
		return "tool", nil
	default:
		return "", fmt.Errorf("unsupported message role %q", role)
	}
}

func checkContinuation(saved *gent.ProviderContinuation, provider string) error {
	if saved.Provider != provider {
		return fmt.Errorf("cannot replay %s items through %s", saved.Provider, provider)
	}
	for _, item := range saved.Items {
		if !json.Valid(item) {
			return errors.New("invalid saved provider item")
		}
	}
	return nil
}

func textOutputOnly(options *gent.CallOptions, provider string) error {
	for _, modality := range options.ResponseModalities {
		if modality != "text" {
			return fmt.Errorf("%s does not support output modality %q", provider, modality)
		}
	}
	if options.ResponseMIMEType != "" && options.ResponseMIMEType != "text/plain" &&
		options.ResponseMIMEType != "application/json" {
		return fmt.Errorf("%s does not support output type %q", provider, options.ResponseMIMEType)
	}
	return nil
}

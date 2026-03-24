package models

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// ErrorCaptureTransport wraps an http.RoundTripper to ensure non-2xx
// response bodies are included in error messages.
//
// LangChainGo's OpenAI client expects error responses in
// {"error":{"message":"..."}} format. When the API returns a different
// format (plain text, HTML, etc.), the body is silently dropped from
// the error. This transport rewrites non-JSON error bodies into the
// expected format so the full response content is visible for debugging.
type ErrorCaptureTransport struct {
	Base http.RoundTripper
}

// openAIErrorBody is the JSON error structure LangChainGo expects.
type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (t *ErrorCaptureTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	// Non-2xx: read body and ensure it's in expected format.
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, nil
	}

	// If already in expected JSON format, pass through unchanged.
	var existing openAIErrorBody
	if json.Unmarshal(body, &existing) == nil &&
		existing.Error.Message != "" {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	// Rewrite raw body into the expected JSON format.
	rawText := string(body)
	const maxBodyLen = 2000
	if len(rawText) > maxBodyLen {
		rawText = rawText[:maxBodyLen] + "... (truncated)"
	}

	var synthetic openAIErrorBody
	synthetic.Error.Message = rawText
	newBody, _ := json.Marshal(synthetic)

	resp.Body = io.NopCloser(bytes.NewReader(newBody))
	resp.ContentLength = int64(len(newBody))

	return resp, nil
}

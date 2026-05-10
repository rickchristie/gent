package trace

import (
	"fmt"

	"github.com/rickchristie/gent"
)

// Redactor removes sensitive data before trace data is stored.
// Implementations must be fast, non-blocking, and concurrency-safe.
type Redactor interface {
	RedactModelRequest(req any) any
	RedactModelResponse(resp any) any
	RedactToolArgs(args any) any
	RedactToolOutput(output any) any
	RedactSystemPrompt(sections []gent.FormattedSection) any
	RedactCommonPayload(eventName string, payload any) any
	RedactDiffPayload(eventName string, before any, after any, diff string) any
	RedactRunOutput(output []gent.ContentPart) any
	RedactChunk(chunk gent.StreamChunk) gent.StreamChunk
	RedactError(err error) *Error
}

// RedactorFuncs adapts functions into a Redactor.
// Nil functions keep non-error values unchanged and use default error redaction.
type RedactorFuncs struct {
	ModelRequest  func(any) any
	ModelResponse func(any) any
	ToolArgs      func(any) any
	ToolOutput    func(any) any
	SystemPrompt  func([]gent.FormattedSection) any
	CommonPayload func(string, any) any
	DiffPayload   func(string, any, any, string) any
	RunOutput     func([]gent.ContentPart) any
	Chunk         func(gent.StreamChunk) gent.StreamChunk
	Error         func(error) *Error
}

func (r RedactorFuncs) RedactModelRequest(req any) any {
	if r.ModelRequest == nil {
		return req
	}
	return r.ModelRequest(req)
}

func (r RedactorFuncs) RedactModelResponse(resp any) any {
	if r.ModelResponse == nil {
		return resp
	}
	return r.ModelResponse(resp)
}

func (r RedactorFuncs) RedactToolArgs(args any) any {
	if r.ToolArgs == nil {
		return args
	}
	return r.ToolArgs(args)
}

func (r RedactorFuncs) RedactToolOutput(output any) any {
	if r.ToolOutput == nil {
		return output
	}
	return r.ToolOutput(output)
}

func (r RedactorFuncs) RedactSystemPrompt(sections []gent.FormattedSection) any {
	if r.SystemPrompt == nil {
		return sections
	}
	return r.SystemPrompt(sections)
}

func (r RedactorFuncs) RedactCommonPayload(eventName string, payload any) any {
	if r.CommonPayload == nil {
		return payload
	}
	return r.CommonPayload(eventName, payload)
}

func (r RedactorFuncs) RedactDiffPayload(
	eventName string,
	before any,
	after any,
	diff string,
) any {
	if r.DiffPayload == nil {
		return map[string]any{"before": before, "after": after, "diff": diff}
	}
	return r.DiffPayload(eventName, before, after, diff)
}

func (r RedactorFuncs) RedactRunOutput(output []gent.ContentPart) any {
	if r.RunOutput == nil {
		return output
	}
	return r.RunOutput(output)
}

func (r RedactorFuncs) RedactChunk(chunk gent.StreamChunk) gent.StreamChunk {
	if r.Chunk == nil {
		return chunk
	}
	return r.Chunk(chunk)
}

func (r RedactorFuncs) RedactError(err error) *Error {
	if err == nil {
		return nil
	}
	if r.Error != nil {
		return r.Error(err)
	}
	return &Error{Message: err.Error(), Type: fmt.Sprintf("%T", err)}
}

func redactError(redactor Redactor, err error) *Error {
	if err == nil {
		return nil
	}
	redacted := redactor.RedactError(err)
	if redacted == nil {
		return &Error{Message: "redacted error"}
	}
	return cloneError(redacted)
}

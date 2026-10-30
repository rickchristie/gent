package gent

import "errors"

var (
	ErrInvalidJSON     = errors.New("invalid JSON")
	ErrMissingToolName = errors.New("tool call has no function name")
	ErrUnknownTool     = errors.New("unknown tool")
	ErrInvalidToolArgs = errors.New("invalid tool arguments")
)

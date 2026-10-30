package gent

import (
	"context"
	"errors"
	"io"
	"reflect"
)

// SavedError preserves error text and the public classifications used during continuation.
// Private implementation types must be rebound through an application codec when needed.
type SavedError struct {
	Message string        `json:"message"`
	Codes   []string      `json:"codes,omitempty"`
	Causes  []*SavedError `json:"causes,omitempty"`
}

// CodedError gives application errors a stable classification without serializing private fields.
// Use an application namespace and keep its meaning stable across recovery versions.
type CodedError interface {
	error
	ErrorCode() string
}

var (
	ErrRepetitionDetected = errors.New("repetition detected: model output is looping")
	ErrMaxResponseSize    = errors.New("max response size exceeded")
	ErrReplayUnavailable  = errors.New("trace replay unavailable")
)

var savedErrorSentinels = map[string]error{
	"canceled":                context.Canceled,
	"deadline_exceeded":       context.DeadlineExceeded,
	"eof":                     io.EOF,
	"unexpected_eof":          io.ErrUnexpectedEOF,
	"invalid_json":            ErrInvalidJSON,
	"missing_tool":            ErrMissingToolName,
	"unknown_tool":            ErrUnknownTool,
	"invalid_tool_args":       ErrInvalidToolArgs,
	"repetition":              ErrRepetitionDetected,
	"response_size":           ErrMaxResponseSize,
	"uncertain_tool":          ErrUncertainTool,
	"unsupported_recovery":    ErrUnsupportedRecovery,
	"incompatible_checkpoint": ErrIncompatibleCheckpoint,
	"event_gap":               ErrEventGap,
	"wrong_run":               ErrWrongRun,
	"history_conflict":        ErrHistoryConflict,
	"persistence_stopped":     ErrPersistenceStopped,
	"tool_plan_complete":      ErrToolPlanComplete,
	"replay_unavailable":      ErrReplayUnavailable,
}

func (e *SavedError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *SavedError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	for _, code := range e.Codes {
		if savedErrorSentinels[code] == target {
			return true
		}
		if coded, ok := target.(CodedError); ok && code == coded.ErrorCode() {
			return true
		}
	}
	return false
}

func (e *SavedError) Unwrap() []error {
	if e == nil {
		return nil
	}
	result := make([]error, len(e.Causes))
	for i, cause := range e.Causes {
		result[i] = cause
	}
	return result
}

func SaveError(err error) *SavedError {
	return saveError(err, 0)
}

func saveError(err error, depth int) *SavedError {
	if err == nil {
		return nil
	}
	value := reflect.ValueOf(err)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil
	}
	result := &SavedError{Message: err.Error()}
	// Stop malformed cyclic error chains. The text of the boundary remains available.
	if depth >= 32 {
		return result
	}
	if saved, ok := err.(*SavedError); ok {
		result.Codes = append([]string(nil), saved.Codes...)
	} else {
		if coded, ok := err.(CodedError); ok && coded.ErrorCode() != "" {
			result.Codes = append(result.Codes, coded.ErrorCode())
		}
		for code, sentinel := range savedErrorSentinels {
			if err == sentinel {
				result.Codes = append(result.Codes, code)
			}
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			result.Causes = append(result.Causes, saveError(cause, depth+1))
		}
		return result
	}
	if cause := errors.Unwrap(err); cause != nil {
		result.Causes = []*SavedError{saveError(cause, depth+1)}
	}
	return result
}

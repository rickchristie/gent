package gent

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Validate checks the explicit output decision. A processor error must be handled before this
// method; a nil result or unclear rejection is an implementation error, not permission to retry.
func (result *OutputResult[T]) Validate() error {
	if result == nil {
		return errors.New("gent: output returned no result")
	}
	if result.Accepted {
		if len(result.Steering) != 0 {
			return errors.New("gent: accepted output contains steering")
		}
		return nil
	}
	if !UsefulSteering(result.Steering) {
		return errors.New("gent: rejected output requires useful steering")
	}
	return nil
}

// UsefulSteering checks for useful user-message content. Tool protocol parts cannot serve as
// correction instructions. Media can supply a reference example without being converted to text.
func UsefulSteering(parts []ContentPart) bool {
	useful := false
	for _, part := range parts {
		switch part := part.(type) {
		case TextContent:
			useful = useful || strings.TrimSpace(part.Text) != ""
		case *TextContent:
			useful = useful || strings.TrimSpace(part.Text) != ""
		case BinaryContent:
			useful = useful || (part.MIMEType != "" && len(part.Data) > 0)
		case *BinaryContent:
			useful = useful || (part.MIMEType != "" && len(part.Data) > 0)
		case ImageURLContent:
			useful = useful || part.URL != ""
		case *ImageURLContent:
			useful = useful || part.URL != ""
		default:
			return false
		}
	}
	return useful
}

// AcceptOutput freezes a validated typed value and its delivery content into one loop result.
// Callers do not decode or assert T. The runner saves this result before it returns T to its
// caller.
// Result codecs must be registered during setup, before model calls or application effects.
func AcceptOutput[T any](ctx *ExecutionContext, result *OutputResult[T], source *ContentSource) (
	*AgentLoopResult, error,
) {
	if err := result.Validate(); err != nil {
		return nil, err
	}
	if !result.Accepted {
		return nil, errors.New("gent: cannot accept a rejected output")
	}
	value, err := ctx.Codecs().Encode(result.Value)
	if err != nil {
		return nil, fmt.Errorf("gent: save output value: %w", err)
	}
	return &AgentLoopResult{Action: LATerminate, TerminationReason: TerminationSuccess,
		Value:         &value,
		Result:        CloneContentParts(result.Content),
		ContentSource: cloneContentSource(source)}, nil
}

// OutputValue decodes a saved successful result directly as T. It does not call the processor or
// validator. A nil pointer is a valid T; a missing typed value is a different, invalid saved state.
func OutputValue[T any](ctx *ExecutionContext) (T, error) {
	var zero T
	value := ctx.AcceptedValue()
	if value == nil {
		return zero, errors.New("gent: execution has no accepted typed value")
	}
	decoded, err := ctx.Codecs().Decode(*value)
	if err != nil {
		return zero, err
	}
	if decoded == nil && reflect.TypeFor[T]().Kind() == reflect.Interface {
		return zero, nil
	}
	result, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf(
			"gent: saved output has type %T, expected %s", decoded, reflect.TypeFor[T]())
	}
	return result, nil
}

// SetAcceptedValue stores the owned value that accompanies the next successful terminal save.
// A runner checks the declared output type before calling this method. Rejection and failure paths
// must not set it. Setting a non-successful termination clears any pending value.
func (ctx *ExecutionContext) SetAcceptedValue(value *EncodedValue) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.acceptedValue = cloneWire(value)
}

func (ctx *ExecutionContext) AcceptedValue() *EncodedValue {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return cloneWire(ctx.acceptedValue)
}

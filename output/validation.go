// Package output supplies standard answer contracts. Callers choose whether to include
// Instructions in their Task. Native request controls and local validation do not depend on it.
package output

import (
	"errors"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
)

type customValidator[T any] struct {
	name string
	call gent.OutputCustomValidator[T]
}

func (v customValidator[T]) check() error {
	if v.call != nil && strings.TrimSpace(v.name) == "" {
		return errors.New("output: custom validator requires a name")
	}
	return nil
}

func (v customValidator[T]) validate(
	ctx *gent.ExecutionContext, value T,
) (processed *gent.OutputResult[T], processErr error) {
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	if v.call == nil {
		return nil, nil
	}
	defer func() {
		var steering []gent.ContentPart
		if processed != nil {
			steering = processed.Steering
		}
		ctx.PublishOutputValidation(gent.OutputValidationCustom, v.name,
			processErr == nil && processed == nil, steering, processErr)
	}()
	// The callback receives its own snapshot. It cannot change an accepted pointer, map, slice,
	// or image buffer. A callback that attempts a transform violates the validation contract.
	if err := ctx.Codecs().RegisterValueType(reflect.TypeFor[T]()); err != nil {
		return nil, err
	}
	ownedValue, err := ctx.Codecs().Clone(value)
	if err != nil {
		return nil, err
	}
	var callbackValue T
	if ownedValue != nil {
		callbackValue = ownedValue.(T)
	}
	result, err := v.call(ctx, callbackValue)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(callbackValue, value) {
		return nil, errors.New("output: custom validator must not change the value")
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("output: custom validator returned no result")
	}
	if result.Accepted {
		if len(result.Steering) != 0 {
			return nil, errors.New("output: accepted validation must not contain steering")
		}
		return nil, nil
	}
	if !gent.UsefulSteering(result.Steering) {
		return nil, errors.New("output: rejected validation requires useful steering")
	}
	owned, err := ctx.Codecs().Clone(result.Steering)
	if err != nil {
		return nil, err
	}
	return &gent.OutputResult[T]{Steering: owned.([]gent.ContentPart)}, nil
}

func reject[T any](message string) *gent.OutputResult[T] {
	return &gent.OutputResult[T]{Steering: []gent.ContentPart{gent.TextContent{Text: message}}}
}

func textContent(parts []gent.ContentPart) (string, []gent.ContentPart, bool) {
	var text strings.Builder
	owned := make([]gent.ContentPart, 0, len(parts))
	for _, part := range parts {
		var value gent.TextContent
		switch p := part.(type) {
		case gent.TextContent:
			value = p
		case *gent.TextContent:
			value = *p
		default:
			return "", nil, false
		}
		text.WriteString(value.Text)
		owned = append(owned, value)
	}
	return text.String(), owned, len(parts) > 0
}

// Only a completed local check emits acceptance. Invalid configuration and failed operations
// stay distinct from candidate rejections, so they cannot consume correction limits.
func publishLocalResult[T any](ctx *gent.ExecutionContext, name string,
	result *gent.OutputResult[T], err error,
) {
	var steering []gent.ContentPart
	if result != nil {
		steering = result.Steering
	}
	ctx.PublishOutputValidation(gent.OutputValidationLocal, name,
		err == nil && result != nil && result.Accepted, steering, err)
}

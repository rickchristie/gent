package gent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// ToolRecoveryStatus states the evidence found about an interrupted call. No value permits
// automatic execution. Gent sends the report to the model, which selects the next action.
type ToolRecoveryStatus string

const (
	// ToolRecoveryCompleted means the original response is available, not that it remains current.
	ToolRecoveryCompleted ToolRecoveryStatus = "completed"
	// ToolRecoveryNotStarted means evidence establishes that the original action did not start.
	ToolRecoveryNotStarted ToolRecoveryStatus = "not_started"
	// ToolRecoveryUnresolved means the outcome cannot be confirmed, including an active remote job.
	ToolRecoveryUnresolved ToolRecoveryStatus = "unresolved"
)

// ToolRecoveryRequest retains original identity, arguments, and effective typed input. The views
// are read-only. Gent restores I before invoking Recover and does not run input hooks again.
// A missing binding or incompatible input becomes a recovery error, not NotStarted.
type ToolRecoveryRequest[I any] struct {
	identity ToolIdentity
	name     string
	args     map[string]any
	input    I
}

// NewToolRecoveryRequest takes ownership of args and input. Tools normally receive a request
// from Gent; custom chains use this constructor when binding the saved input to the current tool.
func NewToolRecoveryRequest[I any](identity ToolIdentity, name string, args map[string]any,
	input I,
) *ToolRecoveryRequest[I] {
	return &ToolRecoveryRequest[I]{identity: identity, name: name, args: args, input: input}
}

// Identity returns the original operation identity, including its interrupted attempt.
func (r *ToolRecoveryRequest[I]) Identity() ToolIdentity { return r.identity }

// Name returns the original selected target name, which chooses its current recovery binding.
func (r *ToolRecoveryRequest[I]) Name() string { return r.name }

// Args returns original parsed model arguments as a read-only view.
func (r *ToolRecoveryRequest[I]) Args() map[string]any { return r.args }

// Input returns the saved effective input after the original hooks. Nested values are read-only.
func (r *ToolRecoveryRequest[I]) Input() I { return r.input }

// ToolRecoveryResponse reports evidence without directing execution. Completed requires Execution;
// NotStarted and Unresolved forbid it. Parts and Instructions describe the check independently.
// Returning a report and an error is invalid. Gent saves both privately and uses safe guidance.
type ToolRecoveryResponse struct {
	// Status states what the check can prove. It does not tell Gent to execute the action.
	Status ToolRecoveryStatus
	// Execution is required for Completed and forbidden for other statuses.
	Execution *ToolExecutionResponse
	// Parts contain optional evidence from the check. Gent saves media and data as parts.
	Parts []ContentPart
	// Instructions contain authored model guidance. Gent always includes them for a valid report.
	Instructions string
}

// ToolRecoveryFunc is a typed inspection handler. It must be safe to repeat after interruption.
type ToolRecoveryFunc[I any] func(context.Context, *ToolRecoveryRequest[I]) (
	*ToolRecoveryResponse, error,
)

// ValidateToolExecution checks the response contract, without inspecting domain success or schema.
func ValidateToolExecution(response *ToolExecutionResponse) error {
	if response == nil {
		return errors.New("gent: tool returned no response")
	}
	switch response.Status {
	case ToolExecuted, ToolBlocked, ToolInvalidArguments:
		return ValidateContentParts(response.Parts)
	default:
		return fmt.Errorf("gent: invalid tool status %q", response.Status)
	}
}

// ValidateToolRecovery checks status and field pairing. It never authorizes a retry.
func ValidateToolRecovery(response *ToolRecoveryResponse) error {
	if response == nil {
		return errors.New("gent: tool recovery returned no response")
	}
	switch response.Status {
	case ToolRecoveryCompleted:
		if err := ValidateToolExecution(response.Execution); err != nil {
			return err
		}
	case ToolRecoveryNotStarted, ToolRecoveryUnresolved:
		if response.Execution != nil {
			return errors.New("gent: only completed recovery has execution")
		}
	default:
		return fmt.Errorf("gent: invalid tool recovery status %q", response.Status)
	}
	return ValidateContentParts(response.Parts)
}

type recoveryCodecsKey struct{}

// BindToolRecoveryInput constructs the registered handler's typed request from saved input.
// Chains call it only during Recover, using the context supplied by ExecutionContext.RecoverTool.
// It does not run input hooks or invoke the original tool. Missing types return a per-call error.
func BindToolRecoveryInput(ctx context.Context, typ reflect.Type,
	request *ToolRecoveryRequest[EncodedValue],
) (any, error) {
	codecs, ok := ctx.Value(recoveryCodecsKey{}).(*CodecRegistry)
	if !ok {
		return nil, errors.New("gent: recovery input codec binding is absent")
	}
	value := reflect.New(typ.Elem()).Interface()
	binder, ok := value.(interface {
		bindSaved(*CodecRegistry, *ToolRecoveryRequest[EncodedValue]) error
	})
	if !ok {
		return nil, errors.New("gent: invalid typed recovery request")
	}
	if err := binder.bindSaved(codecs, request); err != nil {
		return nil, err
	}
	return value, nil
}

func (r *ToolRecoveryRequest[I]) bindSaved(codecs *CodecRegistry,
	saved *ToolRecoveryRequest[EncodedValue],
) error {
	input, err := decodeAs[I](codecs, saved.Input())
	if err != nil {
		return err
	}
	r.identity, r.name, r.args, r.input = saved.Identity(), saved.Name(), saved.Args(), input
	return nil
}

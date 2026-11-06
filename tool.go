package gent

import "context"

// Tool is an operation with typed input and an explicit response. Gent validates input, saves
// the effective input after hooks, invokes Call, and prepares a correlated model reply. The
// author supplies domain data and guidance, without serialization or provider message plumbing.
// Configure tools before sharing them between runs. Methods must support concurrent calls.
type Tool[I any] interface {
	// IndexableTool supplies the name and discovery metadata. Empty search metadata is valid.
	IndexableTool

	// Policy supplies model guidance when the tool is advertised. It is not an approval callback.
	// Enforce approval and other domain conditions in Call before an external action.
	Policy() string

	// Strict requests provider argument enforcement where supported. Local checks still apply.
	Strict() bool

	// ParameterSchema describes valid call arguments. Nil permits any JSON object. Gent validates
	// both supplied arguments and effective input after mutable hooks, before invoking Call.
	// Return a read-only schema; use DefineTool for inference and field constraints.
	ParameterSchema() map[string]any

	// OutputSchema optionally describes domain data to the model. It does not describe Gent's
	// statuses or instructions and does not enable local result validation. Adapters use a native
	// field when applicable, otherwise description text. The returned schema is read-only.
	OutputSchema() map[string]any

	// UnrecoverableErrorInstructions supplies safe authored guidance for an unexpected Call error.
	// It receives no raw error. Empty text uses the execution's ToolError setting, then the default.
	// A tool error ends this operation, not the agent run. Expected guidance belongs in the response.
	UnrecoverableErrorInstructions() string

	// Call receives validated input and a context with the stable tool identity. Expected outcomes
	// return a response and nil error. Unexpected operation failures return nil and an error.
	// Returning both a response and an error is a contract violation, not a partial-result feature:
	// Gent saves both for diagnostics but sends only safe error guidance to the model. Nil, nil and
	// invalid responses use that same safe path. Ownership of a returned response passes to Gent.
	Call(context.Context, I) (*ToolExecutionResponse, error)

	// Recover inspects an interrupted operation using its original identity and saved typed input.
	// It must be safe to repeat and must not repeat the original action. Only Completed includes
	// the recovered execution response. All reports return control to the model, without retry.
	// Errors and rejected reports remain private; Gent sends separate recovery-error guidance.
	Recover(context.Context, *ToolRecoveryRequest[I]) (*ToolRecoveryResponse, error)
}

// ToolStatus describes an accepted execution response. It does not classify business success.
// Empty status is invalid. Interrupted-operation uncertainty belongs in ToolRecoveryStatus.
type ToolStatus string

const (
	// ToolExecuted means the tool returned a domain response. The data determines domain success.
	ToolExecuted ToolStatus = "executed"
	// ToolBlocked means a required condition prevented the action. Instructions explain the next step.
	ToolBlocked ToolStatus = "blocked"
	// ToolInvalidArguments means input was rejected. It does not increment tool-error statistics.
	ToolInvalidArguments ToolStatus = "invalid_arguments"
)

// ToolExecutionResponse supplies content and guidance for a requested execution. Gent can also
// construct it before invocation for input rejection or a blocked action. Status shows the case.
// After return, the response and its parts are owned by Gent and must not be changed.
type ToolExecutionResponse struct {
	Status ToolStatus
	// Parts is model-facing only for Executed. Use NewParts to capture data or supply media.
	// For Blocked or InvalidArguments, put required references and guidance in Instructions.
	Parts []ContentPart
	// Instructions is always included when the response is accepted with nil error. An error
	// rejects the entire response, including these instructions, and keeps it only in diagnostics.
	Instructions string
}

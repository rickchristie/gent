package gent

// TerminationStatus indicates the result of checking for termination.
type TerminationStatus int

const (
	// TerminationContinue indicates no answer section was found or it was ignored.
	// The agent should continue to the next iteration.
	TerminationContinue TerminationStatus = iota

	// TerminationAnswerRejected indicates an answer was found but rejected by a validator.
	// The feedback content should be added to the scratchpad for the LLM to reconsider.
	TerminationAnswerRejected

	// TerminationAnswerAccepted indicates an answer was found and accepted.
	// The content is the final parsed and validated answer.
	TerminationAnswerAccepted
)

// TerminationResult contains the result of a termination check.
type TerminationResult struct {
	// Status indicates whether to continue, reject, or accept the answer.
	Status TerminationStatus

	// Content contains:
	// - For AnswerRejected: feedback to display to the LLM
	// - For AnswerAccepted: the final parsed answer
	// - For Continue: typically nil
	Content []ContentPart
}

// AnswerValidator validates the parsed answer content before acceptance.
// Validators can sanitize, validate format/schema, or check business rules.
type AnswerValidator interface {
	// Name returns the validator's name, used for stats tracking.
	// Stats are recorded as gent:answer_rejected_by:<name>.
	Name() string

	// Validate checks if the answer is acceptable.
	// The answer parameter is the parsed content from ParseSection.
	Validate(execCtx *ExecutionContext, answer any) *ValidationResult
}

// ValidationResult contains the result of answer validation.
type ValidationResult struct {
	// Accepted indicates whether the answer passed validation.
	Accepted bool

	// Feedback contains sections to display to the LLM when the answer is rejected.
	// These are formatted and added to the scratchpad/iteration history.
	Feedback []FormattedSection
}

// Termination is a [TextSection] that signals the agent should stop.
// The parsed result represents the final output of the agent.
type Termination interface {
	TextSection

	// ShouldTerminate checks if the given content indicates termination.
	// Returns a TerminationResult indicating whether to continue, reject, or accept.
	ShouldTerminate(execCtx *ExecutionContext, content string) *TerminationResult

	// SetValidator sets the validator to run on parsed answers before acceptance.
	// Pass nil to remove the validator.
	SetValidator(validator AnswerValidator)
}

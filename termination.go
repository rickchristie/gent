package gent

// TerminationStatus indicates the result of checking for termination.
type TerminationStatus int

const (
	// TerminationContinue indicates no answer section was found or it was empty.
	// The agent should continue to the next iteration.
	TerminationContinue TerminationStatus = iota

	// TerminationAnswerRejected indicates an answer was found but rejected by a validator.
	// The feedback content should be added to the scratchpad for the LLM to reconsider.
	// Stats updated: [SCAnswerRejectedTotal], [SCAnswerRejectedBy].
	TerminationAnswerRejected

	// TerminationAnswerAccepted indicates an answer was found and accepted.
	// The content is the final parsed and validated answer.
	TerminationAnswerAccepted
)

// TerminationResult contains the result of a termination check.
type TerminationResult struct {
	// Status indicates whether to continue, reject, or accept the answer.
	Status TerminationStatus

	// Content varies by status:
	//   - AnswerRejected: Feedback sections to display to the LLM
	//   - AnswerAccepted: The final parsed answer (typically as TextContent)
	//   - Continue: Typically nil
	Content []ContentPart
}

// AnswerValidator validates the parsed answer content before acceptance.
//
// Validators can:
//   - Sanitize output (remove PII, fix formatting)
//   - Validate schema/format compliance
//   - Check business rules (e.g., price within limits)
//   - Verify answer addresses the original question
//
// # Implementing a Validator
//
//	type QualityValidator struct{}
//
//	func (v *QualityValidator) Name() string { return "quality_check" }
//
//	func (v *QualityValidator) Validate(execCtx *ExecutionContext, answer any) *ValidationResult {
//	    text, ok := answer.(string)
//	    if !ok || len(text) < 10 {
//	        return &ValidationResult{
//	            Accepted: false,
//	            Feedback: []FormattedSection{
//	                {Name: "error", Content: "Answer too short. Please provide more detail."},
//	            },
//	        }
//	    }
//	    return &ValidationResult{Accepted: true}
//	}
//
// # Chaining Validators
//
// The Termination interface only supports one validator. To chain multiple validators,
// create a composite validator:
//
//	type CompositeValidator struct {
//	    validators []AnswerValidator
//	}
//
//	func (v *CompositeValidator) Validate(execCtx *ExecutionContext, answer any) *ValidationResult {
//	    for _, validator := range v.validators {
//	        result := validator.Validate(execCtx, answer)
//	        if !result.Accepted {
//	            return result // Return first rejection
//	        }
//	    }
//	    return &ValidationResult{Accepted: true}
//	}
type AnswerValidator interface {
	// Name returns the validator's name, used for stats tracking.
	// Stats are recorded as gent:answer_rejected_by:<name>.
	// Use descriptive names like "schema_validator", "quality_check", "pii_filter".
	Name() string

	// Validate checks if the answer is acceptable.
	//
	// The answer parameter is the parsed content from ParseSection (type depends on
	// the Termination implementation - string for Text, typed struct for JSON).
	//
	// The execCtx provides access to stats, iteration info, and the original task.
	Validate(execCtx *ExecutionContext, answer any) *ValidationResult
}

// ValidationResult contains the result of answer validation.
type ValidationResult struct {
	// Accepted indicates whether the answer passed validation.
	// If false, the agent continues iterating with the feedback.
	Accepted bool

	// Feedback contains sections to display to the LLM when rejected.
	// These are formatted by the agent loop and added to the scratchpad.
	//
	// Example:
	//   []FormattedSection{
	//       {Name: "error", Content: "Answer must include a tracking number."},
	//       {Name: "hint", Content: "Use lookup_order to find the tracking number."},
	//   }
	Feedback []FormattedSection
}

// Termination is a [TextSection] that signals when the agent should stop.
//
// # How It Works
//
// Each iteration, the agent loop:
//  1. Parses the LLM output using TextFormat.Parse()
//  2. Extracts content for the termination section (by Name())
//  3. Calls ParseSection to validate/parse the content
//  4. Calls ShouldTerminate to check if the answer is acceptable
//
// # Implementing Termination
//
// Most use cases are covered by termination.Text and termination.JSON[T].
// For custom termination logic, implement:
//
//	type MyTermination struct {
//	    validator AnswerValidator
//	}
//
//	func (t *MyTermination) Name() string { return "answer" }
//	func (t *MyTermination) Guidance() string { return "Write your final answer." }
//
//	func (t *MyTermination) ParseSection(execCtx *ExecutionContext, content string) (any, error) {
//	    // Parse and validate the content format
//	    // Trace errors if parsing fails (see TextSection docs)
//	    return content, nil
//	}
//
//	func (t *MyTermination) ShouldTerminate(execCtx *ExecutionContext, content string) *TerminationResult {
//	    if content == "" {
//	        return &TerminationResult{Status: TerminationContinue}
//	    }
//
//	    // Run validator if set
//	    if t.validator != nil {
//	        validatorName := t.validator.Name()
//
//	        // REQUIRED: Publish validator called event
//	        execCtx.PublishValidatorCalled(validatorName, content)
//
//	        result := t.validator.Validate(execCtx, content)
//	        if !result.Accepted {
//	            // REQUIRED: Publish validator result (stats auto-updated)
//	            execCtx.PublishValidatorResult(validatorName, content, false, result.Feedback)
//
//	            return &TerminationResult{
//	                Status:  TerminationAnswerRejected,
//	                Content: formatFeedback(result.Feedback),
//	            }
//	        }
//
//	        // REQUIRED: Publish validator accepted
//	        execCtx.PublishValidatorResult(validatorName, content, true, nil)
//	    }
//
//	    return &TerminationResult{
//	        Status:  TerminationAnswerAccepted,
//	        Content: []ContentPart{llms.TextContent{Text: content}},
//	    }
//	}
//
//	func (t *MyTermination) SetValidator(v AnswerValidator) { t.validator = v }
//
// # Validator Event Publishing Requirements
//
// Custom Termination implementations MUST publish events when calling validators.
// This enables debugging and observability of the validation flow. Use:
//
//   - [ExecutionContext.PublishValidatorCalled]: Before calling validator
//   - [ExecutionContext.PublishValidatorResult]: After validator returns (with accepted=true/false)
//
// Stats are automatically updated when publishing ValidatorResultEvent.
//
// # Available Implementations
//
//   - termination.Text: Simple text answer (no parsing)
//   - termination.JSON[T]: JSON answer parsed into typed struct T
type Termination interface {
	TextSection

	// ShouldTerminate checks if the given content indicates termination.
	//
	// This is called AFTER ParseSection succeeds (no parse errors).
	// The content parameter is the raw extracted content (same as ParseSection input).
	//
	// Returns:
	//   - TerminationContinue: Empty/invalid content, continue iterating
	//   - TerminationAnswerRejected: Valid content but validator rejected it
	//   - TerminationAnswerAccepted: Valid content, stop iterating
	//
	// Panics if execCtx is nil.
	ShouldTerminate(execCtx *ExecutionContext, content string) *TerminationResult

	// SetValidator sets the validator to run on parsed answers before acceptance.
	// Pass nil to remove the validator (accept any non-empty answer).
	SetValidator(validator AnswerValidator)
}

package termination

import (
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// Text implements [gent.Termination] for plain text answers.
//
// This is the simplest termination type - any non-empty text in the answer section
// triggers termination. Use this when the agent should respond with free-form text.
//
// # Creating and Configuring
//
//	// Create with section name "answer"
//	term := termination.NewText("answer")
//
//	// Add guidance for the model
//	term := termination.NewText("answer").
//	    WithGuidance("Provide a helpful, concise answer to the user's question.")
//
// # Using with Agent
//
//	agent := react.NewAgent(model).
//	    WithTermination(termination.NewText("answer").
//	        WithGuidance("Write your final response to the user."))
//
// # Adding Validation
//
// You can add an [gent.AnswerValidator] to reject answers that don't meet criteria:
//
//	term := termination.NewText("answer")
//	term.SetValidator(&myValidator{})  // Implement gent.AnswerValidator
//
// When a validator rejects an answer, the status is [gent.TerminationAnswerRejected]
// and feedback is provided for the agent to improve its answer.
//
// # Termination Behavior
//
//   - Empty content: Returns [gent.TerminationContinue]
//   - Non-empty content with validation failure: Returns [gent.TerminationAnswerRejected]
//   - Non-empty content passing validation: Returns [gent.TerminationAnswerAccepted]
type Text struct {
	sectionName string
	guidance    string
	validator   gent.AnswerValidator
}

// NewText creates a new Text termination with the given name.
func NewText(name string) *Text {
	return &Text{
		sectionName: name,
		guidance:    "Write your final answer here.",
	}
}

// WithGuidance sets the guidance text for this termination. The guidance appears inside
// the section tags when TextOutputFormat.DescribeStructure() generates the format prompt.
//
// This can be instructions (e.g., "Write your final answer") or examples, or both.
func (t *Text) WithGuidance(guidance string) *Text {
	t.guidance = guidance
	return t
}

// Name returns the section identifier.
func (t *Text) Name() string {
	return t.sectionName
}

// Guidance returns the guidance text for this termination.
func (t *Text) Guidance() string {
	return t.guidance
}

// ParseSection returns the trimmed content as a string.
// Text termination never fails parsing, so no tracing is performed.
func (t *Text) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return strings.TrimSpace(content), nil
}

// SetValidator sets the validator to run on parsed answers before acceptance.
func (t *Text) SetValidator(validator gent.AnswerValidator) {
	t.validator = validator
}

// ShouldTerminate checks if the content indicates termination.
// For Text termination, any non-empty content triggers termination (after validation).
// Panics if execCtx is nil.
func (t *Text) ShouldTerminate(
	execCtx *gent.ExecutionContext,
	content string,
) *gent.TerminationResult {
	if execCtx == nil {
		panic("termination: ShouldTerminate called with nil ExecutionContext")
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}

	// Run validator if set
	if t.validator != nil {
		result := t.validator.Validate(execCtx, trimmed)
		if !result.Accepted {
			// Track rejection stats
			execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedTotal, 1)
			execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedBy+t.validator.Name(), 1)

			// Convert feedback to ContentPart
			var feedback []gent.ContentPart
			for _, section := range result.Feedback {
				formatted := "<" + section.Name + ">\n" + section.Content + "\n</" + section.Name + ">"
				feedback = append(feedback, llms.TextContent{Text: formatted})
			}

			return &gent.TerminationResult{
				Status:  gent.TerminationAnswerRejected,
				Content: feedback,
			}
		}
	}

	return &gent.TerminationResult{
		Status:  gent.TerminationAnswerAccepted,
		Content: []gent.ContentPart{llms.TextContent{Text: trimmed}},
	}
}

package gent

import "fmt"

type ModelStatus string

const (
	ModelStatusAnswer     ModelStatus = "answer"
	ModelStatusToolCalls  ModelStatus = "tool_calls"
	ModelStatusContinue   ModelStatus = "continue"
	ModelStatusIncomplete ModelStatus = "incomplete"
	ModelStatusRefused    ModelStatus = "refused"
	ModelStatusFailed     ModelStatus = "failed"
)

// ModelRefusal preserves only native refusal fields. Empty content is valid when the provider
// blocks a request without an explanation. Ordinary answer text never establishes refusal.
type ModelRefusal struct {
	Code    string
	Content []ContentPart
}

// ModelCompletion describes the provider response. Gent accepts an answer only after Output
// validation and a terminal checkpoint; provider completion alone cannot establish that success.
type ModelCompletion struct {
	Status         ModelStatus
	ProviderReason string
	Refusal        *ModelRefusal
}

// ValidateCompletion fails closed for missing or inconsistent adapter state. Refused and
// incomplete responses can retain partial calls as evidence, but they never authorize execution.
func (choice *ContentChoice) ValidateCompletion() error {
	completion := choice.Completion
	if (completion.Refusal != nil) != (completion.Status == ModelStatusRefused) {
		return fmt.Errorf("gent: completion %q has inconsistent refusal data", completion.Status)
	}
	switch completion.Status {
	case ModelStatusAnswer:
		if len(choice.ToolCalls) > 0 {
			return fmt.Errorf("gent: answer completion contains tool calls")
		}
	case ModelStatusToolCalls:
		if len(choice.ToolCalls) == 0 {
			return fmt.Errorf("gent: tool completion has no calls")
		}
	case ModelStatusContinue:
		if choice.Continuation == nil {
			return fmt.Errorf("gent: continuation has no provider data")
		}
		if len(choice.ToolCalls) > 0 {
			return fmt.Errorf("gent: continuation contains tool calls")
		}
	case ModelStatusIncomplete, ModelStatusRefused, ModelStatusFailed:
		return nil
	default:
		return fmt.Errorf("gent: unknown model completion %q", completion.Status)
	}
	return nil
}

func cloneRefusal(refusal *ModelRefusal) *ModelRefusal {
	if refusal == nil {
		return nil
	}
	copy := *refusal
	copy.Content = CloneContentParts(refusal.Content)
	return &copy
}

package models

import "github.com/rickchristie/gent"

// capturedToolResponse supplies ordinary text or data in test tools. Error cases keep the
// framework's safe-error path; the helper never puts a diagnostic error in content.
func capturedToolResponse(value any, err error) (*gent.ToolExecutionResponse, error) {
	if err != nil {
		return nil, err
	}
	var parts []gent.ContentPart
	if text, ok := value.(string); ok {
		parts = []gent.ContentPart{gent.TextPart(text)}
	} else {
		part, captureErr := gent.DataPart(value)
		if captureErr != nil {
			return nil, captureErr
		}
		parts = []gent.ContentPart{part}
	}
	return &gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts}, nil
}

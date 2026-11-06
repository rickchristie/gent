package gent

// capturedToolResponse supplies ordinary text or data in test tools. Error cases keep the
// framework's safe-error path; the helper never puts a diagnostic error in content.
func capturedToolResponse(value any, err error) (*ToolExecutionResponse, error) {
	if err != nil {
		return nil, err
	}
	var parts []ContentPart
	if text, ok := value.(string); ok {
		parts = []ContentPart{TextPart(text)}
	} else {
		part, captureErr := DataPart(value)
		if captureErr != nil {
			return nil, captureErr
		}
		parts = []ContentPart{part}
	}
	return &ToolExecutionResponse{Status: ToolExecuted, Parts: parts}, nil
}

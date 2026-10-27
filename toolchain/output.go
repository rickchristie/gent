package toolchain

import (
	"encoding/json"

	"github.com/rickchristie/gent"
)

// formatToolOutputJSON formats a tool's text output as JSON. String outputs are returned
// as-is without marshalling — this is critical for tools that return Markdown, prompts, or
// other structured text (e.g., policy search results). Non-string outputs are JSON-marshalled.
func formatToolOutputJSON(output any) (string, error) {
	if str, ok := output.(string); ok {
		return str, nil
	}
	data, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// formatSectionText is a convenience helper that formats a
// single named section using the given TextFormat.
func formatSectionText(
	tf gent.TextFormat, name string, content string,
) string {
	return tf.FormatSections([]gent.FormattedSection{
		{Name: name, Content: content},
	})
}

// publishFailedToolAttempt records a parsed tool call that failed before the tool's Call
// method could run. These attempts still count for tool-call limits because the model chose
// to spend a tool-call turn, even if the name or arguments were invalid.
func publishFailedToolAttempt(
	execCtx *gent.ExecutionContext,
	call *parsedCall,
	err error,
	result *gent.ToolCallResult,
) {

	beforeEvent := execCtx.PublishBeforeToolCall(call.Name, call.Args, gent.WithToolCallId(call.Id))
	result.ToolCallId = beforeEvent.ToolCallId
	result.AttemptId = execCtx.AttemptId()
	result.Input = beforeEvent.Args
	result.SummaryCaptured = true
	execCtx.PublishAfterToolCall(
		call.Name, beforeEvent.Args, result, 0, err,
		gent.WithToolCallId(beforeEvent.ToolCallId),
		gent.WithToolCallSource(beforeEvent.Source),
	)
}

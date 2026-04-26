package toolchain

import (
	"encoding/json"
	"strings"

	"github.com/rickchristie/gent"
	"gopkg.in/yaml.v3"
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

// formatToolOutputYAML formats a tool's text output as YAML. String outputs are returned
// as-is. Non-string outputs are YAML-marshalled and trimmed.
func formatToolOutputYAML(output any) (string, error) {
	if str, ok := output.(string); ok {
		return str, nil
	}
	data, err := yaml.Marshal(output)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
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
	toolName string,
	args any,
	err error,
) {
	if execCtx == nil {
		return
	}
	beforeEvent := execCtx.PublishBeforeToolCall(toolName, args)
	execCtx.PublishAfterToolCall(toolName, beforeEvent.Args, nil, 0, err)
}

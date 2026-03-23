package toolchain

import (
	"encoding/json"
	"strings"

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

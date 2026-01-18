package agents

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed react.tmpl
var reactSystemTemplateContent string

// ReactTemplateData contains the data passed to ReAct templates.
type ReactTemplateData struct {
	// UserSystemPrompt is additional context provided by the user.
	UserSystemPrompt string

	// OutputPrompt explains how to format output sections (from Formatter).
	OutputPrompt string

	// AvailableTools describes available tools (from ToolChain).
	AvailableTools string

	// Termination describes how to terminate (from Termination).
	Termination string

	// UserInput is the original input/task/question.
	UserInput string

	// LoopText contains previous ReAct iterations and observations.
	LoopText string
}

// DefaultReactSystemTemplate is the default template for the ReAct system prompt.
// It explains the Think-Act-Observe loop to the LLM.
//
// The template file is located at agents/react.tmpl
// Users can replace this template via ReactLoop.WithSystemTemplate().
var DefaultReactSystemTemplate = template.Must(
	template.New("react_system").Parse(reactSystemTemplateContent),
)

// ExecuteTemplate executes a template with the given data and returns the result.
func ExecuteTemplate(tmpl *template.Template, data ReactTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

package react

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/rickchristie/gent"
)

//go:embed react.tmpl
var reactSystemTemplateContent string

// ReActTemplateData contains the data passed to ReAct templates.
type ReActTemplateData struct {
	// UserSystemPrompt is additional context provided by the user.
	UserSystemPrompt string

	// OutputPrompt explains how to format output sections (from Formatter).
	// This describes only the format structure (e.g., XML tags) without tool details.
	OutputPrompt string

	// ToolsPrompt describes available tools and how to call them (from ToolChain).
	ToolsPrompt string

	// Time provides access to time-related functions in templates.
	// Use {{.Time.Today}}, {{.Time.Weekday}}, {{.Time.Format "2006-01-02"}}, etc.
	Time gent.TimeProvider
}

// DefaultReActSystemTemplate is the default template for the ReAct system prompt.
// It explains the Think-Act-Observe loop to the LLM.
//
// The template file is located at agents/react/react.tmpl
// Users can replace this template via ReActLoop.WithSystemTemplate().
var DefaultReActSystemTemplate = template.Must(
	template.New("react_system").Parse(reactSystemTemplateContent),
)

// ExecuteTemplate executes a template with the given data and returns the result.
func ExecuteTemplate(tmpl *template.Template, data ReActTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

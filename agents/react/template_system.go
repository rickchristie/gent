package react

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/rickchristie/gent"
)

//go:embed template_system.tmpl
var reactSystemTemplateContent string

//go:embed template_task.tmpl
var reactTaskTemplateContent string

// SystemPromptData contains the data passed to ReAct templates.
type SystemPromptData struct {
	// BehaviorAndContext contains behavior instructions and context provided by the user.
	BehaviorAndContext string

	// CriticalRules contains critical rules that the agent must follow.
	CriticalRules string

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
// The template file is located at agents/react/template_system.tmpl
// Users can replace this template via Agent.WithSystemTemplate().
var DefaultReActSystemTemplate = template.Must(
	template.New("react_system").Parse(reactSystemTemplateContent),
)

// DefaultReActTaskTemplate is the default template for the ReAct task message.
// It combines the task and scratchpad into a user message.
//
// The template file is located at agents/react/template_task.tmpl
// Users can replace this template via Agent.WithTaskTemplate().
var DefaultReActTaskTemplate = template.Must(
	template.New("react_task").Parse(reactTaskTemplateContent),
)

// TaskMessage represents a single message in the conversation history.
type TaskMessage struct {
	// Role is "user" or "agent"
	Role string
	// Content is the message content
	Content string
	// IsMostRecent marks the most recent user message
	IsMostRecent bool
}

// TaskPromptData contains the data passed to the task template.
type TaskPromptData struct {
	// Task is the original task/input provided by the user (for simple single-turn tasks).
	Task string

	// MessageHistory contains the conversation history for multi-turn tasks.
	// When set, the template will render message history instead of just Task.
	MessageHistory []TaskMessage

	// TaskInstruction is the instruction shown after message history (e.g., "Assist the customer!")
	TaskInstruction string

	// ScratchPad contains the formatted history of previous iterations (agent's internal reasoning).
	ScratchPad string
}

// ExecuteTemplate executes a template with the given data and returns the result.
func ExecuteTemplate(tmpl *template.Template, data SystemPromptData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ExecuteTaskTemplate executes a task template with the given data and returns the result.
func ExecuteTaskTemplate(tmpl *template.Template, data TaskPromptData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

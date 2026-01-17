package termination

import "strings"

// Text simply returns the raw text content as the final answer.
type Text struct {
	sectionName string
	prompt      string
}

// NewText creates a new Text termination with default section name "answer".
func NewText() *Text {
	return &Text{
		sectionName: "answer",
		prompt:      "Write your final answer here.",
	}
}

// WithSectionName sets the section name for this termination.
func (t *Text) WithSectionName(name string) *Text {
	t.sectionName = name
	return t
}

// WithPrompt sets the prompt instructions for this termination.
func (t *Text) WithPrompt(prompt string) *Text {
	t.prompt = prompt
	return t
}

// Name returns the section identifier.
func (t *Text) Name() string {
	return t.sectionName
}

// Prompt returns the instructions for what should go in this section.
func (t *Text) Prompt() string {
	return t.prompt
}

// ParseSection returns the trimmed content as a string.
func (t *Text) ParseSection(content string) (any, error) {
	return strings.TrimSpace(content), nil
}

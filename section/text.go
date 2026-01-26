package section

import (
	"strings"

	"github.com/rickchristie/gent"
)

// Text is a simple TextOutputSection that returns the raw text content as-is.
// It never fails parsing, so no tracing is performed.
//
// Use this for sections where the LLM can write free-form text, such as:
//   - Thinking/reasoning sections
//   - Explanation sections
//   - Any section where you don't need structured parsing
type Text struct {
	sectionName string
	prompt      string
}

// NewText creates a new Text section with the given name.
func NewText(name string) *Text {
	return &Text{
		sectionName: name,
		prompt:      "Write your response here.",
	}
}

// WithPrompt sets the prompt instructions for this section.
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
// Text sections never fail parsing, so no tracing is performed.
func (t *Text) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return strings.TrimSpace(content), nil
}

// Compile-time check that Text implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*Text)(nil)

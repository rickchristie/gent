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
	guidance    string
}

// NewText creates a new Text section with the given name.
func NewText(name string) *Text {
	return &Text{
		sectionName: name,
		guidance:    "Write your response here.",
	}
}

// WithGuidance sets the guidance text for this section. The guidance appears inside
// the section tags when TextOutputFormat.DescribeStructure() generates the format prompt.
//
// This can be instructions (e.g., "Think step by step about the problem") or examples
// showing the expected format, or a combination of both.
func (t *Text) WithGuidance(guidance string) *Text {
	t.guidance = guidance
	return t
}

// Name returns the section identifier.
func (t *Text) Name() string {
	return t.sectionName
}

// Guidance returns the guidance text for this section.
func (t *Text) Guidance() string {
	return t.guidance
}

// ParseSection returns the trimmed content as a string.
// Text sections never fail parsing, so no tracing is performed.
func (t *Text) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return strings.TrimSpace(content), nil
}

// Compile-time check that Text implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*Text)(nil)

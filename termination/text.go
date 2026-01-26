package termination

import (
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// Text simply returns the raw text content as the final answer.
type Text struct {
	sectionName string
	guidance    string
}

// NewText creates a new Text termination with the given name.
func NewText(name string) *Text {
	return &Text{
		sectionName: name,
		guidance:    "Write your final answer here.",
	}
}

// WithGuidance sets the guidance text for this termination. The guidance appears inside
// the section tags when TextOutputFormat.DescribeStructure() generates the format prompt.
//
// This can be instructions (e.g., "Write your final answer") or examples, or both.
func (t *Text) WithGuidance(guidance string) *Text {
	t.guidance = guidance
	return t
}

// Name returns the section identifier.
func (t *Text) Name() string {
	return t.sectionName
}

// Guidance returns the guidance text for this termination.
func (t *Text) Guidance() string {
	return t.guidance
}

// ParseSection returns the trimmed content as a string.
// Text termination never fails parsing, so no tracing is performed.
func (t *Text) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return strings.TrimSpace(content), nil
}

// ShouldTerminate checks if the content indicates termination.
// For Text termination, any non-empty content triggers termination.
func (t *Text) ShouldTerminate(content string) []gent.ContentPart {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	return []gent.ContentPart{llms.TextContent{Text: trimmed}}
}

package section

import (
	"strings"

	"github.com/rickchristie/gent"
)

// Text implements [gent.TextSection] for free-form text content.
//
// Text is the simplest section type - it returns raw content as-is without
// any parsing or validation. It never fails, so no parse error tracing occurs.
//
// # Use Cases
//
//   - Thinking/reasoning sections where the model works through a problem
//   - Explanation sections for providing context
//   - Any section where content format doesn't matter
//
// # Creating and Configuring
//
//	// Basic creation
//	thinking := section.NewText("thinking")
//
//	// With guidance for the model
//	thinking := section.NewText("thinking").
//	    WithGuidance("Think step by step about how to solve this problem.")
//
// # Using with Agent
//
// Text sections are typically registered on the TextFormat via the agent:
//
//	// Using the default "thinking" section in react agent
//	agent := react.NewAgent(model)  // Includes thinking section by default
//
//	// Or register custom text sections
//	textFormat := format.NewXML().
//	    RegisterSection(section.NewText("thinking").
//	        WithGuidance("Reason about the problem.")).
//	    RegisterSection(section.NewText("plan").
//	        WithGuidance("List your planned steps."))
type Text struct {
	sectionName string
	guidance    string
}

// NewText creates a new Text section with the given name.
func NewText(name string) *Text {
	return &Text{
		sectionName: name,
		guidance:    "",
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
// If no custom guidance is set, returns a default based on the section name.
func (t *Text) Guidance() string {
	if t.guidance == "" {
		return t.sectionName + " content goes here..."
	}
	return t.guidance
}

// ParseSection returns the trimmed content as a string.
// Text sections never fail parsing, so no tracing is performed.
func (t *Text) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return strings.TrimSpace(content), nil
}

// Compile-time check that Text implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*Text)(nil)

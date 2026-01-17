package gent

// TextOutputSection defines a section within the LLM's text output.
// Each section knows how to describe itself and parse its content.
type TextOutputSection interface {
	// Name returns the section identifier (e.g., "thinking", "action", "answer")
	Name() string

	// Prompt returns instructions for what should go in this section.
	// This is included in the LLM prompt.
	Prompt() string

	// ParseSection parses the raw text content extracted for this section.
	// Returns the parsed result or an error if parsing fails.
	ParseSection(content string) (any, error)
}

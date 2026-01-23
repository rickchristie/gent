package gent

// TextOutputSection defines a section within the LLM's text output. Each section knows how to
// describe itself and parse its content.
//
// The idea is, on each iteration we're asking the LLM to provide outputs, but there are actually
// multiple tasks in one output. For example, in a ReAct agent loop, the first LLM call might
// ask the LLM to provide "Thought" and "Action" sections. The next LLM call might ask for
// either "Observation" or "Final Answer" section.
//
// By defining sections, we can modularize the prompt construction and output parsing logic.
// Each section can provide its own prompt instructions and parsing logic, making it easier
// to compose complex outputs from the LLM.
//
// See: [TextOutputFormat] for how sections are structured in the overall output.
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

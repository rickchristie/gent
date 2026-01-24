package gent

import "errors"

// TextOutputFormat defines how sections are structured in the LLM output.
// It handles the "envelope" - how sections are delimited and extracted.
//
// See: [TextOutputSection] for section definitions.
type TextOutputFormat interface {
	// DescribeStructure generates the prompt explaining the output format structure.
	// It shows the tag/header format with brief placeholders, without including
	// detailed section prompts. Use this when section prompts (like tool descriptions)
	// should be placed elsewhere in the system prompt.
	DescribeStructure(sections []TextOutputSection) string

	// Parse extracts raw content for each section from the LLM output.
	// Returns map of section name -> slice of content strings (supports multiple instances).
	// Sections not present in output will not appear in the map.
	Parse(output string) (map[string][]string, error)
}

// Parse errors
var (
	ErrNoSectionsFound = errors.New("no recognized sections found in output")
	ErrInvalidJSON     = errors.New("invalid JSON in section content")
	ErrInvalidYAML     = errors.New("invalid YAML in section content")
	ErrMissingToolName = errors.New("tool call missing 'tool' field")
	ErrUnknownTool     = errors.New("unknown tool")
	ErrInvalidToolArgs = errors.New("invalid tool arguments")
)

package gent

// TextSection defines a section within the LLM's text output.
//
// # Concept
//
// Custom text workflows can use separate notes, classifications, and answers. TextSection
// defines each section and its parser. The standard ReAct loop uses native tool calls and
// validates final assistant content without a text action envelope.
//
// # Relationships
//
//   - [TextFormat] uses TextSections to know what to parse and how to format
//   - [Termination] implements TextSection for the "answer" section
//
// # Implementing TextSection
//
// For most cases, use the section package implementations:
//
//	// Simple text passthrough
//	thinking := section.NewText("thinking").WithGuidance("Think step by step")
//
//	// JSON-parsed section with typed output
//	config := section.NewJSON[ConfigSchema]("config").WithGuidance("Provide configuration")
//
// For custom parsing, implement the interface:
//
//	type MySection struct {
//	    name     string
//	    guidance string
//	}
//
//	func (s *MySection) Name() string     { return s.name }
//	func (s *MySection) Guidance() string { return s.guidance }
//
//	func (s *MySection) ParseSection(execCtx *ExecutionContext, content string) (any, error) {
//	    parsed, err := s.doParse(content)
//	    if err != nil {
//	        // Publish error for stats (see Event Requirements below)
//	        if execCtx != nil {
//	            execCtx.PublishParseError("section", content, err)
//	        }
//	        return nil, err
//	    }
//	    // Reset consecutive error gauge on success
//	    if execCtx != nil {
//	        execCtx.Stats().ResetGauge(SGSectionParseErrorConsecutive)
//	    }
//	    return parsed, nil
//	}
//
// # Tracing Requirements
//
// ParseSection MUST handle events for stats tracking:
//
// On parse error:
//   - Call execCtx.PublishParseError() with the appropriate ErrorType
//   - Stats are auto-updated when ParseErrorEvent is published
//
// Supported ErrorTypes and their corresponding keys:
//   - "section": [SGSectionParseErrorConsecutive] (for generic sections)
//   - "termination": [SGTerminationParseErrorConsecutive] (for Termination)
//
// On successful parse:
//   - Call execCtx.Stats().ResetGauge() for the appropriate consecutive error key
//
// Sections that never fail parsing (e.g., simple text passthrough) may skip tracing.
//
// # Available Implementations
//
// The section package provides ready-to-use implementations:
//   - section.Text: Simple passthrough (never fails)
//   - section.JSON[T]: Parse JSON into typed struct T
//   - section.YAML[T]: Parse YAML into typed struct T
//   - section.Schema: Generate guidance from JSON Schema
//
// See: [TextFormat] for how sections are structured in the overall output.
type TextSection interface {
	// Name returns the section identifier (e.g., "notes", "classification", "answer")
	Name() string

	// Guidance returns the guidance text that appears inside this section when
	// TextFormat.DescribeStructure() generates the format prompt for the LLM.
	//
	// This can be either:
	//   - Instructions telling the LLM what to write (e.g., "Write your final answer here")
	//   - Examples showing the expected format (e.g., `{"tool": "search", "query": "..."}`)
	//   - A combination of both
	//
	// The guidance helps the LLM understand what content belongs in this section.
	Guidance() string

	// ParseSection parses the raw text content extracted for this section.
	// Returns the parsed result or an error if parsing fails.
	//
	// The execCtx parameter may be nil (e.g., in unit tests). Implementations should
	// check for nil before tracing or updating stats.
	ParseSection(execCtx *ExecutionContext, content string) (any, error)
}

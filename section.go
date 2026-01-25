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
//
// # Tracing Requirements for Implementors
//
// Implementations MUST handle the following tracing responsibilities:
//
// On parse error:
//   - Trace a ParseErrorTrace with the appropriate ErrorType for your section
//   - The RawContent should contain the content that failed to parse
//   - Stats are auto-updated when ParseErrorTrace is traced
//
// Supported ErrorTypes and their corresponding keys:
//   - "section": KeySectionParseErrorConsecutive (for generic output sections)
//   - "toolchain": KeyToolchainParseErrorConsecutive (for tool call parsing)
//   - "termination": KeyTerminationParseErrorConsecutive (for termination parsing)
//
// On successful parse:
//   - Call execCtx.Stats().ResetCounter() for the appropriate consecutive error key
//
// Sections that never fail parsing (e.g., simple text passthrough) may skip tracing.
//
// Example (for a generic section):
//
//	func (s *MySection) ParseSection(execCtx *ExecutionContext, content string) (any, error) {
//	    result, err := s.doParse(content)
//	    if err != nil {
//	        if execCtx != nil {
//	            execCtx.Trace(ParseErrorTrace{
//	                ErrorType:  "section",
//	                RawContent: content,
//	                Error:      err,
//	            })
//	        }
//	        return nil, err
//	    }
//	    if execCtx != nil {
//	        execCtx.Stats().ResetCounter(KeySectionParseErrorConsecutive)
//	    }
//	    return result, nil
//	}
//
// The section package provides ready-to-use implementations:
//   - section.Text: Simple passthrough section (never fails)
//   - section.JSON[T]: Parses JSON into type T with schema generation
//   - section.YAML[T]: Parses YAML into type T with schema generation
type TextOutputSection interface {
	// Name returns the section identifier (e.g., "thinking", "action", "answer")
	Name() string

	// Prompt returns instructions for what should go in this section.
	// This is included in the LLM prompt.
	Prompt() string

	// ParseSection parses the raw text content extracted for this section.
	// Returns the parsed result or an error if parsing fails.
	//
	// The execCtx parameter may be nil (e.g., in unit tests). Implementations should
	// check for nil before tracing or updating stats.
	ParseSection(execCtx *ExecutionContext, content string) (any, error)
}

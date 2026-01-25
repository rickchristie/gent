package gent

import "errors"

// TextOutputFormat defines how sections are structured in the LLM output.
// It handles the "envelope" - how sections are delimited and extracted.
//
// See: [TextOutputSection] for section definitions.
//
// # Tracing Requirements for Implementors
//
// Implementations MUST handle the following tracing responsibilities:
//
// On parse error:
//   - Trace a ParseErrorTrace with ErrorType="format"
//   - The RawContent should contain the full output that failed to parse
//   - Stats are auto-updated when ParseErrorTrace is traced
//
// On successful parse:
//   - Call execCtx.Stats().ResetCounter(KeyFormatParseErrorConsecutive)
//
// Example:
//
//	func (f *MyFormat) Parse(execCtx *ExecutionContext, output string) (map[string][]string, error) {
//	    result, err := f.doParse(output)
//	    if err != nil {
//	        if execCtx != nil {
//	            execCtx.Trace(ParseErrorTrace{
//	                ErrorType:  "format",
//	                RawContent: output,
//	                Error:      err,
//	            })
//	        }
//	        return nil, err
//	    }
//	    if execCtx != nil {
//	        execCtx.Stats().ResetCounter(KeyFormatParseErrorConsecutive)
//	    }
//	    return result, nil
//	}
type TextOutputFormat interface {
	// DescribeStructure generates the prompt explaining the output format structure.
	// It shows the tag/header format with brief placeholders, without including
	// detailed section prompts. Use this when section prompts (like tool descriptions)
	// should be placed elsewhere in the system prompt.
	DescribeStructure(sections []TextOutputSection) string

	// Parse extracts raw content for each section from the LLM output.
	// Returns map of section name -> slice of content strings (supports multiple instances).
	// Sections not present in output will not appear in the map.
	//
	// The execCtx parameter may be nil (e.g., in unit tests). Implementations should
	// check for nil before tracing or updating stats.
	Parse(execCtx *ExecutionContext, output string) (map[string][]string, error)
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

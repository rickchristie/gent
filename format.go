package gent

import "errors"

// FormattedSection represents a section with its name and formatted content.
// Used when building observation output via TextFormat.FormatSections.
type FormattedSection struct {
	Name    string
	Content string
}

// TextFormat defines how sections are structured in the LLM output and how to format
// sections for input to the LLM.
//
// It handles the "envelope" - how sections are delimited, extracted, and formatted.
// This interface is used bidirectionally:
//   - Parsing: Extract sections from LLM output (Parse method)
//   - Formatting: Build section-delimited text for LLM input (FormatSection, WrapObservation)
//
// See: [TextSection] for section definitions.
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
type TextFormat interface {
	// RegisterSection adds a section to the format. The section name is used for
	// both DescribeStructure output and Parse recognition.
	// Returns self for chaining.
	RegisterSection(section TextSection) TextFormat

	// DescribeStructure generates the prompt explaining the output format structure.
	// It shows the tag/header format with brief placeholders, without including
	// detailed section prompts. Use this when section prompts (like tool descriptions)
	// should be placed elsewhere in the system prompt.
	//
	// Sections must be registered via RegisterSection before calling this method.
	DescribeStructure() string

	// Parse extracts raw content for each section from the LLM output.
	// Returns map of section name -> slice of content strings (supports multiple instances).
	// Sections not present in output will not appear in the map.
	//
	// The execCtx parameter may be nil (e.g., in unit tests). Implementations should
	// check for nil before tracing or updating stats.
	Parse(execCtx *ExecutionContext, output string) (map[string][]string, error)

	// FormatSection formats a single section with its name and content.
	// The format depends on the implementation (e.g., "# name\ncontent" for Markdown,
	// "<name>\ncontent\n</name>" for XML).
	FormatSection(name string, content string) string

	// WrapObservation wraps the complete observation text with format-specific delimiters.
	// For XML format, this wraps in <observation>...</observation> tags.
	// For Markdown format, this may return the text as-is or add a header.
	//
	// This method should be called after all sections are formatted and joined.
	// If the input is empty, returns an empty string.
	WrapObservation(text string) string
}

// TextOutputFormat is an alias for TextFormat for backward compatibility.
// Deprecated: Use TextFormat instead.
type TextOutputFormat = TextFormat

// Parse errors
var (
	ErrNoSectionsFound = errors.New("no recognized sections found in output")
	ErrInvalidJSON     = errors.New("invalid JSON in section content")
	ErrInvalidYAML     = errors.New("invalid YAML in section content")
	ErrMissingToolName = errors.New("tool call missing 'tool' field")
	ErrUnknownTool     = errors.New("unknown tool")
	ErrInvalidToolArgs = errors.New("invalid tool arguments")
)

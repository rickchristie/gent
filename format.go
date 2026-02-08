package gent

import "errors"

// FormattedSection represents a section with its name, content, and optional children.
//
// Used when building sectioned text via TextFormat.FormatSections.
// Children are rendered recursively, with depth-aware formatting:
//   - XML: Nested tags (<parent><child>...</child></parent>)
//   - Markdown: Increasing header levels (#, ##, ###)
//
// Example:
//
//	sections := []FormattedSection{
//	    {Name: "observation", Content: "Tool executed successfully", Children: []FormattedSection{
//	        {Name: "result", Content: "Order #12345 found"},
//	        {Name: "status", Content: "shipped"},
//	    }},
//	}
type FormattedSection struct {
	Name     string
	Content  string
	Children []FormattedSection
}

// TextFormat defines how sections are structured in LLM output and how to format
// sections for input to the LLM.
//
// # Responsibilities
//
// TextFormat handles the "envelope" - how sections are delimited, extracted, and formatted:
//   - Parsing: Extract sections from LLM output (Parse method)
//   - Formatting: Build section-delimited text for LLM input (FormatSections)
//   - Structure: Generate format description for prompts (DescribeStructure)
//
// The format does NOT define what content goes in each section - that's handled by
// [TextSection] implementations (ToolChain, Termination, custom sections).
//
// # Implementing TextFormat
//
// To create a custom format (e.g., for a new delimiter style):
//
//	type MyFormat struct {
//	    sections []TextSection
//	}
//
//	func (f *MyFormat) RegisterSection(section TextSection) TextFormat {
//	    f.sections = append(f.sections, section)
//	    return f
//	}
//
//	func (f *MyFormat) DescribeStructure() string {
//	    // Generate prompt explaining the format structure
//	    var sb strings.Builder
//	    sb.WriteString("Use the following format:\n")
//	    for _, s := range f.sections {
//	        sb.WriteString(fmt.Sprintf("[%s]: %s\n", s.Name(), s.Guidance()))
//	    }
//	    return sb.String()
//	}
//
//	func (f *MyFormat) Parse(execCtx *ExecutionContext, output string) (map[string][]string, error) {
//	    // Parse sections from output (see tracing requirements below)
//	}
//
//	func (f *MyFormat) FormatSections(sections []FormattedSection) string {
//	    // Format sections for LLM input
//	}
//
// # Event Publishing Requirements
//
// Parse MUST publish events for stats tracking:
//
// On parse error:
//   - Call execCtx.PublishParseError("format", rawContent, err)
//   - Stats are auto-updated when the event is published
//
// On successful parse:
//   - Call execCtx.Stats().ResetGauge(SGFormatParseErrorConsecutive)
//
// Example implementation:
//
//	func (f *MyFormat) Parse(execCtx *ExecutionContext, output string) (map[string][]string, error) {
//	    result, err := f.doParse(output)
//	    if err != nil {
//	        if execCtx != nil {
//	            execCtx.PublishParseError("format", output, err)
//	        }
//	        return nil, err
//	    }
//	    if execCtx != nil {
//	        execCtx.Stats().ResetGauge(SGFormatParseErrorConsecutive)
//	    }
//	    return result, nil
//	}
//
// # Available Implementations
//
//   - format.NewXML(): XML-style tags (<section>content</section>)
//   - format.NewMarkdown(): Markdown headers (# Section)
//
// See: [TextSection] for section definitions.
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

	// FormatSections formats sections recursively with their children.
	// The format depends on the implementation:
	//   - Markdown: Uses increasing header levels (# for depth 1, ## for depth 2, etc.)
	//   - XML: Uses nested tags (<name>content<child>...</child></name>)
	//
	// Sections are joined with format-appropriate separators.
	// For each section, Content is rendered first, then Children recursively.
	FormatSections(sections []FormattedSection) string
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

// Package format provides implementations for parsing and formatting structured LLM output.
//
// # Overview
//
// A TextFormat defines how the agent structures its output and how that output
// is parsed back into sections. The format handles both directions:
//
//  1. DescribeStructure() - Generates instructions for the model
//  2. Parse() - Extracts sections from the model's output
//  3. FormatSections() - Formats tool results as observations
//
// # Available Formats
//
//   - [XML]: XML-style tags (<section>content</section>) - recommended for most use cases
//   - [Markdown]: Markdown headers (# Section) - for markdown-native models
//
// # Choosing a Format
//
// Use [XML] (recommended) when:
//   - You want clear, unambiguous section boundaries
//   - The model might mention section names in content
//   - You need nested section support
//
// Use [Markdown] when:
//   - Working with models trained heavily on markdown
//   - You prefer markdown-style output aesthetics
//   - The sections won't be referenced in content
//
// # Example Usage
//
//	// XML format (recommended)
//	agent := react.NewAgent(model).
//	    WithTextFormat(format.NewXML())
//
//	// Markdown format
//	agent := react.NewAgent(model).
//	    WithTextFormat(format.NewMarkdown())
//
// # Section Registration
//
// Sections are typically registered automatically when you configure
// toolchains and terminations on the agent. The agent calls RegisterSection
// for each component that implements [gent.TextSection].
//
// # Parsing and Error Handling
//
// Both formats publish parse errors via [gent.ParseErrorEvent] when provided
// an ExecutionContext. This enables automatic tracking of consecutive parse
// errors for limit enforcement:
//
//	sections, err := textFormat.Parse(execCtx, llmOutput)
//	if err != nil {
//	    // Error traced, stats updated
//	    // Feed error back to model if within retry limits
//	}
//
// # Custom Formats
//
// Implement [gent.TextFormat] to create custom output formats:
//
//	type MyFormat struct {
//	    sections []gent.TextSection
//	}
//
//	func (f *MyFormat) RegisterSection(s gent.TextSection) gent.TextFormat {
//	    f.sections = append(f.sections, s)
//	    return f
//	}
//
//	func (f *MyFormat) DescribeStructure() string {
//	    // Return format instructions for the model
//	}
//
//	func (f *MyFormat) Parse(
//	    execCtx *gent.ExecutionContext,
//	    output string,
//	) (map[string][]string, error) {
//	    // Parse output into section map
//	    // Publish errors via execCtx.PublishParseError() if execCtx != nil
//	}
//
//	func (f *MyFormat) FormatSections(sections []gent.FormattedSection) string {
//	    // Format sections for observation
//	}
package format

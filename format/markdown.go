package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rickchristie/gent"
)

// Markdown implements [gent.TextFormat] using markdown headers to delimit sections.
//
// Markdown format is suitable for models that are trained on markdown content and
// naturally produce markdown-formatted output. It uses `# SectionName` headers to
// mark section boundaries.
//
// # Creating and Configuring
//
//	textFormat := format.NewMarkdown()
//
// # Registering Sections
//
// Sections are typically registered automatically by the agent when you configure
// toolchains and terminations. You can also register sections manually:
//
//	textFormat := format.NewMarkdown().
//	    RegisterSection(toolchain).
//	    RegisterSection(termination)
//
// # Example LLM Output
//
//	# Thinking
//	I need to search for the weather in Tokyo to answer this question.
//
//	# Action
//	tool: search
//	args:
//	  query: weather in tokyo
//
// # Parsing Behavior
//
// The parser extracts content between headers. Headers are case-insensitive during
// parsing but the original registered name is used in the result map:
//
//	sections, _ := textFormat.Parse(execCtx, llmOutput)
//	// sections["Thinking"] = ["I need to search..."]
//	// sections["Action"] = ["tool: search..."]
//
// # Nested Sections
//
// FormatSections supports hierarchical output with depth-aware headers:
//
//	# ToolResult          (depth 1)
//	## search             (depth 2 - child)
//	{"results": [...]}
//
// # Using with Agent
//
// The agent handles format registration automatically:
//
//	agent := react.NewAgent(model).
//	    WithTextFormat(format.NewMarkdown()).
//	    WithToolChain(toolchain.NewYAML().RegisterTool(searchTool)).
//	    WithTermination(termination.NewText("answer"))
type Markdown struct {
	sections      []gent.TextSection
	knownSections map[string]string // lowercase key -> original name
}

// NewMarkdown creates a new Markdown format.
func NewMarkdown() *Markdown {
	return &Markdown{
		sections:      make([]gent.TextSection, 0),
		knownSections: make(map[string]string),
	}
}

// RegisterSection adds a section to the format.
// If a section with the same name already exists, it is not added again.
// Returns self for chaining.
func (f *Markdown) RegisterSection(section gent.TextSection) gent.TextFormat {
	lowerName := strings.ToLower(section.Name())
	if _, exists := f.knownSections[lowerName]; exists {
		return f // Already registered
	}
	f.sections = append(f.sections, section)
	f.knownSections[lowerName] = section.Name() // Store original name
	return f
}

// FormatSections formats sections recursively with depth-aware markdown headers.
// Root level uses #, children use ##, grandchildren use ###, etc.
// Sections are joined with double newlines.
func (f *Markdown) FormatSections(sections []gent.FormattedSection) string {
	return f.formatSectionsAtDepth(sections, 1)
}

// formatSectionsAtDepth recursively formats sections at the given depth level.
func (f *Markdown) formatSectionsAtDepth(sections []gent.FormattedSection, depth int) string {
	if len(sections) == 0 {
		return ""
	}

	var parts []string
	for _, section := range sections {
		parts = append(parts, f.formatSectionAtDepth(section, depth))
	}
	return strings.Join(parts, "\n\n")
}

// formatSectionAtDepth formats a single section with its children at the given depth.
func (f *Markdown) formatSectionAtDepth(section gent.FormattedSection, depth int) string {
	// Build header with appropriate number of # symbols
	header := strings.Repeat("#", depth) + " " + section.Name

	var parts []string
	parts = append(parts, header)

	// Add content if present
	if section.Content != "" {
		parts = append(parts, section.Content)
	}

	// Format children at next depth level
	if len(section.Children) > 0 {
		childrenText := f.formatSectionsAtDepth(section.Children, depth+1)
		if childrenText != "" {
			parts = append(parts, childrenText)
		}
	}

	return strings.Join(parts, "\n")
}

// DescribeStructure generates the prompt explaining the output format structure.
// It shows the header format with each section's prompt instructions.
func (f *Markdown) DescribeStructure() string {
	if len(f.sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Format your response using markdown headers for each section:\n\n")

	for _, section := range f.sections {
		name := section.Name()
		fmt.Fprintf(&sb, "# %s\n", name)
		fmt.Fprintf(&sb, "%s\n\n", section.Guidance())
	}

	return sb.String()
}

// Parse extracts raw content for each section from the LLM output.
func (f *Markdown) Parse(
	execCtx *gent.ExecutionContext,
	output string,
) (map[string][]string, error) {
	result, err := f.doParse(output)
	if err != nil {
		// Publish parse error event (auto-updates stats)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeFormat, output, err)
		}
		return nil, err
	}

	// Successful parse - reset consecutive error counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
	}

	return result, nil
}

// doParse performs the actual parsing logic.
func (f *Markdown) doParse(output string) (map[string][]string, error) {
	result := make(map[string][]string)

	// Match markdown headers: # SectionName
	headerPattern := regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	matches := headerPattern.FindAllStringSubmatchIndex(output, -1)

	if len(matches) == 0 {
		return nil, gent.ErrNoSectionsFound
	}

	// Extract content between headers
	for i, match := range matches {
		// match[0:1] is the full match, match[2:3] is the section name group
		sectionName := strings.TrimSpace(output[match[2]:match[3]])
		sectionNameLower := strings.ToLower(sectionName)

		// Determine the key for the result map
		// If knownSections is populated, skip unrecognized sections and use original name
		var resultKey string
		if len(f.knownSections) > 0 {
			originalName, exists := f.knownSections[sectionNameLower]
			if !exists {
				continue // Skip sections we don't recognize
			}
			resultKey = originalName
		} else {
			// No known sections - use the name as found in the output
			resultKey = sectionName
		}

		// Content starts after the header line
		contentStart := match[1]

		// Content ends at the next header or end of output
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		} else {
			contentEnd = len(output)
		}

		content := strings.TrimSpace(output[contentStart:contentEnd])
		if content != "" {
			result[resultKey] = append(result[resultKey], content)
		}
	}

	if len(result) == 0 {
		return nil, gent.ErrNoSectionsFound
	}

	return result, nil
}

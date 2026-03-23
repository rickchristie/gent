package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rickchristie/gent"
)

// ErrAmbiguousTags is returned in strict mode when section tags appear inside other sections.
var ErrAmbiguousTags = fmt.Errorf("ambiguous tags: section tag found inside another section")

// ErrUnclosedTag is returned when an opening tag has no matching closing tag.
var ErrUnclosedTag = fmt.Errorf("unclosed tag")

// XML implements [gent.TextFormat] using XML-style tags to delimit sections.
//
// XML format is the recommended default for most agents. The clear opening and
// closing tags make it easy for models to produce correctly structured output,
// and the parser handles edge cases like literal tag names in content.
//
// # Creating and Configuring
//
//	// Standard usage
//	textFormat := format.NewXML()
//
//	// Enable strict mode for debugging ambiguous outputs
//	textFormat := format.NewXML().WithStrict(true)
//
// # Registering Sections
//
// Sections are typically registered automatically by the agent when you configure
// toolchains and terminations. You can also register sections manually:
//
//	textFormat := format.NewXML().
//	    RegisterSection(toolchain).
//	    RegisterSection(termination)
//
// # Example LLM Output
//
//	<thinking>
//	I need to search for the weather in Tokyo to answer this question.
//	</thinking>
//
//	<action>
//	tool: search
//	args:
//	  query: weather in tokyo
//	</action>
//
// # Parsing Behavior
//
// The parser extracts content between matching tags. Tags are case-insensitive
// during parsing but the original registered name is used in the result map:
//
//	sections, _ := textFormat.Parse(execCtx, llmOutput)
//	// sections["thinking"] = ["I need to search..."]
//	// sections["action"] = ["tool: search..."]
//
// # Handling Literal Tags in Content
//
// The parser correctly handles cases where the model mentions tag names literally:
//
//	<thinking>
//	I should provide the answer in the <answer> section.
//	</thinking>
//	<answer>
//	The weather is sunny.
//	</answer>
//
// The parser pairs each closing tag with its nearest preceding opening tag,
// correctly extracting both sections even when `<answer>` appears in thinking.
//
// # Strict Mode
//
// Enable strict mode to detect potentially ambiguous parses:
//
//	textFormat := format.NewXML().WithStrict(true)
//
// In strict mode, Parse returns [ErrAmbiguousTags] if a section's content
// contains another registered section's tags.
//
// # Nested Sections
//
// FormatSections supports hierarchical output with nested tags:
//
//	<tool_result>
//	<search>
//	{"results": [...]}
//	</search>
//	</tool_result>
//
// # Using with Agent
//
// The agent handles format registration automatically:
//
//	agent := react.NewAgent(model).
//	    WithTextFormat(format.NewXML()).
//	    WithToolChain(toolchain.NewYAML().RegisterTool(searchTool)).
//	    WithTermination(termination.NewText("answer"))
type XML struct {
	sections      []gent.TextSection
	knownSections map[string]string // lowercase key -> original name
	strict        bool
}

// NewXML creates a new XML format.
func NewXML() *XML {
	return &XML{
		sections:      make([]gent.TextSection, 0),
		knownSections: make(map[string]string),
	}
}

// WithStrict enables strict mode validation.
// In strict mode, Parse returns an error if there are parsing ambiguities,
// such as section tags appearing inside other sections' content.
func (f *XML) WithStrict(strict bool) *XML {
	f.strict = strict
	return f
}

// RegisterSection adds a section to the format.
// If a section with the same name already exists, it is not added again.
// Returns self for chaining.
func (f *XML) RegisterSection(section gent.TextSection) gent.TextFormat {
	lowerName := strings.ToLower(section.Name())
	if _, exists := f.knownSections[lowerName]; exists {
		return f // Already registered
	}
	f.sections = append(f.sections, section)
	f.knownSections[lowerName] = section.Name() // Store original name
	return f
}

// FormatSections formats sections recursively with XML tags.
// Children are nested within their parent's tags.
// Sections are joined with newlines.
func (f *XML) FormatSections(sections []gent.FormattedSection) string {
	if len(sections) == 0 {
		return ""
	}

	var parts []string
	for _, section := range sections {
		parts = append(parts, f.formatSection(section))
	}
	return strings.Join(parts, "\n")
}

// formatSection formats a single section with its children.
func (f *XML) formatSection(section gent.FormattedSection) string {
	var inner []string

	// Add content if present
	if section.Content != "" {
		inner = append(inner, section.Content)
	}

	// Format children recursively
	if len(section.Children) > 0 {
		childrenText := f.FormatSections(section.Children)
		if childrenText != "" {
			inner = append(inner, childrenText)
		}
	}

	innerContent := strings.Join(inner, "\n")
	return fmt.Sprintf("<%s>\n%s\n</%s>", section.Name, innerContent, section.Name)
}

// DescribeStructure generates the prompt explaining the output format structure.
// It shows the tag format with each section's prompt instructions.
func (f *XML) DescribeStructure() string {
	if len(f.sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Format your response using XML-style tags for each section:\n")
	sb.WriteString("**CRITICAL!**: Your output MUST be valid XML with matched opening and closing tags.\n\n")

	for _, section := range f.sections {
		name := section.Name()
		fmt.Fprintf(&sb, "<%s>\n", name)
		fmt.Fprintf(&sb, "%s\n", section.Guidance())
		fmt.Fprintf(&sb, "</%s>\n", name)
	}

	return sb.String()
}

// Parse extracts raw content for each section from the LLM output.
func (f *XML) Parse(execCtx *gent.ExecutionContext, output string) (map[string][]string, error) {
	result, err := f.doParse(output)
	if err != nil {
		// Publish parse error event (auto-updates stats)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeFormat, output, err)
		}
		return nil, err
	}

	// Successful parse - reset consecutive error gauge
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
	}

	return result, nil
}

// doParse performs the actual parsing logic.
func (f *XML) doParse(output string) (map[string][]string, error) {
	result := make(map[string][]string)

	// For each known section, find matches by pairing closing tags with their nearest
	// preceding opening tags. This handles cases where LLM writes literal tags in content.
	var allRanges []sectionRange
	for lowerName, originalName := range f.knownSections {
		matches, ranges := f.findSectionMatches(output, lowerName)
		for _, content := range matches {
			result[originalName] = append(result[originalName], content)
		}
		allRanges = append(allRanges, ranges...)
	}

	// If no known sections, try to find any XML-style tags
	if len(f.knownSections) == 0 {
		pattern := `(?si)<(\w+)>(.*?)</\1>`
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(output, -1)

		for _, match := range matches {
			if len(match) >= 3 {
				sectionName := strings.ToLower(match[1])
				content := strings.TrimSpace(match[2])
				result[sectionName] = append(result[sectionName], content)
			}
		}
	}

	// Check for unclosed tags — opening tags that appear outside all matched
	// sections and have no matching closing tag. This catches cases where the
	// LLM starts a section (e.g. <answer>) but never closes it. Tags that
	// appear inside other matched sections (e.g. literal "provide <answer>."
	// inside <thinking>) are excluded.
	if unclosed := f.findUnclosedTags(output, allRanges); len(unclosed) > 0 {
		return result, fmt.Errorf(
			"%w: %s", ErrUnclosedTag, strings.Join(unclosed, ", "),
		)
	}

	if len(result) == 0 {
		return nil, gent.ErrNoSectionsFound
	}

	// In strict mode, check for ambiguities
	if f.strict {
		if err := f.validateNoAmbiguities(output, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// sectionRange represents the byte range of a matched section in the output.
type sectionRange struct {
	start int // byte offset of opening tag start
	end   int // byte offset of closing tag end
}

// findUnclosedTags checks for opening tags of known sections that appear
// outside all matched section ranges and have no matching closing tag.
// This avoids false positives from literal tag names inside other sections'
// content (e.g. "provide <answer>." inside <thinking>).
func (f *XML) findUnclosedTags(
	output string, matchedRanges []sectionRange,
) []string {
	var unclosed []string
	for lowerName, originalName := range f.knownSections {
		openPattern := fmt.Sprintf(`(?i)<%s>`, lowerName)
		openRe := regexp.MustCompile(openPattern)
		closePattern := fmt.Sprintf(`(?i)</%s>`, lowerName)
		closeRe := regexp.MustCompile(closePattern)

		opens := openRe.FindAllStringIndex(output, -1)
		closes := closeRe.FindAllStringIndex(output, -1)

		// Filter opens to only those outside all matched ranges.
		var outsideOpens [][]int
		for _, o := range opens {
			if !f.insideAnyRange(o[0], matchedRanges) {
				outsideOpens = append(outsideOpens, o)
			}
		}

		// Filter closes to only those outside all matched ranges.
		var outsideCloses [][]int
		for _, c := range closes {
			if !f.insideAnyRange(c[0], matchedRanges) {
				outsideCloses = append(outsideCloses, c)
			}
		}

		// Pair outside opens with outside closes using the same
		// algorithm as findSectionMatches.
		usedOpens := make(map[int]bool)
		for _, closeMatch := range outsideCloses {
			closeStart := closeMatch[0]
			var bestOpen []int
			for _, openMatch := range outsideOpens {
				if openMatch[1] <= closeStart &&
					!usedOpens[openMatch[0]] {
					bestOpen = openMatch
				}
			}
			if bestOpen != nil {
				usedOpens[bestOpen[0]] = true
			}
		}

		unpaired := len(outsideOpens) - len(usedOpens)
		if unpaired > 0 {
			unclosed = append(unclosed, fmt.Sprintf(
				"<%s> (%d unpaired)", originalName, unpaired,
			))
		}
	}
	return unclosed
}

// insideAnyRange returns true if the byte position falls inside any
// matched section range.
func (f *XML) insideAnyRange(
	pos int, ranges []sectionRange,
) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// findSectionMatches finds all instances of a section by pairing closing tags with their
// nearest preceding opening tags. This handles cases where the LLM writes literal tag names
// in content (e.g., "provide <answer>." inside <thinking>).
// Returns the matched contents and the byte ranges of each matched section (open tag start
// to close tag end) for use in unclosed tag detection.
func (f *XML) findSectionMatches(
	output string, sectionName string,
) ([]string, []sectionRange) {
	var results []string
	var ranges []sectionRange

	// Find all closing tags
	closePattern := fmt.Sprintf(`(?i)</%s>`, sectionName)
	closeRe := regexp.MustCompile(closePattern)
	closeMatches := closeRe.FindAllStringIndex(output, -1)

	if len(closeMatches) == 0 {
		return nil, nil
	}

	// Find all opening tags
	openPattern := fmt.Sprintf(`(?i)<%s>`, sectionName)
	openRe := regexp.MustCompile(openPattern)
	openMatches := openRe.FindAllStringIndex(output, -1)

	if len(openMatches) == 0 {
		return nil, nil
	}

	// For each closing tag, find the LAST opening tag before it that hasn't been used
	// This correctly handles cases like: <thinking>...<answer>...</thinking>...<answer>...</answer>
	usedOpens := make(map[int]bool)

	for _, closeMatch := range closeMatches {
		closeStart := closeMatch[0]

		// Find the LAST opening tag before this closing tag that hasn't been used
		var bestOpen []int
		for _, openMatch := range openMatches {
			openEnd := openMatch[1]
			if openEnd <= closeStart && !usedOpens[openMatch[0]] {
				bestOpen = openMatch // Keep updating to get the LAST one
			}
		}

		if bestOpen != nil {
			usedOpens[bestOpen[0]] = true
			content := output[bestOpen[1]:closeStart]
			trimmed := strings.TrimSpace(content)
			if trimmed != "" {
				results = append(results, trimmed)
			}
			ranges = append(ranges, sectionRange{
				start: bestOpen[0],
				end:   closeMatch[1],
			})
		}
	}

	return results, ranges
}

// validateNoAmbiguities checks if any parsed section's content contains another section's tags.
// This is used in strict mode to detect potentially ambiguous parses.
func (f *XML) validateNoAmbiguities(output string, result map[string][]string) error {
	for sectionName, contents := range result {
		for _, content := range contents {
			for otherSection := range f.knownSections {
				if otherSection == sectionName {
					continue
				}
				// Check if content contains another section's opening or closing tag
				openPattern := fmt.Sprintf(`(?i)<%s>`, otherSection)
				closePattern := fmt.Sprintf(`(?i)</%s>`, otherSection)
				if matched, _ := regexp.MatchString(openPattern, content); matched {
					return fmt.Errorf("%w: <%s> found inside <%s> content",
						ErrAmbiguousTags, otherSection, sectionName)
				}
				if matched, _ := regexp.MatchString(closePattern, content); matched {
					return fmt.Errorf("%w: </%s> found inside <%s> content",
						ErrAmbiguousTags, otherSection, sectionName)
				}
			}
		}
	}
	return nil
}

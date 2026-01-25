package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rickchristie/gent"
)

// ErrAmbiguousTags is returned in strict mode when section tags appear inside other sections.
var ErrAmbiguousTags = fmt.Errorf("ambiguous tags: section tag found inside another section")

// XML uses XML-style tags to delimit sections.
//
// Example output:
//
//	<thinking>
//	I need to search for the weather...
//	</thinking>
//
//	<action>
//	{"tool": "search", "args": {"query": "weather"}}
//	</action>
type XML struct {
	knownSections map[string]bool
	strict        bool
}

// NewXML creates a new XML format.
func NewXML() *XML {
	return &XML{
		knownSections: make(map[string]bool),
	}
}

// WithStrict enables strict mode validation.
// In strict mode, Parse returns an error if there are parsing ambiguities,
// such as section tags appearing inside other sections' content.
func (f *XML) WithStrict(strict bool) *XML {
	f.strict = strict
	return f
}

// DescribeStructure generates the prompt explaining only the format structure.
// It shows the tag format with brief placeholders, without including detailed section prompts.
func (f *XML) DescribeStructure(sections []gent.TextOutputSection) string {
	if len(sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Format your response using XML-style tags for each section:\n\n")

	for _, section := range sections {
		name := section.Name()
		f.knownSections[strings.ToLower(name)] = true
		fmt.Fprintf(&sb, "<%s>\n", name)
		fmt.Fprintf(&sb, "... %s content here ...\n", name)
		fmt.Fprintf(&sb, "</%s>\n", name)
	}

	return sb.String()
}

// Parse extracts raw content for each section from the LLM output.
func (f *XML) Parse(execCtx *gent.ExecutionContext, output string) (map[string][]string, error) {
	result, err := f.doParse(output)
	if err != nil {
		// Trace parse error (auto-updates stats)
		if execCtx != nil {
			execCtx.Trace(gent.ParseErrorTrace{
				ErrorType:  "format",
				RawContent: output,
				Error:      err,
			})
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
func (f *XML) doParse(output string) (map[string][]string, error) {
	result := make(map[string][]string)

	// For each known section, find matches by pairing closing tags with their nearest
	// preceding opening tags. This handles cases where LLM writes literal tags in content.
	for sectionName := range f.knownSections {
		matches := f.findSectionMatches(output, sectionName)
		for _, content := range matches {
			result[sectionName] = append(result[sectionName], content)
		}
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

// findSectionMatches finds all instances of a section by pairing closing tags with their
// nearest preceding opening tags. This handles cases where the LLM writes literal tag names
// in content (e.g., "provide <answer>." inside <thinking>).
func (f *XML) findSectionMatches(output string, sectionName string) []string {
	var results []string

	// Find all closing tags
	closePattern := fmt.Sprintf(`(?i)</%s>`, sectionName)
	closeRe := regexp.MustCompile(closePattern)
	closeMatches := closeRe.FindAllStringIndex(output, -1)

	if len(closeMatches) == 0 {
		return nil
	}

	// Find all opening tags
	openPattern := fmt.Sprintf(`(?i)<%s>`, sectionName)
	openRe := regexp.MustCompile(openPattern)
	openMatches := openRe.FindAllStringIndex(output, -1)

	if len(openMatches) == 0 {
		return nil
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
		}
	}

	return results
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

package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rickchristie/gent"
)

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
}

// NewXML creates a new XML format.
func NewXML() *XML {
	return &XML{
		knownSections: make(map[string]bool),
	}
}

// Describe generates the prompt section explaining the output format.
func (f *XML) Describe(sections []gent.TextOutputSection) string {
	if len(sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Format your response using XML-style tags for each section:\n\n")

	for _, section := range sections {
		name := section.Name()
		f.knownSections[strings.ToLower(name)] = true
		fmt.Fprintf(&sb, "<%s>\n", name)
		if prompt := section.Prompt(); prompt != "" {
			sb.WriteString(prompt)
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "</%s>\n\n", name)
	}

	return sb.String()
}

// Parse extracts raw content for each section from the LLM output.
func (f *XML) Parse(output string) (map[string][]string, error) {
	result := make(map[string][]string)

	// Match XML-style tags: <name>...</name> (case-insensitive, multiline)
	// We use (?s) to make . match newlines
	for sectionName := range f.knownSections {
		pattern := fmt.Sprintf(`(?si)<%s>(.*?)</%s>`, sectionName, sectionName)
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(output, -1)

		for _, match := range matches {
			if len(match) >= 2 {
				content := strings.TrimSpace(match[1])
				result[sectionName] = append(result[sectionName], content)
			}
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

	return result, nil
}

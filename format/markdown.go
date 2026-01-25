package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rickchristie/gent"
)

// Markdown uses markdown headers to delimit sections.
//
// Example output:
//
//	# Thinking
//	I need to search for the weather...
//
//	# Action
//	{"tool": "search", "args": {"query": "weather"}}
type Markdown struct {
	knownSections map[string]bool
}

// NewMarkdown creates a new Markdown format.
func NewMarkdown() *Markdown {
	return &Markdown{
		knownSections: make(map[string]bool),
	}
}

// DescribeStructure generates the prompt explaining only the format structure.
// It shows the header format with brief placeholders, without including detailed section prompts.
func (f *Markdown) DescribeStructure(sections []gent.TextOutputSection) string {
	if len(sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Format your response using markdown headers for each section:\n\n")

	for _, section := range sections {
		name := section.Name()
		f.knownSections[strings.ToLower(name)] = true
		fmt.Fprintf(&sb, "# %s\n", name)
		fmt.Fprintf(&sb, "... %s content here ...\n\n", name)
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

		// Skip sections we don't recognize (if knownSections is populated)
		if len(f.knownSections) > 0 && !f.knownSections[sectionNameLower] {
			continue
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
			result[sectionNameLower] = append(result[sectionNameLower], content)
		}
	}

	if len(result) == 0 {
		return nil, gent.ErrNoSectionsFound
	}

	return result, nil
}

package format

import (
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

// XML renders tool results and summaries. It has no parser or mutable state.
type XML struct{}

func NewXML() *XML { return &XML{} }

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

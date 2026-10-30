package format

import (
	"strings"

	"github.com/rickchristie/gent"
)

// Markdown renders tool results and summaries. It has no parser or mutable state.
type Markdown struct{}

func NewMarkdown() *Markdown { return &Markdown{} }

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

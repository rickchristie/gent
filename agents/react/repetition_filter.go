package react

import (
	"regexp"
	"strings"
)

// FilterPoisonedText processes raw repeated text before including it in the recovery
// reminder. Implementations should strip dangerous content (XML tags, hallucinated
// output) and return sanitized text. Return empty string to suppress the repeated
// block entirely — the agent will show a generic hallucination warning instead.
type FilterPoisonedText func(text string, poisonKeywords []string) string

// DefaultFilterPoisonedText strips XML tags and checks for poison keywords.
// If any keyword is found in the stripped text, returns empty string (suppressed).
// Otherwise returns the stripped text truncated to maxChars.
func DefaultFilterPoisonedText(text string, poisonKeywords []string) string {
	// Strip all XML tags.
	stripped := xmlTagPattern.ReplaceAllString(text, "")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return ""
	}

	// Check for poison keywords — if present, the text contains hallucinated
	// content that would anchor the model on its own mistakes.
	lower := strings.ToLower(stripped)
	for _, kw := range poisonKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return ""
		}
	}

	// Truncate to a reasonable preview length.
	const maxPreviewChars = 200
	if len(stripped) > maxPreviewChars {
		stripped = stripped[:maxPreviewChars] + "..."
	}
	return stripped
}

var xmlTagPattern = regexp.MustCompile(`<[^>]+>`)

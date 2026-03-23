package toolchain

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/rickchristie/gent"
)

// RegexToolSearcher searches tools using regex pattern matching.
//
// It stores each tool's searchable texts (name, description,
// domain, keywords, categories, synthetic queries) and counts
// regex matches across all texts. Tools are ranked by total
// match count descending.
//
// Invalid regex patterns return a descriptive error that is
// surfaced to the LLM so it can fix the query.
type RegexToolSearcher struct {
	tools          []regexToolEntry
	searchGuidance string
}

type regexToolEntry struct {
	name  string
	texts []string // all searchable texts for this tool
}

const defaultRegexGuidance = "Use regex patterns to " +
	"search tool names, descriptions, keywords, and " +
	"categories. Patterns are case-insensitive. " +
	"Examples: \"order.*status\", \"email|sms\", " +
	"\"^lookup\""

// NewRegexToolSearcher creates a new regex-based search engine.
func NewRegexToolSearcher() *RegexToolSearcher {
	return &RegexToolSearcher{
		searchGuidance: defaultRegexGuidance,
	}
}

// WithSearchGuidance sets custom search guidance text.
func (e *RegexToolSearcher) WithSearchGuidance(
	guidance string,
) *RegexToolSearcher {
	e.searchGuidance = guidance
	return e
}

// Id returns "regex".
func (e *RegexToolSearcher) Id() string {
	return "regex"
}

// SearchGuidance returns instructions for writing regex
// queries. Override with WithSearchGuidance.
func (e *RegexToolSearcher) SearchGuidance() string {
	return e.searchGuidance
}

// IndexAll indexes all provided tools for regex searching.
func (e *RegexToolSearcher) IndexAll(
	tools []gent.IndexableTool,
) error {
	entries := make([]regexToolEntry, len(tools))
	for i, tool := range tools {
		var texts []string
		texts = append(texts, tool.Name())
		texts = append(texts, tool.Description())
		texts = append(texts, tool.Domain())
		texts = append(texts, tool.Keywords()...)
		texts = append(texts, tool.Categories()...)
		texts = append(texts, tool.SyntheticQueries()...)
		entries[i] = regexToolEntry{
			name:  tool.Name(),
			texts: texts,
		}
	}
	e.tools = entries
	return nil
}

// Search finds tools matching the regex pattern.
func (e *RegexToolSearcher) Search(
	_ context.Context,
	query string,
) ([]string, error) {
	re, err := regexp.Compile("(?i)" + query)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid regex pattern %q: %w", query, err,
		)
	}

	type scored struct {
		name  string
		count int
	}

	var results []scored
	for _, entry := range e.tools {
		count := 0
		for _, text := range entry.texts {
			matches := re.FindAllStringIndex(text, -1)
			count += len(matches)
		}
		if count > 0 {
			results = append(results, scored{
				name:  entry.name,
				count: count,
			})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.name
	}
	return names, nil
}

// Compile-time check that RegexToolSearcher implements
// ToolSearcher.
var _ gent.ToolSearcher = (*RegexToolSearcher)(nil)

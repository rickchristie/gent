package toolchain

import (
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

// domainInfo stores aggregated domain metadata for prompt
// generation.
type domainInfo struct {
	name       string
	categories map[string]bool
	catOrder   []string // insertion order
	toolCount  int
}

// buildDomainSummary generates a summary of tool domains,
// their categories, and tool counts. Maintains insertion order
// for deterministic output.
//
// Output format:
//
//   - Customer (tenant, landlord) - 25 tools
//   - Communication (email, SMS) - 12 tools
func buildDomainSummary(
	tools []gent.IndexableTool,
) string {
	var domains []*domainInfo
	domainMap := make(map[string]*domainInfo)

	for _, tool := range tools {
		d := tool.Domain()
		if d == "" {
			continue
		}

		info, exists := domainMap[d]
		if !exists {
			info = &domainInfo{
				name:       d,
				categories: make(map[string]bool),
			}
			domainMap[d] = info
			domains = append(domains, info)
		}
		info.toolCount++

		for _, cat := range tool.Categories() {
			if !info.categories[cat] {
				info.categories[cat] = true
				info.catOrder = append(
					info.catOrder, cat,
				)
			}
		}
	}

	var sb strings.Builder
	for _, info := range domains {
		sb.WriteString("- ")
		sb.WriteString(info.name)
		if len(info.catOrder) > 0 {
			sb.WriteString(" (")
			sb.WriteString(
				strings.Join(info.catOrder, ", "),
			)
			sb.WriteString(")")
		}
		fmt.Fprintf(&sb, " - %d tools\n", info.toolCount)
	}
	return sb.String()
}

// buildSearchToolSchema generates the JSON Schema for the
// built-in search tool. The query_type enum is populated from
// registered engine IDs.
func buildSearchToolSchema(
	engines []gent.ToolSearcher,
) map[string]any {
	engineIDs := make([]any, len(engines))
	for i, eng := range engines {
		engineIDs[i] = eng.Id()
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"query_type": map[string]any{
				"type": "string",
				"enum": engineIDs,
				"description": "Search engine " +
					"to use",
			},
			"page": map[string]any{
				"type":        "integer",
				"default":     1,
				"minimum":     1,
				"description": "Result page number",
			},
		},
		"required": []string{"query", "query_type"},
	}
}

// searchToolName is the name of the built-in search tool.
const searchToolName = "tool_registry_search"

// buildSimpleList generates a flat indented list of tool
// names from the registered IndexableTool slice.
//
// Output format:
//
//   - search_customer
//   - get_policy
func buildSimpleList(
	tools []gent.IndexableTool,
) string {
	var sb strings.Builder
	for _, tool := range tools {
		fmt.Fprintf(&sb, "  - %s\n", tool.Name())
	}
	return sb.String()
}

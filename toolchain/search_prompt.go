package toolchain

import (
	"encoding/json"
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
//	- Customer (tenant, landlord) - 25 tools
//	- Communication (email, SMS) - 12 tools
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
	engines []gent.SearchEngine,
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
				"description": "Result page number",
			},
		},
		"required": []string{"query", "query_type"},
	}
}

// searchToolName is the name of the built-in search tool.
const searchToolName = "tool_registry_search"

// buildSearchToolPrompt builds the full prompt for the
// built-in "Tool Registry Search" tool. It follows the same
// format as JSON.AvailableToolsPrompt().
//
// When hintType is SearchHintSimpleList, the prompt lists all
// tool names. When SearchHintDomainCategories, it shows
// domain/category summaries with tool counts.
func buildSearchToolPrompt(
	tools []gent.IndexableTool,
	engines []gent.SearchEngine,
	schemaMap map[string]any,
	hintType SearchHintType,
) string {
	var sb strings.Builder

	// Tool count + hint (domain summary or simple list)
	if hintType == SearchHintSimpleList {
		fmt.Fprintf(
			&sb, "There are %d tools:\n",
			len(tools),
		)
		sb.WriteString(buildSimpleList(tools))
	} else {
		fmt.Fprintf(
			&sb,
			"There are %d tools across the "+
				"following domains:\n",
			len(tools),
		)
		domainSummary := buildDomainSummary(tools)
		if domainSummary != "" {
			sb.WriteString(domainSummary)
		}
	}

	// Search tool definition
	fmt.Fprintf(
		&sb,
		"\n- %s: Search the tool registry.\n",
		searchToolName,
	)

	// Per-engine search guidance
	sb.WriteString("  Search guidance:\n")
	for _, eng := range engines {
		fmt.Fprintf(
			&sb,
			"  - %s: %s\n",
			eng.Id(),
			eng.SearchGuidance(),
		)
	}

	// Schema
	schemaJSON, err := json.MarshalIndent(
		schemaMap, "  ", "  ",
	)
	if err == nil {
		sb.WriteString("  Parameters: ")
		sb.Write(schemaJSON)
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildSimpleList generates a flat indented list of tool
// names from the registered IndexableTool slice.
//
// Output format:
//
//	  - search_customer
//	  - get_policy
func buildSimpleList(
	tools []gent.IndexableTool,
) string {
	var sb strings.Builder
	for _, tool := range tools {
		fmt.Fprintf(&sb, "  - %s\n", tool.Name())
	}
	return sb.String()
}

// formatToolDedup returns an abbreviated reference for a
// tool that has already been printed in full earlier.
func formatToolDedup(name string) string {
	return fmt.Sprintf(
		"- %s: (see definition above)\n", name,
	)
}

// formatToolDefinitions formats a list of tool definitions
// (name, description, policy, schema) for inclusion in search
// results. Uses the same format as JSON.AvailableToolsPrompt().
func formatToolDefinitions(tools []any) string {
	var sb strings.Builder
	for _, tool := range tools {
		meta, err := GetToolMeta(tool)
		if err != nil {
			continue
		}
		fmt.Fprintf(
			&sb, "- %s: %s\n",
			meta.Name(), meta.Description(),
		)
		if policy := meta.Policy(); policy != "" {
			sb.WriteString("  Policy: ")
			sb.WriteString(policy)
			sb.WriteString("\n")
		}
		if s := meta.Schema(); s != nil {
			schemaJSON, err := json.MarshalIndent(
				s, "  ", "  ",
			)
			if err == nil {
				sb.WriteString("  Parameters: ")
				sb.Write(schemaJSON)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

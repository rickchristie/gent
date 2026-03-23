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
				"description": "Result page number",
			},
		},
		"required": []string{"query", "query_type"},
	}
}

// searchToolName is the name of the built-in search tool.
const searchToolName = "tool_registry_search"

// getSchemaToolName is the name of the built-in get-schema tool.
// Only available when SearchType is SearchGet.
const getSchemaToolName = "get_tool_schema"

// SearchType controls how the search tool presents results.
type SearchType int

const (
	// Search is the default: search results include full tool definitions
	// (name, description, policy, parameter schema, output schema).
	Search SearchType = iota
	// SearchGet returns only tool name + description in search results.
	// A separate get_tool_schema tool is provided to fetch the full schema
	// for a specific tool by name.
	SearchGet
)

// buildSearchToolPrompt builds the full prompt for the
// built-in "Tool Registry Search" tool. It follows the same
// format as JSON.AvailableToolsPrompt().
//
// When hintType is SearchHintSimpleList, the prompt lists all
// tool names. When SearchHintDomainCategories, it shows
// domain/category summaries with tool counts.
//
// pinnedTools are displayed with full definitions alongside
// the search tool. When present, the guidance text changes
// to indicate not all tools are shown.
func buildSearchToolPrompt(
	tools []gent.IndexableTool,
	engines []gent.ToolSearcher,
	schemaMap map[string]any,
	hintType SearchHintType,
	pinnedTools []any,
	printOutputSchema bool,
	searchType SearchType,
) string {
	hasPinned := len(pinnedTools) > 0
	var sb strings.Builder

	// Tool count + hint (domain summary or simple list)
	if hintType == SearchHintSimpleList {
		fmt.Fprintf(
			&sb, "There are %d tools:\n",
			len(tools),
		)
		sb.WriteString(buildSimpleList(tools))
		if hasPinned {
			fmt.Fprintf(
				&sb,
				"Some tools are pinned below. "+
					"Use %s to get other tool "+
					"details.\n",
				searchToolName,
			)
		} else {
			fmt.Fprintf(
				&sb,
				"Use %s to get tool details "+
					"before calling.\n",
				searchToolName,
			)
		}
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
		if hasPinned {
			fmt.Fprintf(
				&sb,
				"Some tools are pinned below. "+
					"Use %s to discover more.\n",
				searchToolName,
			)
		} else {
			fmt.Fprintf(
				&sb,
				"Use %s for tool discovery.\n",
				searchToolName,
			)
		}
	}

	// Search tool definition
	fmt.Fprintf(
		&sb,
		"\n- %s: Search the tool registry.\n",
		searchToolName,
	)

	// Per-engine search guidance
	sb.WriteString("  Tool Search:\n")
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

	// get_tool_schema tool (only in SearchGet mode)
	if searchType == SearchGet {
		fmt.Fprintf(
			&sb,
			"\n- %s: Get the full parameter and output "+
				"schema for a tool by name.\n",
			getSchemaToolName,
		)
		getSchemaJSON, err := json.MarshalIndent(
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool_name": map[string]any{
						"type": "string",
						"description": "The exact tool " +
							"name to get schema for",
					},
				},
				"required": []string{"tool_name"},
			}, "  ", "  ",
		)
		if err == nil {
			sb.WriteString("  Parameters: ")
			sb.Write(getSchemaJSON)
			sb.WriteString("\n")
		}
	}

	// Pinned tool definitions
	if hasPinned {
		sb.WriteString("\n")
		sb.WriteString(formatToolDefinitions(
			pinnedTools, printOutputSchema,
		))
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

// formatToolBrief formats tools with only name and description.
// Used by SearchGet mode where full schemas are fetched separately
// via get_tool_schema.
func formatToolBrief(tools []any) string {
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
	}
	return sb.String()
}

// formatToolDefinitions formats a list of tool definitions
// (name, description, policy, schema) for inclusion in search
// results. Uses the same format as JSON.AvailableToolsPrompt().
// If printOutputSchema is true, includes the output schema.
func formatToolDefinitions(
	tools []any, printOutputSchema bool,
) string {
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
		if printOutputSchema {
			if os := meta.OutputSchema(); os != nil {
				outJSON, err := json.MarshalIndent(
					os, "  ", "  ",
				)
				if err == nil {
					sb.WriteString("  Returns: ")
					sb.Write(outJSON)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

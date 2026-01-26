package react

import (
	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// SystemPromptContext provides data for building system prompts.
type SystemPromptContext struct {
	// Format is the TextFormat used to format sections consistently.
	Format gent.TextFormat

	// BehaviorAndContext contains behavior instructions and context provided by the user.
	BehaviorAndContext string

	// CriticalRules contains critical rules that the agent must follow.
	CriticalRules string

	// OutputPrompt explains how to format output sections (from Formatter).
	OutputPrompt string

	// ToolsPrompt describes available tools and how to call them (from ToolChain).
	ToolsPrompt string

	// Time provides access to time-related functions.
	Time gent.TimeProvider
}

// SystemPromptBuilder builds system prompt messages from the given context.
// It returns a slice of MessageContent, allowing for multi-message system prompts
// or few-shot examples if needed.
type SystemPromptBuilder func(ctx SystemPromptContext) []gent.MessageContent

// reactExplanation is the default ReAct pattern explanation text.
const reactExplanation = `You are an AI assistant that solves problems using the ReAct (Reasoning and Acting) pattern.

## How ReAct Works

You will solve problems through a cycle of:
1. **Think**: Analyze the current situation, reason about what you know, and decide what to do next.
2. **Act**: Take an action by calling one of the available tools.
3. **Observe**: Review the results of your action.

Repeat this cycle until you have enough information to provide a final answer.

## Important Guidelines

- Always think before acting. Explain your reasoning clearly.
- Use tools to gather information. Don't make up facts.
- If a tool call fails, analyze the error and try a different approach.
- When you have sufficient information to answer, provide your final response.
- Be concise but thorough in your reasoning.`

// DefaultSystemPromptBuilder is the default builder for ReAct system prompts.
// It formats all sections using the TextFormat for consistency.
func DefaultSystemPromptBuilder(ctx SystemPromptContext) []gent.MessageContent {
	var sections []gent.FormattedSection

	// Behavior and context (if provided)
	if ctx.BehaviorAndContext != "" {
		sections = append(sections, gent.FormattedSection{
			Name:    "behavior",
			Content: ctx.BehaviorAndContext,
		})
	}

	// ReAct explanation
	sections = append(sections, gent.FormattedSection{
		Name:    "re_act",
		Content: reactExplanation,
	})

	// Critical rules (if provided)
	if ctx.CriticalRules != "" {
		sections = append(sections, gent.FormattedSection{
			Name:    "critical_rules",
			Content: ctx.CriticalRules,
		})
	}

	// Available tools
	if ctx.ToolsPrompt != "" {
		sections = append(sections, gent.FormattedSection{
			Name:    "available_tools",
			Content: ctx.ToolsPrompt,
		})
	}

	// Output format
	if ctx.OutputPrompt != "" {
		sections = append(sections, gent.FormattedSection{
			Name:    "output_format",
			Content: ctx.OutputPrompt,
		})
	}

	systemContent := ctx.Format.FormatSections(sections)

	return []gent.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []gent.ContentPart{llms.TextContent{Text: systemContent}},
		},
	}
}

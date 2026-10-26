package react

import (
	"github.com/rickchristie/gent"
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

	// Time provides access to time-related functions.
	Time gent.TimeProvider
}

// SystemPromptBuilder builds system prompt sections from the given context.
// It returns a slice of FormattedSection that will be formatted by the TextFormat
// and wrapped in a system message. Subscribers can modify these sections via
// BeforeSystemPromptEvent before they are formatted.
type SystemPromptBuilder func(ctx *SystemPromptContext) []gent.FormattedSection

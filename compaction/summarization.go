package compaction

import (
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// SummarizationStrategy compacts the scratchpad by
// summarizing older iterations into a single synthetic
// iteration. Recent iterations are preserved untouched.
//
// This implements both "progressive summarization" and
// "summary buffer hybrid" patterns:
//   - KeepRecent = 0: pure progressive (summarize everything)
//   - KeepRecent > 0: hybrid (keep last N, summarize rest)
//
// Pinned iterations (importance score >=
// ImportanceScorePinned) are always preserved untouched,
// regardless of their position.
//
// # Result Ordering
//
// The compacted scratchpad is ordered as:
//
//	[synthetic] → [pinned...] → [toKeep...]
//
// This ordering cannot preserve the original chronological
// positions of pinned iterations. Summarization collapses
// multiple iterations into one synthetic iteration, so
// pinned iterations that were interspersed among the
// summarized iterations lose their original neighbors.
//
// For example, with keepRecent=2 and iteration 2 pinned:
//
//	Before: [iter0, iter1, iter2(pinned), iter3, iter4, iter5]
//	After:  [synthetic(0,1,3), iter2(pinned), iter4, iter5]
//
// Neither [synthetic, pinned, ...] nor [pinned, synthetic, ...]
// is chronologically correct — the summary covers events
// from both before and after the pinned iteration. No linear
// ordering can faithfully represent this. The only
// theoretically correct approach would be to split the
// summary around each pinned iteration (one summary for
// [0,1], then pinned 2, then another summary for [3]),
// but this requires multiple model calls per compaction
// and adds significant complexity for marginal benefit.
//
// Given that no ordering is perfect, [synthetic, pinned,
// toKeep] is chosen because it produces the best agent
// loop performance:
//
//  1. Context before detail. The synthetic summary
//     establishes what the agent has been doing — the
//     task, progress, and decisions made. When the LLM
//     then encounters pinned iterations, it can interpret
//     them in context. Without that background, pinned
//     iterations appear in a vacuum ("API returns 429
//     for batch > 100" is less useful without knowing the
//     agent is building a batch pipeline).
//
//  2. Pinned text is excluded from the summary input, so
//     the summary provides complementary context — it
//     tells the narrative around the pinned details.
//     Reading narrative first, then seeing the preserved
//     specifics, follows natural information hierarchy:
//     overview → important details → recent activity.
//
//  3. Recency matters most. Recent iterations (toKeep)
//     are placed last where LLMs attend to them most
//     strongly. Both orderings achieve this, but
//     [pinned, synthetic, toKeep] pushes the summary
//     into the middle which is the lowest-attention zone
//     for large contexts.
//
//  4. Scales with pinned count. With many pinned
//     iterations (5-10), [pinned, synthetic, toKeep]
//     front-loads a large block of decontextualized
//     detail before any framing. This gets worse as
//     pinned count grows.
//
// Note: [SlidingWindowStrategy] does not have this
// limitation — it preserves relative ordering because it
// only drops iterations without merging them.
//
// # Multi-Modal Handling
//
// Non-text content parts (images, audio, etc.) in non-pinned
// iterations are dropped during summarization. Only text
// content is extracted and passed to the summarization model.
// If multi-modal content is important, pin the iteration.
//
// # Model Usage
//
// The strategy calls the injected Model to generate
// summaries. Token usage from these calls is tracked in the
// ExecutionContext's stats (via the model's standard event
// publishing). The summarization prompt can be customized.
//
// # Example
//
//	strategy := compaction.NewSummarization(model).
//	    WithKeepRecent(5)
type SummarizationStrategy struct {
	model      gent.Model
	keepRecent int
	prompt     string
}

// NewSummarization creates a SummarizationStrategy with the
// given model.
func NewSummarization(
	model gent.Model,
) *SummarizationStrategy {
	return &SummarizationStrategy{
		model:      model,
		keepRecent: 0,
		prompt:     DefaultSummarizationPrompt,
	}
}

// WithKeepRecent sets the number of recent iterations to
// preserve without summarization. Default is 0 (pure
// progressive summarization).
func (s *SummarizationStrategy) WithKeepRecent(
	n int,
) *SummarizationStrategy {
	s.keepRecent = n
	return s
}

// WithPrompt sets a custom summarization prompt.
// The prompt receives the existing summary (if any) and the
// new messages to summarize via fmt.Sprintf with two %s
// placeholders.
func (s *SummarizationStrategy) WithPrompt(
	prompt string,
) *SummarizationStrategy {
	s.prompt = prompt
	return s
}

// DefaultSummarizationPrompt is the default prompt used
// by [SummarizationStrategy]. Override it with
// [SummarizationStrategy.WithPrompt] to customize.
//
// The prompt takes two fmt.Sprintf placeholders:
//
//	%s — existing summary block (or "None" on first run)
//	%s — formatted new messages to incorporate
//
// # Designing a Custom Summarization Prompt
//
// The default prompt is informed by public implementations
// from Claude Code, Codex CLI, Aider, SWE-agent, MemGPT,
// AutoGen, OpenHands, and Cursor, plus academic work from
// ReSum (Wu 2025), MemGPT (Packer 2023), the "Complexity
// Trap" paper (Lindenbauer, NeurIPS 2025), MEM1 (Zhou,
// NeurIPS 2025 Workshop), and LLMLingua (Microsoft
// Research 2023-2024). The sections below explain the
// design decisions so you can make informed tradeoffs when
// writing your own prompt.
//
// # Handoff Framing
//
// The prompt opens by telling the model that another
// instance of the agent will continue this work using
// only the summary. This "handoff" framing, used by
// Codex CLI, produces better summaries than asking for
// a generic conversation summary. The model writes for
// an audience (the next agent instance) instead of
// summarizing abstractly, which causes it to preserve
// more operational detail — what was tried, what worked,
// what's next — because it understands the reader needs
// to pick up where it left off.
//
// Compare the two framings:
//
//	Generic: "Summarize the conversation so far."
//	Handoff: "Create a summary so another instance can
//	          resume this work."
//
// The handoff version consistently produces summaries
// that include more concrete state (file paths, error
// messages, partial results) and fewer vague narrative
// statements.
//
// # Structured Sections
//
// The prompt requests output in named sections (Task &
// Intent, Progress, Key Decisions, etc.) rather than
// free-form prose. Claude Code uses a 7-section format
// and it is the most detailed production summarization
// prompt publicly available. The evidence strongly
// favors explicit, section-based output over free-form
// prose summaries:
//
//   - Sections act as a checklist — the model is less
//     likely to forget a category of information.
//   - Sections make the summary scannable for the next
//     model instance, improving attention to relevant
//     parts.
//   - Sections create a consistent structure across
//     compaction cycles, making progressive
//     summarization more stable (the model can update
//     each section rather than restructuring the whole
//     summary each time).
//
// When designing your own sections, consider what
// categories of information your agent produces. A
// coding agent needs file paths and error messages. A
// research agent needs source citations and claims. A
// customer service agent needs case IDs and resolution
// status.
//
// # Recency Bias
//
// The prompt instructs the model to include more detail
// for recent activity and less for older completed work.
// Both Claude Code and Aider use this technique. Claude
// Code says "pay special attention to the most recent
// messages." Aider says "include less detail about older
// parts and more detail about the most recent messages."
//
// This works because: (1) recent context is what the
// agent needs most to decide its next action, (2) older
// work is more likely to be completed and only needs
// the conclusion rather than the full trace, and (3)
// LLMs attend more strongly to the end of their context,
// so placing the densest information there aligns with
// the model's natural attention pattern.
//
// The "Current State" section in the default prompt is
// the strongest expression of this — it asks for maximum
// detail about what was happening immediately before the
// checkpoint. Without this, summaries tend to distribute
// detail evenly, which wastes tokens on completed work
// at the expense of in-progress work.
//
// # Preserving Exact Identifiers
//
// The prompt explicitly tells the model to preserve
// exact names, paths, values, function signatures, error
// messages, schemas, URLs, and configuration details.
// This is consensus across every major framework.
//
// Identifiers compress poorly under free-form
// summarization. A model might write "the API endpoint"
// instead of "/api/v2/users/batch", or "the config
// file" instead of "~/.config/app/settings.toml". When
// the next agent instance reads the summary, it cannot
// recover the original identifier. This is especially
// damaging for coding agents where exact file paths and
// function names are required to take any action, but it
// applies to any domain with structured identifiers.
//
// When writing a custom prompt, enumerate the specific
// types of identifiers your agent works with. Generic
// instructions like "preserve important details" are
// less effective than "preserve exact file paths,
// function signatures, and error messages."
//
// # Anti-Drift
//
// The prompt contains two anti-drift guardrails:
//
//  1. "Do not invent new tasks or plan beyond what was
//     asked" (Remaining Work section) — prevents the
//     summarizer from speculating about future work
//     that wasn't part of the user's request.
//
//  2. "Do NOT treat this as a conclusion — the agent's
//     work continues" (Rules section) — prevents the
//     summarizer from writing closing language ("In
//     summary, we accomplished...") which signals
//     completion to the next agent instance and can
//     cause it to think the work is done.
//
// Claude Code adds a third guardrail: verbatim quotes
// from the most recent conversation to prevent task
// interpretation drift across compactions. The default
// prompt uses a lighter version of this ("use verbatim
// quotes" for recent user requests in the Task & Intent
// section).
//
// Aider explicitly warns: "DO NOT conclude the summary
// with language like 'Finally, ...'. Because the
// conversation continues after the summary."
//
// The ReSum paper (Wu 2025) found that asking summaries
// to list information gaps or action plans trapped
// agents in self-verification loops — the agent would
// spend turns verifying the summary's plan instead of
// making forward progress. This is why the default
// prompt does not include a "Next Step" section with
// detailed action plans. "Remaining Work" lists what's
// pending without specifying how to do it.
//
// # Progressive Summarization
//
// When an existing summary is present (from a previous
// compaction), the prompt tells the model to extend and
// update it rather than starting fresh. This is the
// progressive summarization pattern used by LangChain
// and langmem.
//
// Without this instruction, the model often repeats the
// existing summary verbatim then appends new content,
// wasting tokens. With it, the model integrates new
// information into the existing structure — updating
// the Progress section, moving items from Remaining
// Work to Progress, etc.
//
// This also means the summary quality is cumulative.
// Each compaction refines the summary rather than
// restarting from scratch. The tradeoff is that errors
// in early summaries can persist. If an early summary
// mischaracterizes the task, subsequent compactions may
// propagate that error. This is an inherent limitation
// of progressive summarization.
//
// # No Chain-of-Thought in Output
//
// Claude Code asks for an <analysis> block (chain-of-
// thought reasoning) before the summary. This improves
// summary quality because the model "thinks through"
// the conversation before writing the summary. However,
// the default prompt does NOT include this because the
// output becomes the synthetic iteration's content
// directly — analysis text would pollute the scratchpad
// that the agent reads on subsequent iterations. If your
// use case can tolerate the extra tokens (or you strip
// the analysis block before storing), adding a "think
// before you write" step improves quality.
//
// # No Large Code Blocks
//
// Aider's prompt forbids fenced code blocks in
// summaries. The default prompt says "summarize and
// reference instead." Code blocks consume many tokens
// but rarely need to be in the summary verbatim — the
// agent can re-read the file. The exception is when code
// represents critical in-progress state (a half-written
// function, a failing test case). The default prompt
// handles this by asking for "specific outputs, results,
// and in-progress work" in the Current State section
// without explicitly requesting code blocks.
//
// If your agent does not work with files (and therefore
// cannot re-read source material), you may want to relax
// this rule and allow verbatim content in summaries.
//
// # Model Choice
//
// Aider, Cursor, and Devin all use a cheaper or smaller
// model for summarization rather than the primary agent
// model. Aider defaults to GPT-3.5/Haiku with the main
// model as fallback. This is a good practice because
// summarization is a simpler task than the agent's
// primary reasoning, and the cost savings compound
// across many compaction cycles. The [SummarizationStrategy]
// accepts any [gent.Model], so you can inject a
// cheaper model specifically for summarization.
//
// # What to Preserve vs. Discard
//
// Across all frameworks, a consistent hierarchy emerges:
//
// Always preserve:
//   - User's original goals and explicit requests
//   - Current progress and status
//   - Architectural and design decisions with rationale
//   - Unresolved errors and their context
//   - Exact identifiers (paths, names, signatures)
//   - Failing tests and their error messages
//   - Key constraints or discovered limitations
//
// Safe to discard:
//   - Verbose raw tool outputs (raw file contents, long
//     test logs) — the agent can re-read these
//   - Intermediate reasoning traces — keep conclusions
//   - Redundant information repeated across turns
//   - Failed retry attempts — keep the final resolution
//
// The boundary between these depends on whether the
// agent has retrieval capability. MemGPT and Cursor
// allow agents to search their full history after
// compaction, so their summaries can be more aggressive
// about discarding detail. Without retrieval, the
// summary is the only record and must be more
// conservative.
//
// # Token Budget
//
// The default prompt does not specify a target summary
// length. Aider uses small budgets (1,024-2,048 tokens)
// while Claude Code allows the model to write as much as
// needed. Shorter summaries save context space but lose
// more information. If your agent operates under tight
// context constraints, consider adding a length
// instruction like "Keep the summary under N words" to
// your custom prompt. The langmem library defaults to
// max_summary_tokens=256, which is aggressive but
// workable for simple tasks.
//
// # Observation Masking Alternative
//
// The JetBrains/TUM "Complexity Trap" paper (NeurIPS
// 2025) found that deterministic observation masking
// (replacing older tool outputs with placeholders while
// preserving reasoning traces) outperformed LLM
// summarization in 4 of 5 settings at roughly half the
// cost. LLM summarization caused ~15% more turns
// because summaries "smooth over" failure signals,
// causing agents to persist on unproductive paths.
//
// Consider using [SlidingWindowStrategy] (which is
// closer to observation masking) as a first tier, with
// [SummarizationStrategy] as a second tier when the
// sliding window alone is insufficient. The recommended
// approach from the research is: cheap deterministic
// pruning first, LLM summarization second.
const DefaultSummarizationPrompt = `You are creating a ` +
	`continuation checkpoint for an AI agent. Another ` +
	`instance will resume this work using only your ` +
	`summary and any recent iterations retained after ` +
	`compaction. Your summary must enable seamless ` +
	`continuation without loss of critical details.

%s

## New Activity

%s

## Output Format

Write a summary with the following sections. Include ` +
	`more detail for recent activity and less for ` +
	`older completed work.

### Task & Intent
The user's primary request and any refinements or ` +
	`feedback received. For the most recent user ` +
	`requests, use verbatim quotes.

### Progress
What has been accomplished so far. Include specific ` +
	`identifiers: names, values, file paths, function ` +
	`signatures, schemas, URLs, and configuration ` +
	`details. These compress poorly and are critical ` +
	`for continuation.

### Key Decisions & Findings
Important decisions made, constraints discovered, ` +
	`and technical findings that inform future work. ` +
	`Include rationale where it affects next steps.

### Errors & Resolutions
Problems encountered and how they were resolved. ` +
	`Include specific error messages and the fixes ` +
	`applied. Omit this section entirely if none.

### Current State
What was being worked on immediately before this ` +
	`checkpoint. This is the highest-priority section ` +
	`— provide maximum detail about the most recent ` +
	`activity including specific outputs, results, ` +
	`and in-progress work.

### Remaining Work
Pending tasks in priority order. Only include work ` +
	`that was explicitly requested or clearly ` +
	`established as necessary. Do not invent new tasks ` +
	`or plan beyond what was asked.

## Rules
- Preserve exact identifiers: names, paths, values, ` +
	`error messages, and configuration details
- If an existing summary is provided, extend and ` +
	`update it — do not repeat it wholesale
- Do NOT include large verbatim text blocks — ` +
	`summarize and reference instead
- Do NOT treat this as a conclusion — the agent's ` +
	`work continues after this checkpoint
- Write ONLY the summary sections, no preamble or ` +
	`wrapper tags`

// Compact implements gent.CompactionStrategy.
func (s *SummarizationStrategy) Compact(
	execCtx *gent.ExecutionContext,
) error {
	scratchpad := execCtx.Data().GetScratchPad()

	// Separate: pinned, existing summary, summarizable,
	// recent
	var (
		pinned          []*gent.Iteration
		existingSummary *gent.Iteration
		nonPinned       []*gent.Iteration
	)

	// First pass: extract pinned and existing summary
	for _, iter := range scratchpad {
		if isPinned(iter) {
			pinned = append(pinned, iter)
			continue
		}
		if iter.Origin ==
			gent.IterationCompactedSynthetic {
			existingSummary = iter
			continue
		}
		nonPinned = append(nonPinned, iter)
	}

	// Split non-pinned into toSummarize and toKeep
	var toSummarize []*gent.Iteration
	var toKeep []*gent.Iteration

	if s.keepRecent > 0 &&
		len(nonPinned) > s.keepRecent {
		splitIdx := len(nonPinned) - s.keepRecent
		toSummarize = nonPinned[:splitIdx]
		toKeep = nonPinned[splitIdx:]
	} else if s.keepRecent == 0 && len(nonPinned) > 0 {
		toSummarize = nonPinned
	} else {
		// Nothing to summarize
		return nil
	}

	if len(toSummarize) == 0 {
		return nil
	}

	// Build summarization input
	existingText := ""
	if existingSummary != nil {
		existingText = "## Existing Summary\n\n" +
			extractText(existingSummary)
	} else {
		existingText = "## Existing Summary\n\n" +
			"None (first compaction)."
	}

	newMessages := extractTextFromIterations(toSummarize)
	fullPrompt := fmt.Sprintf(
		s.prompt, existingText, newMessages,
	)

	// Call model directly without TextFormat/TextSection.
	// This is a one-shot call where the entire output is
	// the summary — no multi-section parsing needed.
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: fullPrompt},
			},
		},
	}
	streamID := fmt.Sprintf(
		"compaction-summarization-%d",
		execCtx.Iteration(),
	)
	response, err := s.model.GenerateContent(
		execCtx,
		streamID,
		"compaction",
		messages,
	)
	if err != nil {
		return fmt.Errorf(
			"summarization model call: %w", err,
		)
	}

	if len(response.Choices) == 0 {
		return fmt.Errorf(
			"summarization model returned no choices",
		)
	}

	summaryText := response.Choices[0].Content

	// Create synthetic summary iteration
	synthetic := &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeGeneric,
				Parts: []gent.ContentPart{
					llms.TextContent{
						Text: summaryText,
					},
				},
			},
		},
		Origin: gent.IterationCompactedSynthetic,
	}

	// Rebuild scratchpad: synthetic + pinned + toKeep.
	// See "Result Ordering" in the type doc for why this
	// ordering is used and why chronological ordering
	// cannot be preserved.
	result := make(
		[]*gent.Iteration,
		0,
		1+len(pinned)+len(toKeep),
	)
	result = append(result, synthetic)
	result = append(result, pinned...)
	result = append(result, toKeep...)

	execCtx.Data().SetScratchPad(result)
	return nil
}

// extractText extracts all text content from an iteration,
// dropping non-text content parts.
func extractText(iter *gent.Iteration) string {
	var parts []string
	for _, msg := range iter.Messages {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.TextContent); ok {
				parts = append(parts, tc.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractTextFromIterations extracts text from multiple
// iterations, formatting them with iteration markers.
func extractTextFromIterations(
	iterations []*gent.Iteration,
) string {
	var sb strings.Builder
	for i, iter := range iterations {
		fmt.Fprintf(&sb, "### Message %d\n\n", i+1)
		sb.WriteString(extractText(iter))
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Compile-time check.
var _ gent.CompactionStrategy = (*SummarizationStrategy)(nil)

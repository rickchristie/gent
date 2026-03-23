package search

import (
	"strings"
)

// MarkdownChunker splits text into chunks using a Markdown-aware separator hierarchy with
// heading ancestor tracking. This is the recommended default chunker for all text.
//
// # Why Markdown
//
// Markdown has emerged as the de facto intermediate format for document preprocessing in
// 2024-2025 RAG pipelines. Converting documents to Markdown before chunking is the
// recommended path because:
//   - Markdown headings (#, ##, ###) provide natural semantic boundaries for splitting
//   - Markdown uses ~67% fewer tokens than raw HTML while preserving structure
//   - LLMs are extensively trained on Markdown from GitHub, docs, and web content
//   - It degrades gracefully: plain text with no Markdown headings is split using
//     paragraph, sentence, and word boundaries — identical to generic recursive splitting
//
// # Heading Ancestor Tracking
//
// When splitting a document, each chunk gets a compact heading ancestor prefix prepended for
// context. A paragraph under "# Terms of Service > ## Refund Policy" gets:
//
//	h1: Terms of Service | h2: Refund Policy
//	Full refunds are processed within 5-7 days...
//
// This ensures the embedding model captures not just the paragraph content but where it sits
// in the document. Without context, "Full refunds are processed within 5-7 days" is ambiguous.
// The compact "h1: ... | h2: ..." format uses fewer tokens than repeating full Markdown
// heading lines, which matters when ancestor overhead eats into the chunk budget.
//
// Note: the ancestor prefix counts toward the chunk's token budget. For deeply nested
// documents (4 heading levels), the prefix may be ~15-20 tokens, reducing the available
// space for content. The chunker accounts for this automatically when splitting.
//
// # Research Context
//
// "Chunking configuration had comparable or greater influence on retrieval quality than
// embedding model choice" (Vectara, NAACL 2025). Recursive splitting at ~512 tokens with
// zero overlap is the empirically strongest default (Vectara 2025, Chroma 2024, "Chunk Twice
// Embed Once" 2025).
//
// # Table Preprocessing
//
// Markdown tables must be converted to natural language sentences before chunking. Embedding
// models understand the topic of a table but not spatial relationships between cells. A table
// like:
//
//	| Fare Type | Change Fee | Refund |
//	|-----------|-----------|--------|
//	| Flex      | Free      | Full   |
//	| Standard  | $50       | Credit |
//
// Should be converted to:
//
//	Flex fare: changes are free, full refund available.
//	Standard fare: change fee is $50, refund issued as travel credit.
//
// This conversion is the caller's responsibility before passing text to the chunker. Tables
// embedded as-is will be chunked but produce poor embeddings because the model cannot parse
// the row/column structure from the flattened token sequence.
//
// # Separator Hierarchy
//
// The chunker tries separators in order, falling through to finer granularity:
//
//	"\n# "    → top-level headings
//	"\n## "   → second-level headings
//	"\n### "  → third-level headings
//	"\n#### " → fourth-level headings
//	"\n\n"    → paragraphs
//	"\n"      → lines
//	". "      → sentences (period)
//	"! "      → sentences (exclamation)
//	"? "      → sentences (question)
//	" "       → words
type MarkdownChunker struct {
	ChunkSize    int              // target chunk size in tokens
	ChunkOverlap int              // token overlap between chunks (default: 0)
	TokenCount   func(string) int // token counting function (nil = word count fallback)
}

// Chunk splits text into chunks with Markdown heading ancestor tracking. Returns nil for
// empty or whitespace-only text.
func (c *MarkdownChunker) Chunk(text string) []Chunk {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	sections := c.parseSections(text)
	var result []Chunk

	for _, sec := range sections {
		// Build ancestor context: all headings above this section's level.
		ancestorText := sec.ancestorPrefix()

		if ancestorText == "" {
			// No ancestors — just chunk the section content.
			subChunks := c.splitContent(sec.content)
			for _, sc := range subChunks {
				result = append(result, Chunk{Text: sc, Metadata: sec.headingMap()})
			}
		} else {
			// Prepend ancestors to content and check if it fits.
			full := ancestorText + sec.content
			if c.tokenCount(full) <= c.ChunkSize {
				result = append(result, Chunk{Text: full, Metadata: sec.headingMap()})
			} else {
				// Content is too long with ancestors — split content at a reduced budget
				// that reserves space for the ancestor prefix.
				ancestorTokens := c.tokenCount(ancestorText)
				contentBudget := c.ChunkSize - ancestorTokens
				if contentBudget < 1 {
					contentBudget = 1
				}
				subChunker := &MarkdownChunker{
					ChunkSize: contentBudget, TokenCount: c.TokenCount,
				}
				subChunks := subChunker.splitContent(sec.content)
				for _, sc := range subChunks {
					result = append(result, Chunk{
						Text: ancestorText + sc, Metadata: sec.headingMap(),
					})
				}
			}
		}
	}

	// Filter empty chunks and apply overlap.
	var filtered []Chunk
	for _, ch := range result {
		trimmed := strings.TrimSpace(ch.Text)
		if trimmed != "" {
			ch.Text = trimmed
			filtered = append(filtered, ch)
		}
	}

	if c.ChunkOverlap > 0 && len(filtered) > 1 {
		filtered = c.applyOverlap(filtered)
	}
	return filtered
}

// section represents a parsed section of the document with its heading hierarchy.
type section struct {
	content   string   // the section's text content (may include its own heading)
	ancestors []string // heading lines above this section (e.g., ["# Title", "## Section"])
	levels    []int    // heading levels for each ancestor (e.g., [1, 2])
}

// ancestorPrefix returns the heading ancestors as a compact single-line prefix with trailing
// newline, ready to prepend to chunk text. Format: "h1: Title | h2: Section | h3: Sub\n".
// This is more token-efficient than repeating full Markdown heading lines.
func (s section) ancestorPrefix() string {
	if len(s.ancestors) == 0 {
		return ""
	}
	parts := make([]string, len(s.ancestors))
	for i, a := range s.ancestors {
		level := s.levels[i]
		headingText := strings.TrimSpace(strings.TrimLeft(a, "#"))
		parts[i] = "h" + string(rune('0'+level)) + ": " + headingText
	}
	return strings.Join(parts, " | ") + "\n"
}

// headingMap returns the heading hierarchy as metadata.
func (s section) headingMap() map[string]string {
	if len(s.ancestors) == 0 {
		return nil
	}
	m := make(map[string]string, len(s.ancestors))
	for i, a := range s.ancestors {
		level := s.levels[i]
		// Strip the "# " prefix to get just the heading text.
		headingText := strings.TrimSpace(strings.TrimLeft(a, "#"))
		key := "h" + string(rune('0'+level))
		m[key] = headingText
	}
	return m
}

// parseSections splits the document into sections based on Markdown headings and tracks
// the heading hierarchy. Each section knows its content and its ancestor headings.
func (c *MarkdownChunker) parseSections(text string) []section {
	// Split on heading markers. We look for headings at line starts.
	type headingPos struct {
		index int
		level int
		line  string // the full heading line (e.g., "## Refund Policy")
	}

	var headings []headingPos
	lines := strings.Split(text, "\n")
	charIdx := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if level := headingLevel(trimmed); level > 0 {
			headings = append(headings, headingPos{index: charIdx, level: level, line: trimmed})
		}
		charIdx += len(line) + 1 // +1 for the \n
	}

	// If no headings found, treat entire text as one section (plain text fallback).
	if len(headings) == 0 {
		return []section{{content: text}}
	}

	// Build sections between headings.
	var sections []section
	headingStack := make([]string, 5) // index 1-4 for h1-h4
	levelStack := make([]int, 5)

	// Content before the first heading (if any).
	if headings[0].index > 0 {
		preamble := strings.TrimSpace(text[:headings[0].index])
		if preamble != "" {
			sections = append(sections, section{content: preamble})
		}
	}

	for i, h := range headings {
		// Update heading stack: clear all levels >= current.
		for lvl := h.level; lvl <= 4; lvl++ {
			headingStack[lvl] = ""
			levelStack[lvl] = 0
		}
		headingStack[h.level] = h.line
		levelStack[h.level] = h.level

		// Determine section content: from this heading to the next heading (or end).
		start := h.index
		end := len(text)
		if i+1 < len(headings) {
			end = headings[i+1].index
		}
		content := text[start:end]

		// Build ancestor list: all headings above current level.
		var ancestors []string
		var levels []int
		for lvl := 1; lvl < h.level; lvl++ {
			if headingStack[lvl] != "" {
				ancestors = append(ancestors, headingStack[lvl])
				levels = append(levels, lvl)
			}
		}

		sections = append(sections, section{
			content:   strings.TrimRight(content, "\n"),
			ancestors: ancestors,
			levels:    levels,
		})
	}

	return sections
}

// headingLevel returns the Markdown heading level (1-4) or 0 if not a heading.
func headingLevel(line string) int {
	for level := 4; level >= 1; level-- {
		prefix := strings.Repeat("#", level) + " "
		if strings.HasPrefix(line, prefix) {
			return level
		}
	}
	return 0
}

// splitContent splits non-heading content using paragraph → sentence → word separators.
var contentSeparators = []string{"\n\n", "\n", ". ", "! ", "? ", " "}

func (c *MarkdownChunker) splitContent(text string) []string {
	return c.splitRecursive(text, contentSeparators)
}

// splitRecursive splits text using the separator hierarchy. For each separator, it splits
// the text and merges small pieces. If a piece is still too large, it recurses with the
// next separator.
func (c *MarkdownChunker) splitRecursive(text string, separators []string) []string {
	if c.tokenCount(text) <= c.ChunkSize {
		return []string{text}
	}
	if len(separators) == 0 {
		return []string{text}
	}

	sep := separators[0]
	parts := splitKeepSeparator(text, sep)
	if len(parts) <= 1 {
		return c.splitRecursive(text, separators[1:])
	}

	var result []string
	var current strings.Builder
	for _, part := range parts {
		candidate := current.String() + part
		if current.Len() > 0 && c.tokenCount(candidate) > c.ChunkSize {
			buf := current.String()
			if c.tokenCount(buf) > c.ChunkSize {
				result = append(result, c.splitRecursive(buf, separators[1:])...)
			} else {
				result = append(result, buf)
			}
			current.Reset()
		}
		current.WriteString(part)
	}
	if current.Len() > 0 {
		buf := current.String()
		if c.tokenCount(buf) > c.ChunkSize {
			result = append(result, c.splitRecursive(buf, separators[1:])...)
		} else {
			result = append(result, buf)
		}
	}
	return result
}

// applyOverlap adds overlap between consecutive chunks by prepending trailing characters
// from the previous chunk. Overlap is approximated as ChunkOverlap * 4 characters.
func (c *MarkdownChunker) applyOverlap(chunks []Chunk) []Chunk {
	overlapChars := c.ChunkOverlap * 4
	result := make([]Chunk, len(chunks))
	result[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1].Text
		overlap := ""
		if len(prev) > overlapChars {
			overlap = prev[len(prev)-overlapChars:]
		} else {
			overlap = prev
		}
		result[i] = Chunk{
			Text:     strings.TrimSpace(overlap + " " + chunks[i].Text),
			Metadata: chunks[i].Metadata,
		}
	}
	return result
}

// tokenCount returns the token count, falling back to word count if no function is set.
func (c *MarkdownChunker) tokenCount(text string) int {
	if c.TokenCount != nil {
		return c.TokenCount(text)
	}
	return len(strings.Fields(text))
}

// splitKeepSeparator splits text by sep, keeping the separator attached to the preceding
// segment so that rejoining produces the original text.
func splitKeepSeparator(text, sep string) []string {
	parts := strings.Split(text, sep)
	if len(parts) <= 1 {
		return parts
	}
	result := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(parts)-1 {
			result = append(result, part+sep)
		} else {
			result = append(result, part)
		}
	}
	return result
}

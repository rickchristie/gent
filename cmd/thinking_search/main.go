package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/rickchristie/gent/integrationtest/ecommerce"
	"github.com/rickchristie/gent/integrationtest/testutil"
	"github.com/rickchristie/gent/search"
	"github.com/rickchristie/gent/toolchain"
)

func main() {
	// 1. Read all thinking blocks from LOGS_JSON
	allBlocks := extractThinkingBlocks("LOGS_JSON")

	// 2. Setup embedder + ecommerce fixture
	cfg := testutil.DefaultTestConfig()
	embedder := cfg.Embedder
	defer embedder.Close()

	fixture := ecommerce.NewEcommerceFixture(nil, embedder)

	// 3. Setup tool search engine + SearchJSON
	toolSearcher := toolchain.NewFusedToolSearcher(embedder)
	tc := toolchain.NewSearchJSON(
		toolchain.SearchHintDomainCategories,
	).RegisterEngine(toolSearcher)
	fixture.RegisterAllToolsSearch(tc)
	if err := tc.Initialize(); err != nil {
		panic("initialize: " + err.Error())
	}

	// 4. Chunk and search each thinking block
	ctx := context.Background()
	chunker := &search.MarkdownChunker{
		ChunkSize:  256,
		TokenCount: func(s string) int {
			return len(s) / 4
		},
	}

	var sb strings.Builder
	for i, block := range allBlocks {
		chunks := chunker.Chunk(block)

		// Collect tool scores
		toolBest := map[string]float64{}
		for _, chunk := range chunks {
			toolNames, err := toolSearcher.Search(
				ctx, chunk.Text,
			)
			if err != nil {
				continue
			}
			for rank, name := range toolNames {
				score := 1.0 / float64(rank+1)
				if score > toolBest[name] {
					toolBest[name] = score
				}
			}
		}

		// Policy suggestions
		policySuggestion := fixture.PolicySuggestionPrompt(
			ctx, block,
		)

		// Write combined output
		fmt.Fprintf(&sb, "## Thinking Block %d\n\n", i+1)
		sb.WriteString(block)
		sb.WriteString("\n\n")

		// Search results box
		sb.WriteString(
			"### Search Results\n\n",
		)

		// Policies
		sb.WriteString("**Policies:**\n")
		if policySuggestion != "" {
			for _, line := range strings.Split(
				policySuggestion, "\n",
			) {
				if strings.HasPrefix(line, "- id:") {
					sb.WriteString(line)
					sb.WriteString("\n")
				}
			}
		} else {
			sb.WriteString("- (none)\n")
		}

		// Tools
		sb.WriteString("\n**Tools:**\n")
		if len(toolBest) == 0 {
			sb.WriteString("- (none)\n")
		} else {
			topTools := topN(toolBest, 3)
			for _, e := range topTools {
				fmt.Fprintf(
					&sb,
					"- %s (score: %.3f)\n",
					e.id, e.score,
				)
			}
		}

		sb.WriteString("\n---\n\n")
	}

	err := os.WriteFile(
		"THINKING_LIST.md", []byte(sb.String()), 0644,
	)
	if err != nil {
		panic("write: " + err.Error())
	}
	fmt.Printf(
		"Wrote %d thinking blocks with search results "+
			"to THINKING_LIST.md\n",
		len(allBlocks),
	)
}

func extractThinkingBlocks(filename string) []string {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"Warning: cannot read %s: %v\n",
			filename, err,
		)
		return nil
	}

	re := regexp.MustCompile(
		`(?s)<thinking>(.*?)</thinking>`,
	)
	matches := re.FindAllStringSubmatch(
		string(data), -1,
	)

	var blocks []string
	for _, m := range matches {
		text := strings.TrimSpace(m[1])
		if text != "" {
			blocks = append(blocks, text)
		}
	}
	return blocks
}

type entry struct {
	id    string
	score float64
}

func topN(scores map[string]float64, n int) []entry {
	sorted := make([]entry, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, entry{id, score})
	}
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

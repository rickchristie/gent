package react

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepetitionDetector_ExactMatch(t *testing.T) {
	type input struct {
		config RepetitionConfig
		text   string
	}

	type expected struct {
		err error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "no repetition returns nil",
			input: input{
				config: RepetitionConfig{
					Enabled: true, BlockSize: 10,
					ExactThreshold: 3, SimilarThreshold: 100,
					MaxHammingDist: 3,
				},
				text: "abcdefghij" + "klmnopqrst" + "uvwxyz1234",
			},
			expected: expected{err: nil},
		},
		{
			name: "exact block repeated 3 times triggers",
			input: input{
				config: RepetitionConfig{
					Enabled: true, BlockSize: 10,
					ExactThreshold: 3, SimilarThreshold: 100,
					MaxHammingDist: 3,
				},
				text: "abcdefghij" + "abcdefghij" + "abcdefghij",
			},
			expected: expected{err: ErrRepetitionDetected},
		},
		{
			name: "exact block repeated 2 times does not trigger at threshold 3",
			input: input{
				config: RepetitionConfig{
					Enabled: true, BlockSize: 10,
					ExactThreshold: 3, SimilarThreshold: 100,
					MaxHammingDist: 3,
				},
				text: "abcdefghij" + "abcdefghij" + "klmnopqrst",
			},
			expected: expected{err: nil},
		},
		{
			name: "disabled detector returns nil",
			input: input{
				config: RepetitionConfig{Enabled: false},
				text:   strings.Repeat("abcdefghij", 10),
			},
			expected: expected{err: nil},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newRepetitionDetector(tc.input.config)
			result := d.Feed(tc.input.text)
			if tc.expected.err != nil {
				require.NotNil(t, result, "expected detection")
				require.ErrorIs(t, result.Err, tc.expected.err)
			} else {
				require.Nil(t, result, "expected no detection")
			}
		})
	}
}

func TestRepetitionDetector_NearDuplicate(t *testing.T) {
	// Near-duplicate detection uses SimHash with overlapping blocks. We test by
	// constructing paragraphs that differ by one word — the SimHash fingerprints
	// should be close enough to trigger.
	blockSize := 100

	// Build two similar paragraphs: identical except for one word.
	buildParagraph := func(word string) string {
		base := fmt.Sprintf(
			"The customer needs help with their transaction records and payment "+
				"processing for the order that was placed last %s in the system", word)
		if len(base) < blockSize {
			base += strings.Repeat(" ", blockSize-len(base))
		}
		return base[:blockSize]
	}

	para1 := buildParagraph("Monday")
	para2 := buildParagraph("Tuesday")
	para3 := buildParagraph("Wednesday")

	// Measure actual distance to set threshold dynamically.
	sh1 := simHash([]byte(para1))
	sh2 := simHash([]byte(para2))
	dist := hammingDistance(sh1, sh2)

	cfg := RepetitionConfig{
		Enabled: true, BlockSize: blockSize,
		ExactThreshold:   100, // disable exact match
		SimilarThreshold: 3,
		MaxHammingDist:   dist + 2, // slightly above measured distance
	}

	t.Run("similar paragraphs trigger detection", func(t *testing.T) {
		d := newRepetitionDetector(cfg)
		// Feed enough repetitions for overlapping blocks to accumulate matches.
		text := para1 + para2 + para3 + para1 + para2
		result := d.Feed(text)
		require.NotNil(t, result, "should detect near-duplicate loop")
		require.ErrorIs(t, result.Err, ErrRepetitionDetected)
	})

	t.Run("dissimilar text does not trigger", func(t *testing.T) {
		d := newRepetitionDetector(cfg)
		// All unique paragraphs with different content.
		for i := 0; i < 10; i++ {
			unique := fmt.Sprintf("Completely unique paragraph number %d with different "+
				"vocabulary and structure than all others in the set xyzzy", i)
			if len(unique) < blockSize {
				unique += strings.Repeat(".", blockSize-len(unique))
			}
			result := d.Feed(unique[:blockSize])
			require.Nil(t, result, "unique paragraph %d should not trigger", i)
		}
	})
}

func TestRepetitionDetector_CheckAccumulated(t *testing.T) {
	cfg := RepetitionConfig{
		Enabled: true, BlockSize: 10, ExactThreshold: 3,
		SimilarThreshold: 100, MaxHammingDist: 5,
	}

	t.Run("finds repetition with relaxed threshold", func(t *testing.T) {
		d := newRepetitionDetector(cfg)
		// Feed same block twice (below threshold of 3 for streaming detection).
		block := "abcdefghij"
		require.Nil(t, d.Feed(block+block+"klmnopqrst"))
		// Streaming detection didn't trigger (count=2 < threshold=3).
		// But CheckAccumulated uses relaxed threshold (3-1=2).
		result := d.CheckAccumulated()
		require.NotNil(t, result)
		require.ErrorIs(t, result.Err, ErrRepetitionDetected)
	})

	t.Run("returns nil when no repetition", func(t *testing.T) {
		d := newRepetitionDetector(cfg)
		require.Nil(t, d.Feed("abcdefghijklmnopqrst"))
		require.Nil(t, d.CheckAccumulated())
	})
}

func TestRepetitionDetector_IncrementalChunks(t *testing.T) {
	// Simulate streaming: feed text in small chunks that span block boundaries.
	block := "abcdefghij" // 10 chars
	cfg := RepetitionConfig{
		Enabled: true, BlockSize: 10,
		ExactThreshold: 3, SimilarThreshold: 100,
		MaxHammingDist: 3,
	}

	d := newRepetitionDetector(cfg)
	// Feed the same block 3 times in 2-char chunks.
	fullText := block + block + block
	var result *RepetitionResult
	for i := 0; i < len(fullText); i += 2 {
		end := i + 2
		if end > len(fullText) {
			end = len(fullText)
		}
		result = d.Feed(fullText[i:end])
		if result != nil {
			break
		}
	}
	require.NotNil(t, result)
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

func TestHammingDistance(t *testing.T) {
	assert.Equal(t, 0, hammingDistance(0, 0))
	assert.Equal(t, 1, hammingDistance(1, 0))
	assert.Equal(t, 1, hammingDistance(0, 1))
	assert.Equal(t, 64, hammingDistance(0, ^uint64(0)))
	assert.Equal(t, 3, hammingDistance(0b101, 0b010))
}

func TestSimHash_SimilarTexts(t *testing.T) {
	a := []byte("the quick brown fox jumps over the lazy dog today")
	b := []byte("the quick brown fox jumps over the lazy cat today")
	c := []byte("completely different text about weather forecast")

	shA := simHash(a)
	shB := simHash(b)
	shC := simHash(c)

	distAB := hammingDistance(shA, shB)
	distAC := hammingDistance(shA, shC)

	// a and b differ by one word — should be more similar than a and c.
	assert.Less(t, distAB, distAC,
		"similar texts should have lower Hamming distance than different texts (AB=%d, AC=%d)",
		distAB, distAC)
}

func TestSplitWords(t *testing.T) {
	words := splitWords([]byte("  hello  world  foo  "))
	assert.Len(t, words, 3)
	assert.Equal(t, "hello", string(words[0]))
	assert.Equal(t, "world", string(words[1]))
	assert.Equal(t, "foo", string(words[2]))
}

func TestRepetitionDetector_RealisticLoop(t *testing.T) {
	// Simulate the real-world repetition pattern from the user's report: paragraph-level thought loops.
	// The paragraph is padded to exactly DefaultBlockSize to
	// ensure block boundaries align with repetitions.
	cfg := DefaultRepetitionConfig()
	base := `I need to find a way to get transaction IDs. ` +
		`Let's consider the possibility that the purchase ` +
		`is a separate transaction. I need to find a way ` +
		`to search for transactions by amount. The search ` +
		`did not reveal a tool for this. Given the tools, ` +
		`I cannot directly search for transactions by amount or product name. I am stuck on this step.`

	// Pad to exactly block size.
	paragraph := base
	if len(paragraph) < cfg.BlockSize {
		paragraph += strings.Repeat(" ", cfg.BlockSize-len(paragraph))
	} else {
		paragraph = paragraph[:cfg.BlockSize]
	}

	d := newRepetitionDetector(cfg)

	// Feed the paragraph 4 times (simulating a loop). Should
	// trigger on the 3rd repetition (exact match threshold=3).
	var result *RepetitionResult
	for i := 0; i < 4; i++ {
		result = d.Feed(paragraph)
		if result != nil {
			break
		}
	}
	require.NotNil(t, result)
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

// ============================================================================
// Block alignment edge cases — repeating units of various sizes
// ============================================================================

func TestRepetitionDetector_Alignment_RepeatSmallerThanBlock(t *testing.T) {
	// Repeating unit (200 chars) is smaller than block size (400). Two copies of the unit
	// fit in one block. With overlapping windows, the same 400-char block appears when the
	// unit repeats enough times.
	cfg := DefaultRepetitionConfig()
	unit := "I need to find a way to get transaction IDs from the payment gateway but the " +
		"tool_registry_search did not reveal a tool for this specific use case so I am stuck. "
	// Pad/trim to exactly 200 chars.
	if len(unit) > 200 {
		unit = unit[:200]
	} else {
		unit += strings.Repeat(" ", 200-len(unit))
	}

	// Repeat 8 times = 1600 chars. With 400-char blocks at 200-char stride, the detector
	// sees the same 400-char window (2 copies of the unit) multiple times.
	text := strings.Repeat(unit, 8)
	d := newRepetitionDetector(cfg)
	result := d.Feed(text)
	require.NotNil(t, result, "should detect repetition of 200-char unit with 400-char blocks")
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

func TestRepetitionDetector_Alignment_RepeatLargerThanBlock(t *testing.T) {
	// Repeating unit (600 chars) is larger than block size (400). The key scenario from the
	// review: without overlapping blocks, the block boundaries split the unit at different
	// positions each cycle, so hashes differ. With 50% overlap, some window will align.
	cfg := DefaultRepetitionConfig()
	unit := "I need to find a way to get transaction IDs. Let's consider the possibility that " +
		"the Mighty Mouse purchase is a separate transaction that is not linked to an order in " +
		"the get_orders tool, or it's an older order. I need to find a way to search for " +
		"transactions by amount. The tool_registry_search did not reveal a tool for this. Given " +
		"the available tools, I cannot directly search for transactions by amount or product " +
		"name. The gateway_get_tx_detail tool requires a transaction ID. I am blocked on step " +
		"two of the policy. I need to find a tool that can search. "
	// Pad/trim to exactly 600 chars.
	if len(unit) > 600 {
		unit = unit[:600]
	} else {
		unit += strings.Repeat(" ", 600-len(unit))
	}

	// Repeat 4 times = 2400 chars.
	text := strings.Repeat(unit, 4)
	d := newRepetitionDetector(cfg)
	result := d.Feed(text)
	require.NotNil(t, result, "should detect 600-char repeating unit with 400-char blocks")
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

func TestRepetitionDetector_Alignment_RepeatExactlyBlockSize(t *testing.T) {
	// Repeating unit exactly equals block size (400 chars). This is the ideal case —
	// block boundaries align perfectly with repetition boundaries.
	cfg := DefaultRepetitionConfig()
	unit := strings.Repeat("This is a repeating sentence for testing purposes. ", 8)
	if len(unit) > 400 {
		unit = unit[:400]
	} else {
		unit += strings.Repeat(" ", 400-len(unit))
	}

	text := strings.Repeat(unit, 4)
	d := newRepetitionDetector(cfg)
	result := d.Feed(text)
	require.NotNil(t, result, "should detect 400-char unit with 400-char blocks")
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

func TestRepetitionDetector_Alignment_RepeatSlightlyMisaligned(t *testing.T) {
	// Repeating unit is 450 chars — not a multiple of block size (400) and not a multiple
	// of stride (200). This is the hardest case: every block boundary falls at a different
	// offset within the unit. With overlapping windows, eventually a window will capture
	// the same 400-char substring across two repetition cycles.
	cfg := DefaultRepetitionConfig()
	unit := "I need to find transaction IDs but the search tool did not reveal anything useful. " +
		"Let me consider that this might be a separate transaction not linked to any order. " +
		"I should try searching by amount but there is no tool for that. The gateway requires " +
		"a transaction ID which I do not have. I am stuck on this step of the resolution " +
		"procedure and cannot make progress without additional information or tools. "
	if len(unit) > 450 {
		unit = unit[:450]
	} else {
		unit += strings.Repeat(" ", 450-len(unit))
	}

	// Repeat 15 times = 6750 chars. With stride 200 and unit 450, the LCM is 1800.
	// After 3 full LCM cycles (5400 chars), the same 400-char window has appeared 3 times.
	text := strings.Repeat(unit, 15)
	d := newRepetitionDetector(cfg)
	result := d.Feed(text)
	require.NotNil(t, result, "should detect 450-char repeating unit with 400-char blocks")
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

func TestRepetitionDetector_Alignment_VerySmallRepeat(t *testing.T) {
	// Repeating unit (50 chars) much smaller than block size (400). Eight copies fit in one
	// block. The same 400-char block appears repeatedly as more copies accumulate.
	cfg := DefaultRepetitionConfig()
	unit := "I am stuck I am stuck I am stuck stuck stuck. "
	if len(unit) > 50 {
		unit = unit[:50]
	} else {
		unit += strings.Repeat(" ", 50-len(unit))
	}

	// Repeat 30 times = 1500 chars.
	text := strings.Repeat(unit, 30)
	d := newRepetitionDetector(cfg)
	result := d.Feed(text)
	require.NotNil(t, result, "should detect 50-char repeating unit")
	require.ErrorIs(t, result.Err, ErrRepetitionDetected)
}

// ============================================================================
// False positive tests — legitimate content must NOT trigger detection
// ============================================================================

func TestRepetitionDetector_FalsePositive_LongUniqueResponse(t *testing.T) {
	// A long response with unique content across many blocks should never trigger.
	cfg := DefaultRepetitionConfig()
	d := newRepetitionDetector(cfg)

	for i := 0; i < 20; i++ {
		block := fmt.Sprintf(
			"This is unique paragraph number %d with distinct content that discusses topic %d "+
				"in detail. The information here is completely different from all other paragraphs. "+
				"Key fact: %d times %d equals %d. Additional padding to reach the block size "+
				"threshold for proper testing of the detection algorithm boundaries.",
			i, i*7, i, i+1, i*(i+1))
		require.Nil(t, d.Feed(block), "block %d should not trigger", i)
	}
}

func TestRepetitionDetector_FalsePositive_NumberedList(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	d := newRepetitionDetector(cfg)

	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&sb,
			"%d. Step %d: Perform action %d which involves processing item number %d "+
				"through the verification pipeline for quality check %d.\n",
			i, i, i*3, i*7, i*11)
	}
	require.Nil(t, d.Feed(sb.String()))
}

func TestRepetitionDetector_FalsePositive_SimilarStructureDifferentData(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	d := newRepetitionDetector(cfg)

	var sb strings.Builder
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&sb,
			`{"id": %d, "name": "item_%d", "price": %.2f, "category": "cat_%d", `+
				`"description": "Product %d is a high-quality item for use case %d"}`+"\n",
			i, i, float64(i)*9.99, i%5, i, i*3)
	}
	require.Nil(t, d.Feed(sb.String()))
}

func TestRepetitionDetector_FalsePositive_CodeBlocks(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	d := newRepetitionDetector(cfg)

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb,
			"func process%d(input string) (string, error) {\n"+
				"\tif input == \"\" { return \"\", fmt.Errorf(\"input %d is empty\") }\n"+
				"\tresult := transform%d(input)\n"+
				"\tvalidated := validate%d(result)\n"+
				"\treturn fmt.Sprintf(\"output_%d: %%s\", validated), nil\n}\n\n",
			i, i, i, i, i)
	}
	require.Nil(t, d.Feed(sb.String()))
}

// ============================================================================
// Integration test — agent recovers from repetition via format error path
// ============================================================================

func TestAgent_RepetitionDetection_Recovery(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("I am stuck in a loop and cannot proceed. ", 2)
	if len(loopBlock) > cfg.BlockSize {
		loopBlock = loopBlock[:cfg.BlockSize]
	} else {
		loopBlock += strings.Repeat("x", cfg.BlockSize-len(loopBlock))
	}
	loopingResponse := strings.Repeat(loopBlock, 5)

	validResponse := "<thought>\nLet me try a different approach.\n</thought>\n" +
		"<answer>\nThe issue has been resolved.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test task"})
	execCtx := gent.NewExecutionContext(context.Background(), "repetition-test", data)

	// First call: model loops → detected → truncated → format error → LAContinue.
	result, err := agent.Next(execCtx)
	require.NoError(t, err, "agent should not return error on repetition")
	assert.Equal(t, gent.LAContinue, result.Action,
		"agent should continue after repetition detection")

	// Verify loop stats were incremented.
	assert.Equal(t, int64(1),
		execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))

	// Second call: model returns valid answer → agent terminates.
	// Consecutive gauge should reset.
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action,
		"agent should terminate with valid answer")
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"consecutive gauge should reset on successful termination")
}

// ============================================================================
// FilterPoisonedText tests
// ============================================================================

func TestDefaultFilterPoisonedText(t *testing.T) {
	type input struct {
		text     string
		keywords []string
	}

	type expected struct {
		result string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "strips XML tags and returns clean text",
			input: input{
				text:     `<thought>I need to find something</thought>`,
				keywords: []string{"observation"},
			},
			expected: expected{result: "I need to find something"},
		},
		{
			name: "returns empty when poison keyword found in content",
			input: input{
				text:     `The observation shows that the tool returned data`,
				keywords: []string{"observation"},
			},
			expected: expected{result: ""},
		},
		{
			name: "keyword match is case-insensitive",
			input: input{
				text:     "This contains OBSERVATION data",
				keywords: []string{"observation"},
			},
			expected: expected{result: ""},
		},
		{
			name: "truncates long text to preview length",
			input: input{
				text:     strings.Repeat("abcde", 100), // 500 chars
				keywords: nil,
			},
			// TrimSpace is no-op, then truncate at 200 + "..."
			expected: expected{result: strings.Repeat("abcde", 40) + "..."},
		},
		{
			name: "empty text returns empty",
			input: input{text: "", keywords: nil},
			expected: expected{result: ""},
		},
		{
			name: "whitespace-only after stripping returns empty",
			input: input{
				text: "<tag>  </tag>", keywords: nil,
			},
			expected: expected{result: ""},
		},
		{
			name: "no keywords means no poison check",
			input: input{
				text:     "observation is a normal word here",
				keywords: nil,
			},
			expected: expected{
				result: "observation is a normal word here",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DefaultFilterPoisonedText(tc.input.text, tc.input.keywords)
			assert.Equal(t, tc.expected.result, result)
		})
	}
}

// ============================================================================
// Terminate mode test
// ============================================================================

func TestAgent_RepetitionDetection_TerminateImmediately(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	cfg.BlockSize = 50
	cfg.Action = RepetitionTerminate

	loopBlock := strings.Repeat("x", cfg.BlockSize)
	loopingResponse := strings.Repeat(loopBlock, 5)

	model := newMockModel(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: loopingResponse}},
		Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
	})

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "term-test", data)

	_, err := agent.Next(execCtx)
	require.Error(t, err, "should return fatal error in terminate mode")
	assert.Contains(t, err.Error(), "repetition detected")
}

// ============================================================================
// Gauge reset test — tool execution resets consecutive gauge
// ============================================================================

func TestAgent_RepetitionGauge_ResetsOnToolExecution(t *testing.T) {
	cfg := DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("x", cfg.BlockSize)
	loopingResponse := strings.Repeat(loopBlock, 5)

	// Call 1: loop detected (gauge → 1)
	// Call 2: valid tool call (gauge → 0)
	toolCallResponse := "<thought>\nLet me try.\n</thought>\n" +
		"<action>\ntool_name: test_tool\nargs: {}\n</action>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: toolCallResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	tc := newMockToolChain()
	tc.WithResults(&gent.ToolChainResult{
		Text: "<observation>tool result</observation>",
	})

	agent := NewAgent(model).WithRepetitionConfig(cfg).WithToolChain(tc)

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "gauge-test", data)

	// Iteration 1: loop → gauge=1
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))

	// Iteration 2: tool call → gauge reset to 0
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"gauge should reset after successful tool execution")
}

// ============================================================================
// MaxResponseChars two-step check tests
// ============================================================================

func TestAgent_MaxResponseChars_TooLongButNotLooping(t *testing.T) {
	// A long unique response exceeds MaxResponseChars. Since there's no repetition,
	// the agent should inject a "too long" reminder, not a loop reminder.
	// Generate unique text where every paragraph has completely different vocabulary
	// to avoid SimHash near-duplicate false positives across overlapping windows.
	words := []string{
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
		"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
		"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray",
	}
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		w := words[i%len(words)]
		fmt.Fprintf(&sb, "%s%d_%d_%d_%d ", w, i, i*7, i*13, i*31)
	}
	longResponse := sb.String()

	validResponse := "<thought>\nBe concise.\n</thought>\n<answer>\nDone.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: longResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 3000},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithMaxResponseChars(500). // Very low limit to trigger truncation.
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "too-long-test", data)

	// First call: too long but not looping → conciseness reminder, LAContinue.
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	// Loop stats should NOT be incremented.
	assert.Equal(t, int64(0), execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal),
		"loop counter should not increment for non-looping long response")
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"loop gauge should not increment for non-looping long response")

	// Second call: concise valid answer → terminate.
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action)
}

func TestAgent_MaxResponseChars_TooLongAndLooping(t *testing.T) {
	// A response exceeds MaxResponseChars AND contains repetition. The two-step check
	// should detect the loop and use the loop recovery path (with loop stats).
	cfg := DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("I am stuck in this loop. ", 3)
	if len(loopBlock) > cfg.BlockSize {
		loopBlock = loopBlock[:cfg.BlockSize]
	} else {
		loopBlock += strings.Repeat(" ", cfg.BlockSize-len(loopBlock))
	}
	// 2 repetitions: below the streaming threshold (3) but above CheckAccumulated's
	// relaxed threshold (2). MaxResponseChars triggers first, then the two-step
	// check finds the repetition.
	loopingResponse := strings.Repeat(loopBlock, 2) + strings.Repeat("padding text ", 10)

	validResponse := "<thought>\nDifferent approach.\n</thought>\n<answer>\nResolved.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 200},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithMaxResponseChars(len(loopingResponse) - 10). // Just under total, forces truncation.
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "long-loop-test", data)

	// First call: too long + loop detected → loop recovery path.
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	// Loop stats SHOULD be incremented (it's a genuine loop).
	assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))
}

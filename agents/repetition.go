package agents

import (
	"errors"
	"hash/fnv"
)

// ErrRepetitionDetected is returned when the repetition detector
// identifies a degenerate loop in the model's streaming output.
var ErrRepetitionDetected = errors.New("repetition detected: model output is looping")

// ErrMaxResponseSize is returned when the accumulated response
// exceeds the configured maximum character limit.
var ErrMaxResponseSize = errors.New("max response size exceeded")

// RepetitionAction controls what happens when repetition is detected.
type RepetitionAction int

const (
	// RepetitionRecover truncates the response, increments the loop gauge, and injects
	// a reminder to try a different approach. The agent continues to the next iteration.
	// If the consecutive loop gauge reaches the executor's limit, execution terminates.
	RepetitionRecover RepetitionAction = iota

	// RepetitionTerminate returns an error immediately, terminating execution.
	RepetitionTerminate
)

// RepetitionConfig configures the streaming repetition detector.
type RepetitionConfig struct {
	// Enabled controls whether repetition detection runs. Default: true.
	Enabled bool

	// Action controls recovery behavior. Default: RepetitionRecover.
	Action RepetitionAction

	// BlockSize is the number of characters per detection block. Smaller blocks detect
	// shorter loops faster but increase false positive risk. Default: 400 (~100 tokens).
	BlockSize int

	// ExactThreshold is the number of times an exact block must repeat before
	// triggering. Default: 3 (original + 2 repeats).
	ExactThreshold int

	// SimilarThreshold is the number of near-duplicate blocks (SimHash Hamming
	// distance <= MaxHammingDist) before triggering. Default: 3.
	SimilarThreshold int

	// MaxHammingDist is the maximum Hamming distance between two SimHash fingerprints
	// to consider them near-duplicates. Default: 5 (out of 64 bits, ~92% similarity).
	// Start loose and tighten if you get false positives.
	MaxHammingDist int

	// Filter processes raw repeated text before including it in the recovery reminder.
	// Return empty string to suppress the block (shows hallucination warning instead).
	// Default: [DefaultFilterPoisonedText].
	Filter FilterPoisonedText

	// PoisonKeywords are passed to Filter. If any keyword appears in the filtered text,
	// the text is suppressed. For ReAct agents, this defaults to ["observation"].
	PoisonKeywords []string

	// RecoverMessage is the template shown when filtered text is available.
	// {block} is replaced with the filtered text, {count} with the loop count.
	// Default: see [DefaultRecoverMessage].
	RecoverMessage string

	// RecoverPoisonedMessage is shown when Filter returns empty (text is poisoned).
	// Default: see [DefaultRecoverPoisonedMessage].
	RecoverPoisonedMessage string
}

// DefaultRecoverMessage is the reminder injected when a loop is detected
// and the repeated text passes the poison filter.
const DefaultRecoverMessage = `You have repeated the same reasoning/action multiple times in a loop:
"{block}"

This approach is not working. The exact same action has produced the exact same result {count} ` +
	`times — repeating it will not produce a different outcome.

You MUST choose a DIFFERENT strategy.
Try a completely different tool or approach to solve this sub-problem.

DO NOT repeat the same action or reasoning again.`

// DefaultMaxResponseChars is the maximum characters allowed in a single model response.
// Based on Anthropic's default max_tokens of 4096 (~16K chars). Responses exceeding this
// are truncated and inspected for repetition.
const DefaultMaxResponseChars = 16000

// DefaultResponseTooLongMessage is injected when a response exceeds MaxResponseChars
// but does NOT contain repetition — it's genuinely too long, not looping.
const DefaultResponseTooLongMessage = `Your response was too long and was truncated. ` +
	`Please be more concise. Break your reasoning into smaller steps — output one action ` +
	`at a time rather than planning everything in a single response.`

// DefaultRecoverPoisonedMessage is the reminder injected when Filter returns
// empty, indicating the repeated text contains hallucinated content.
const DefaultRecoverPoisonedMessage = `You have repeated the same reasoning/action multiple times ` +
	`in a loop.
We cannot show your previous reasoning because it contains hallucinated text.
DO NOT HALLUCINATE. FOLLOW SYSTEM PROMPT CLOSELY.`

// DefaultRepetitionConfig returns sensible defaults for repetition detection.
func DefaultRepetitionConfig() RepetitionConfig {
	return RepetitionConfig{
		Enabled:                true,
		Action:                 RepetitionRecover,
		BlockSize:              400,
		ExactThreshold:         3,
		SimilarThreshold:       3,
		MaxHammingDist:         5,
		Filter:                 DefaultFilterPoisonedText,
		PoisonKeywords:         []string{"observation"},
		RecoverMessage:         DefaultRecoverMessage,
		RecoverPoisonedMessage: DefaultRecoverPoisonedMessage,
	}
}

// RepetitionResult is returned by the detector when repetition is found.
type RepetitionResult struct {
	// Err is ErrRepetitionDetected or ErrMaxResponseSize.
	Err error
	// RepeatedBlock is the raw text block that was detected as repeating.
	// Empty for max response size errors.
	RepeatedBlock string
}

// RepetitionDetector monitors streaming text for degenerate loops.
// It uses two complementary strategies:
//
//  1. Exact block matching via FNV-1a hash: detects verbatim repetition of paragraph-sized blocks.
//  2. Near-duplicate detection via SimHash: detects blocks with minor word-level variations.
//
// Blocks are extracted with 50% overlap (stride = BlockSize/2) to catch repeating units that
// don't align with fixed block boundaries. A third check enforces a hard max on total response
// length.
type RepetitionDetector struct {
	cfg RepetitionConfig

	// Accumulated text buffer.
	buf []byte

	// Position of the next block start (advances by stride, not block size).
	nextBlockStart int

	// Exact match: FNV hash -> repetition count.
	exactHashes map[uint64]int

	// SimHash: list of fingerprints for near-duplicate comparison.
	simHashes       []uint64
	similarRunCount int
}

// NewRepetitionDetector creates a new RepetitionDetector with the given config.
func NewRepetitionDetector(cfg RepetitionConfig) *RepetitionDetector {
	return &RepetitionDetector{
		cfg:         cfg,
		exactHashes: make(map[uint64]int),
	}
}

// Feed adds new text to the detector and checks for repetition.
// Returns a non-nil RepetitionResult if a kill condition is met, nil otherwise.
func (d *RepetitionDetector) Feed(text string) *RepetitionResult {
	if !d.cfg.Enabled {
		return nil
	}

	d.buf = append(d.buf, text...)

	// Process blocks with 50% overlap (stride = BlockSize/2). This catches repeating units
	// that don't align with fixed block boundaries — a 600-char paragraph repeated 3 times
	// will be caught regardless of where the first block boundary falls.
	stride := d.cfg.BlockSize / 2
	if stride < 1 {
		stride = 1
	}
	for d.nextBlockStart+d.cfg.BlockSize <= len(d.buf) {
		block := d.buf[d.nextBlockStart : d.nextBlockStart+d.cfg.BlockSize]
		d.nextBlockStart += stride

		// Exact match detection.
		h := fnvHash(block)
		d.exactHashes[h]++
		if d.exactHashes[h] >= d.cfg.ExactThreshold {
			return &RepetitionResult{Err: ErrRepetitionDetected, RepeatedBlock: string(block)}
		}

		// Near-duplicate detection via SimHash.
		sh := simHash(block)
		if d.isNearDuplicate(sh) {
			d.similarRunCount++
			if d.similarRunCount >= d.cfg.SimilarThreshold {
				return &RepetitionResult{
					Err: ErrRepetitionDetected, RepeatedBlock: string(block),
				}
			}
		} else {
			d.similarRunCount = 0
		}
		d.simHashes = append(d.simHashes, sh)
	}

	return nil
}

// CheckAccumulated runs repetition detection on whatever content has been accumulated
// so far, with a relaxed threshold (ExactThreshold - 1, SimilarThreshold - 1). This is
// used when MaxResponseChars truncates a response — we check if the truncated content
// shows signs of looping even if the normal streaming detection hasn't triggered yet.
// Returns non-nil RepetitionResult if repetition is found, nil if the content looks clean.
func (d *RepetitionDetector) CheckAccumulated() *RepetitionResult {
	if !d.cfg.Enabled {
		return nil
	}
	// Check if any exact hash appeared at least twice (relaxed from threshold).
	for h, count := range d.exactHashes {
		if count >= max(d.cfg.ExactThreshold-1, 2) {
			// Find the block text for the repeated hash.
			block := d.findBlockByHash(h)
			return &RepetitionResult{Err: ErrRepetitionDetected, RepeatedBlock: block}
		}
	}
	// Check if any SimHash pair is within distance (relaxed consecutive requirement).
	if d.similarRunCount >= max(d.cfg.SimilarThreshold-1, 2) {
		return &RepetitionResult{Err: ErrRepetitionDetected}
	}
	return nil
}

// findBlockByHash scans the buffer for a block whose FNV hash matches. Returns empty
// string if not found (should not happen since the hash came from our own blocks).
func (d *RepetitionDetector) findBlockByHash(target uint64) string {
	stride := d.cfg.BlockSize / 2
	if stride < 1 {
		stride = 1
	}
	for start := 0; start+d.cfg.BlockSize <= len(d.buf); start += stride {
		block := d.buf[start : start+d.cfg.BlockSize]
		if fnvHash(block) == target {
			return string(block)
		}
	}
	return ""
}

// isNearDuplicate checks if the given SimHash is within
// MaxHammingDist of any previously seen fingerprint.
func (d *RepetitionDetector) isNearDuplicate(sh uint64) bool {
	for _, prev := range d.simHashes {
		if hammingDistance(sh, prev) <= d.cfg.MaxHammingDist {
			return true
		}
	}
	return false
}

// --- Hash functions ---

// fnvHash computes a 64-bit FNV-1a hash of the block.
func fnvHash(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

// simHash computes a 64-bit SimHash fingerprint from word-level 5-grams.
// Each 5-gram is hashed and votes on each bit position:
// if the hash bit is 1, the counter increments; if 0, it decrements.
// The final fingerprint has bit i = 1 if counter[i] > 0.
func simHash(data []byte) uint64 {
	var counters [64]int

	// Extract words (split on whitespace).
	words := splitWords(data)
	if len(words) < 5 {
		// Too few words for 5-grams — hash the whole block.
		h := fnvHash(data)
		return h
	}

	// Generate 5-grams and vote.
	for i := 0; i <= len(words)-5; i++ {
		gram := joinWords(words[i : i+5])
		h := fnvHash(gram)
		for bit := range 64 {
			if h&(1<<uint(bit)) != 0 {
				counters[bit]++
			} else {
				counters[bit]--
			}
		}
	}

	// Build fingerprint.
	var fingerprint uint64
	for bit := range 64 {
		if counters[bit] > 0 {
			fingerprint |= 1 << uint(bit)
		}
	}
	return fingerprint
}

// hammingDistance returns the number of differing bits between two 64-bit values.
func hammingDistance(a, b uint64) int {
	x := a ^ b
	count := 0
	for x != 0 {
		count++
		x &= x - 1 // Clear lowest set bit.
	}
	return count
}

// splitWords splits data on ASCII whitespace, returning byte
// slices pointing into the original data.
func splitWords(data []byte) [][]byte {
	var words [][]byte
	start := -1
	for i, b := range data {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			if start >= 0 {
				words = append(words, data[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, data[start:])
	}
	return words
}

// joinWords concatenates word slices with a space separator.
func joinWords(words [][]byte) []byte {
	size := 0
	for _, w := range words {
		size += len(w) + 1
	}
	buf := make([]byte, 0, size)
	for i, w := range words {
		if i > 0 {
			buf = append(buf, ' ')
		}
		buf = append(buf, w...)
	}
	return buf
}

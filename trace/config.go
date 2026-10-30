package trace

import "github.com/rickchristie/gent"

const (
	DefaultMaxRecentEvents          = 5000
	DefaultMaxRecentLifecycleEvents = 500
	DefaultMaxRecentChunkEvents     = 500
	DefaultMaxContentBytes          = 64 * 1024
	DefaultMaxMetaContentBytes      = 64 * 1024

	NoRecentEvents = -1
	NoContentLimit = -1
)

// Config controls trace capture and snapshot retention.
type Config struct {
	Codecs                *gent.CodecRegistry
	IncludeModelRequests  bool
	IncludeModelResponses bool
	IncludeToolArgs       bool
	IncludeToolOutput     bool
	IncludeCommonPayload  bool
	IncludeStateChanges   bool
	IncludeChunkText      bool
	IncludeRunOutput      bool

	MaxRecentEvents          int
	MaxRecentLifecycleEvents int
	MaxRecentChunkEvents     int
	MaxContentBytes          int
	MaxMetaContentBytes      int

	Redactor Redactor
}

type config struct {
	Config
	maxRecentEvents          int
	maxRecentLifecycleEvents int
	maxRecentChunkEvents     int
	maxContentBytes          int
	maxMetaContentBytes      int
	redactor                 Redactor
}

func normalizeConfig(cfg Config) config {
	result := config{Config: cfg}
	result.maxRecentEvents = normalizeDefaultedLimit(
		cfg.MaxRecentEvents, DefaultMaxRecentEvents,
	)
	result.maxRecentLifecycleEvents = normalizeDefaultedLimit(
		cfg.MaxRecentLifecycleEvents, DefaultMaxRecentLifecycleEvents,
	)
	result.maxRecentChunkEvents = normalizeDefaultedLimit(
		cfg.MaxRecentChunkEvents, DefaultMaxRecentChunkEvents,
	)
	result.maxContentBytes = normalizeDefaultedLimit(
		cfg.MaxContentBytes, DefaultMaxContentBytes,
	)
	result.maxMetaContentBytes = normalizeDefaultedLimit(
		cfg.MaxMetaContentBytes, DefaultMaxMetaContentBytes,
	)
	result.redactor = cfg.Redactor
	if result.redactor == nil {
		result.redactor = RedactorFuncs{}
	}
	return result
}

func normalizeDefaultedLimit(value int, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

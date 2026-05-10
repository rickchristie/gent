package trace

const (
	DefaultMaxRecentEvents          = 5000
	DefaultMaxRecentLifecycleEvents = 500
	DefaultMaxRecentChunkEvents     = 500
	DefaultMaxModelContentBytes     = 64 * 1024
	DefaultMaxReasoningContentBytes = 64 * 1024

	NoRecentEvents = -1
	NoContentLimit = -1
)

// Config controls trace capture and snapshot retention.
type Config struct {
	IncludeModelRequests  bool
	IncludeModelResponses bool
	IncludeToolArgs       bool
	IncludeToolOutput     bool
	IncludeSystemPrompt   bool
	IncludeCommonPayload  bool
	IncludeDiffPayload    bool
	IncludeChunkText      bool
	IncludeRunOutput      bool

	MaxRecentEvents          int
	MaxRecentLifecycleEvents int
	MaxRecentChunkEvents     int
	MaxModelContentBytes     int
	MaxReasoningContentBytes int

	Redactor Redactor
}

type config struct {
	Config
	maxRecentEvents          int
	maxRecentLifecycleEvents int
	maxRecentChunkEvents     int
	maxModelContentBytes     int
	maxReasoningContentBytes int
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
	result.maxModelContentBytes = normalizeDefaultedLimit(
		cfg.MaxModelContentBytes, DefaultMaxModelContentBytes,
	)
	result.maxReasoningContentBytes = normalizeDefaultedLimit(
		cfg.MaxReasoningContentBytes, DefaultMaxReasoningContentBytes,
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

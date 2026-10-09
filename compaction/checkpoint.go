package compaction

import (
	"fmt"

	"github.com/rickchristie/gent"
)

func (s *SlidingWindowStrategy) RecoveryConfig() any {
	return map[string]any{"type": "sliding_window/v1", "window": s.windowSize}
}

func (s *SummarizationStrategy) RecoveryConfig() any {
	configuration := map[string]any{"type": "summarization/v1", "recent": s.keepRecent,
		"system": s.systemPrompt, "user": s.userPrompt, "format": s.textFormat.DescribeStructure(),
		"modelType": fmt.Sprintf("%T", s.model)}
	if named, ok := s.model.(interface{ ModelName() string }); ok {
		configuration["model"] = named.ModelName()
	}
	if provider, ok := s.model.(interface{ Provider() string }); ok {
		configuration["provider"] = provider.Provider()
	}
	if configured, ok := s.model.(gent.RecoveryConfig); ok {
		configuration["modelConfig"] = configured.RecoveryConfig()
	}
	return configuration
}

func (t *StatThresholdTrigger) RecoveryConfig() any {
	var counters, gauges []map[string]any
	for _, threshold := range t.counterThresholds {
		counters = append(counters, map[string]any{"key": threshold.key,
			"mode": threshold.matchMode, "delta": threshold.delta})
	}
	for _, threshold := range t.gaugeThresholds {
		gauges = append(gauges, map[string]any{"key": threshold.key,
			"mode": threshold.matchMode, "value": threshold.value})
	}
	return map[string]any{"type": "stat_threshold/v1", "counters": counters, "gauges": gauges}
}

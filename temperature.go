package gent

import "strings"

// ModelNamer is an optional interface that models can implement to expose their name.
// This is used by [DefaultSamplingParams] to select model-appropriate defaults.
//
// [LCGWrapper] in the models package implements this interface.
type ModelNamer interface {
	ModelName() string
}

// ParamDirective indicates how a sampling parameter should be handled for a given model.
type ParamDirective int

const (
	// ParamOmit means the parameter should not be sent. The API will use its own default.
	// This is appropriate when the model works fine without the parameter and the API
	// default is acceptable (e.g., top-p for Gemini 3 which uses thinking_level instead).
	ParamOmit ParamDirective = iota

	// ParamOverride means the parameter should be sent with the specified Value. This is
	// appropriate for model-specific defaults that differ from the API default (e.g.,
	// temperature 0.2 for Claude).
	ParamOverride

	// ParamForbidden means the parameter MUST NOT be sent. The API will return an error if
	// it receives this parameter (e.g., temperature for OpenAI o-series). If a user
	// explicitly sets a forbidden parameter, a warning event should be published.
	ParamForbidden
)

// SamplingParam is a single sampling parameter with its directive and value.
type SamplingParam struct {
	Directive ParamDirective
	Value     float64
}

// SamplingParams contains model-appropriate default sampling parameters for an LLM call.
// Each parameter carries a [ParamDirective] indicating whether it should be sent.
//
// Use [DefaultSamplingParams] to obtain defaults for a model.
//
// # Model-Specific Defaults
//
// Derived from vendor documentation and empirical findings in .concepts/10-TEMPERATURE.md:
//
//	┌──────────────────────┬──────────────┬──────────────┐
//	│ Model                │ Temperature  │ TopP         │
//	├──────────────────────┼──────────────┼──────────────┤
//	│ OpenAI reasoning     │ forbidden    │ forbidden    │
//	│ (o1/o3/o4/gpt-5)    │              │              │
//	├──────────────────────┼──────────────┼──────────────┤
//	│ Gemini 3+            │ override 1.0 │ omit         │
//	├──────────────────────┼──────────────┼──────────────┤
//	│ DeepSeek-R1          │ override 0.6 │ override 0.95│
//	├──────────────────────┼──────────────┼──────────────┤
//	│ Qwen3                │ override 0.6 │ override 0.95│
//	├──────────────────────┼──────────────┼──────────────┤
//	│ All others           │ override 0.2 │ override 1.0 │
//	└──────────────────────┴──────────────┴──────────────┘
type SamplingParams struct {
	Temperature SamplingParam
	TopP        SamplingParam
}

// DefaultSamplingParams returns model-appropriate default sampling parameters based on
// the model's name.
//
// If the model does not implement [ModelNamer] or the name is empty, the generic defaults
// are returned (temperature 0.2, top-p 1.0).
//
// This function is designed to be called by AgentLoop implementations to set sensible
// defaults. User-provided options (via WithCallOptions or similar) should always override
// the values returned here.
func DefaultSamplingParams(model Model) SamplingParams {
	namer, ok := model.(ModelNamer)
	if !ok {
		return defaultParams
	}

	name := strings.ToLower(namer.ModelName())
	if name == "" {
		return defaultParams
	}

	// OpenAI reasoning models — both params forbidden.
	if isOpenAIReasoningModel(name) {
		return SamplingParams{
			Temperature: SamplingParam{Directive: ParamForbidden},
			TopP:        SamplingParam{Directive: ParamForbidden},
		}
	}

	// Gemini 3+ — temperature must be 1.0, top-p omitted (uses thinking_level).
	if isGemini3Plus(name) {
		return SamplingParams{
			Temperature: SamplingParam{Directive: ParamOverride, Value: 1.0},
			TopP:        SamplingParam{Directive: ParamOmit},
		}
	}

	// DeepSeek-R1 — vendor-recommended range 0.5–0.7 for temperature, top-p 0.95.
	if isDeepSeekR1(name) {
		return SamplingParams{
			Temperature: SamplingParam{Directive: ParamOverride, Value: 0.6},
			TopP:        SamplingParam{Directive: ParamOverride, Value: 0.95},
		}
	}

	// Qwen3 — thinking mode: temperature 0.6, top-p 0.95.
	if isQwen3(name) {
		return SamplingParams{
			Temperature: SamplingParam{Directive: ParamOverride, Value: 0.6},
			TopP:        SamplingParam{Directive: ParamOverride, Value: 0.95},
		}
	}

	return defaultParams
}

// defaultParams is the generic default: temperature 0.2, top-p 1.0 (effectively disabled).
var defaultParams = SamplingParams{
	Temperature: SamplingParam{Directive: ParamOverride, Value: 0.2},
	TopP:        SamplingParam{Directive: ParamOverride, Value: 1.0},
}

// DefaultTemperature returns the recommended default temperature for the given model.
// This is a convenience wrapper around [DefaultSamplingParams].
//
// The second return value indicates whether temperature should be set at all. Returns
// false for both [ParamForbidden] and [ParamOmit] directives.
func DefaultTemperature(model Model) (float64, bool) {
	p := DefaultSamplingParams(model)
	return p.Temperature.Value, p.Temperature.Directive == ParamOverride
}

// isOpenAIReasoningModel returns true for OpenAI reasoning models that do not support
// sampling parameters. Covers: o1, o3, o4-mini, gpt-5 family.
func isOpenAIReasoningModel(name string) bool {
	n := stripPrefix(name, "openai/")
	if n == "o1" || strings.HasPrefix(n, "o1-") ||
		n == "o3" || strings.HasPrefix(n, "o3-") ||
		n == "o4-mini" || strings.HasPrefix(n, "o4-") {
		return true
	}

	if strings.HasPrefix(n, "gpt-5") {
		return true
	}

	return false
}

// isGemini3Plus returns true for Gemini 3+ models where temperature below 1.0 causes
// looping or degraded performance.
func isGemini3Plus(name string) bool {
	n := stripPrefix(name, "google/")
	if !strings.HasPrefix(n, "gemini-") {
		return false
	}

	rest := n[len("gemini-"):]
	if len(rest) == 0 {
		return false
	}

	// Parse major version digit. Gemini uses "gemini-{major}.{minor}" format.
	major := rest[0]
	return major >= '3' && major <= '9'
}

// isDeepSeekR1 returns true for DeepSeek-R1 models.
func isDeepSeekR1(name string) bool {
	n := stripPrefix(name, "deepseek/")
	return strings.HasPrefix(n, "deepseek-r1")
}

// isQwen3 returns true for Qwen3 models.
func isQwen3(name string) bool {
	return strings.Contains(name, "qwen3") || strings.Contains(name, "qwen-3")
}

// stripPrefix removes the prefix if present, otherwise returns the string unchanged.
func stripPrefix(s, prefix string) string {
	after, _ := strings.CutPrefix(s, prefix)
	return after
}

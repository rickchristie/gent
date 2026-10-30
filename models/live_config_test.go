package models

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type liveProvider struct {
	name, prefix, defaultModel string
	local, images              bool
}

func liveProviders() []liveProvider {
	return []liveProvider{
		{"OpenAI", "OPENAI", gent.ModelOpenAIGPT54Mini, false, true},
		{"Anthropic", "ANTHROPIC", gent.ModelAnthropicClaude46Sonnet, false, false},
		{"Gemini", "GEMINI", gent.ModelGoogleGemini25Flash, false, true},
		{"xAI", "XAI", gent.ModelXAIGrok43, false, true},
		{"DeepSeek", "DEEPSEEK", "deepseek-flash", false, false},
		{"OpenRouter", "OPENROUTER", "", false, true},
		{"vLLM", "VLLM", "", true, false},
		{"Ollama", "OLLAMA", "", true, false},
		{"llama.cpp", "LLAMACPP", "", true, false},
		{"SGLang", "SGLANG", "", true, false},
	}
}

// A supplied key or endpoint enables paid tests in normal go test runs. Require-all mode turns
// missing configuration into a failure, so an incomplete live matrix cannot appear successful.
func readLiveConfig(provider liveProvider, getenv func(string) string, image bool) (
	Config, bool, error,
) {
	prefix := "GENT_TEST_" + provider.prefix
	modelKey := prefix + "_MODEL"
	if image {
		modelKey = prefix + "_IMAGE_MODEL"
	}
	config := Config{APIKey: getenv(prefix + "_KEY"), BaseURL: getenv(prefix + "_URL"),
		Model: getenv(modelKey)}
	required := getenv("GENT_LIVE_REQUIRE_ALL") == "1"
	if !required && image && config.Model == "" {
		return Config{}, false, nil
	}
	if !required && config.APIKey == "" && config.BaseURL == "" && config.Model == "" {
		return Config{}, false, nil
	}
	if config.Model == "" && !image {
		config.Model = provider.defaultModel
	}
	var missing []string
	if !provider.local && config.APIKey == "" {
		missing = append(missing, prefix+"_KEY")
	}
	if provider.local && config.BaseURL == "" {
		missing = append(missing, prefix+"_URL")
	}
	if config.Model == "" {
		missing = append(missing, modelKey)
	}
	if len(missing) > 0 {
		return Config{}, false, fmt.Errorf("live provider needs %s", strings.Join(missing, ", "))
	}
	return config, true, nil
}

func liveConfig(t *testing.T, provider liveProvider, image bool) Config {
	t.Helper()
	config, enabled, err := readLiveConfig(provider, os.Getenv, image)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	if !enabled {
		t.Skipf("configure GENT_TEST_%s variables in .env.example", provider.prefix)
	}
	return config
}

func newLiveModel(provider liveProvider, config *Config) gent.Model {
	switch provider.prefix {
	case "OPENAI", "XAI":
		name, endpoint := "openai", "https://api.openai.com/v1"
		if provider.prefix == "XAI" {
			name, endpoint = "xai", "https://api.x.ai/v1"
		}
		resolved := providerConfig(config, endpoint)
		return NewResponses(&ResponsesConfig{Provider: name, BaseURL: resolved.BaseURL,
			APIKey: resolved.APIKey, Model: resolved.Model, HTTPClient: resolved.HTTPClient})
	case "ANTHROPIC":
		return NewAnthropic(config)
	case "GEMINI":
		return NewGemini(config)
	case "DEEPSEEK":
		return NewDeepSeek(config)
	case "OPENROUTER":
		return NewOpenRouter(config)
	case "VLLM":
		return NewVLLM(config)
	case "OLLAMA":
		return NewOllama(config)
	case "LLAMACPP":
		return NewLlamaCpp(config)
	case "SGLANG":
		return NewSGLang(config)
	default:
		panic("unknown live provider")
	}
}

func TestLiveConfig(t *testing.T) {
	type input struct {
		provider liveProvider
		env      map[string]string
		image    bool
	}
	type expected struct {
		config  Config
		enabled bool
		err     string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cloud := liveProvider{"OpenAI", "OPENAI", "default-model", false, true}
	local := liveProvider{"vLLM", "VLLM", "", true, false}
	cases := []testCase{
		{"unconfigured cloud", input{cloud, nil, false}, expected{}},
		{"configured cloud", input{cloud, map[string]string{"GENT_TEST_OPENAI_KEY": "test-key"},
			false}, expected{Config{APIKey: "test-key", Model: "default-model"}, true, ""}},
		{"cloud overrides", input{cloud, map[string]string{"GENT_TEST_OPENAI_KEY": "test-key",
			"GENT_TEST_OPENAI_URL": "https://provider.test/v1", "GENT_TEST_OPENAI_MODEL": "chosen"},
			false}, expected{Config{APIKey: "test-key", BaseURL: "https://provider.test/v1",
			Model: "chosen"}, true, ""}},
		{"required cloud key", input{cloud, map[string]string{"GENT_LIVE_REQUIRE_ALL": "1"}, false},
			expected{err: "live provider needs GENT_TEST_OPENAI_KEY"}},
		{"partial cloud settings", input{cloud, map[string]string{
			"GENT_TEST_OPENAI_MODEL": "chosen"}, false},
			expected{err: "live provider needs GENT_TEST_OPENAI_KEY"}},
		{"unconfigured local", input{local, nil, false}, expected{}},
		{"local without authentication", input{local, map[string]string{
			"GENT_TEST_VLLM_URL": "http://localhost:8000/v1", "GENT_TEST_VLLM_MODEL": "served"}, false},
			expected{Config{BaseURL: "http://localhost:8000/v1", Model: "served"}, true, ""}},
		{"local with authentication", input{local, map[string]string{"GENT_TEST_VLLM_KEY": "key",
			"GENT_TEST_VLLM_URL": "http://localhost:8000/v1", "GENT_TEST_VLLM_MODEL": "served"}, false},
			expected{Config{APIKey: "key", BaseURL: "http://localhost:8000/v1", Model: "served"}, true, ""}},
		{"local model missing", input{local, map[string]string{
			"GENT_TEST_VLLM_URL": "http://localhost:8000/v1"}, false},
			expected{err: "live provider needs GENT_TEST_VLLM_MODEL"}},
		{"required local settings", input{local, map[string]string{"GENT_LIVE_REQUIRE_ALL": "1"},
			false}, expected{err: "live provider needs GENT_TEST_VLLM_URL, GENT_TEST_VLLM_MODEL"}},
		{"image model unset", input{cloud, map[string]string{"GENT_TEST_OPENAI_KEY": "test-key"},
			true}, expected{}},
		{"image model selected", input{cloud, map[string]string{"GENT_TEST_OPENAI_KEY": "test-key",
			"GENT_TEST_OPENAI_IMAGE_MODEL": "image-model"}, true},
			expected{Config{APIKey: "test-key", Model: "image-model"}, true, ""}},
		{"required image model", input{cloud, map[string]string{"GENT_TEST_OPENAI_KEY": "test-key",
			"GENT_LIVE_REQUIRE_ALL": "1"}, true},
			expected{err: "live provider needs GENT_TEST_OPENAI_IMAGE_MODEL"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config, enabled, err := readLiveConfig(test.input.provider, func(key string) string {
				return test.input.env[key]
			}, test.input.image)
			actual := expected{config: config, enabled: enabled}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

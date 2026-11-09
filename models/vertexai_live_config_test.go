package models

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func readLiveVertexConfig(provider liveProvider, getenv func(string) string, image bool) (
	VertexAIConfig, bool, error,
) {
	modelKey := "GENT_TEST_VERTEXAI_MODEL"
	if image {
		modelKey = "GENT_TEST_VERTEXAI_IMAGE_MODEL"
	}
	config := VertexAIConfig{Project: getenv("GOOGLE_CLOUD_PROJECT"),
		Location: getenv("GOOGLE_CLOUD_LOCATION"), Model: getenv(modelKey),
		BaseURL: getenv("GENT_TEST_VERTEXAI_URL")}
	required := getenv("GENT_LIVE_REQUIRE_ALL") == "1"
	if !required && image && config.Model == "" {
		return VertexAIConfig{}, false, nil
	}
	if !required && config.Project == "" && config.BaseURL == "" && config.Model == "" &&
		getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		return VertexAIConfig{}, false, nil
	}
	if config.Model == "" && !image {
		config.Model = provider.defaultModel
	}
	var missing []string
	if config.Project == "" {
		missing = append(missing, "GOOGLE_CLOUD_PROJECT")
	}
	if config.Model == "" {
		missing = append(missing, modelKey)
	}
	if len(missing) > 0 {
		return VertexAIConfig{}, false, fmt.Errorf("live provider needs %s", strings.Join(missing, ", "))
	}
	return config, true, nil
}

func TestLiveVertexConfig(t *testing.T) {
	type input struct {
		env   map[string]string
		image bool
	}
	type expected struct {
		config  VertexAIConfig
		enabled bool
		err     string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	provider := liveProvider{"VertexAI", "VERTEXAI", "default-model", false, true}
	cases := []testCase{
		{"unconfigured", input{}, expected{}},
		{"ADC", input{env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project"}},
			expected{VertexAIConfig{Project: "project", Model: "default-model"}, true, ""}},
		{"file without project", input{env: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "key"}},
			expected{err: "live provider needs GOOGLE_CLOUD_PROJECT"}},
		{"required", input{env: map[string]string{"GENT_LIVE_REQUIRE_ALL": "1"}},
			expected{err: "live provider needs GOOGLE_CLOUD_PROJECT"}},
		{"image unset", input{map[string]string{"GOOGLE_CLOUD_PROJECT": "project"}, true}, expected{}},
		{"image required", input{map[string]string{"GENT_LIVE_REQUIRE_ALL": "1",
			"GOOGLE_CLOUD_PROJECT": "project"}, true},
			expected{err: "live provider needs GENT_TEST_VERTEXAI_IMAGE_MODEL"}},
		{"overrides", input{map[string]string{"GOOGLE_CLOUD_PROJECT": "project",
			"GOOGLE_CLOUD_LOCATION": "us-central1", "GENT_TEST_VERTEXAI_IMAGE_MODEL": "image-model",
			"GENT_TEST_VERTEXAI_URL": "https://proxy.test/v1"}, true},
			expected{VertexAIConfig{Project: "project", Location: "us-central1", Model: "image-model",
				BaseURL: "https://proxy.test/v1"}, true, ""}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config, enabled, err := readLiveVertexConfig(provider, func(key string) string {
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

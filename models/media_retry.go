package models

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/modelcontext"
)

type modelHTTPError struct {
	status  int
	body    string
	code    string
	message string
}

func (e *modelHTTPError) Error() string {
	return fmt.Sprintf("model HTTP %d: %s", e.status, e.body)
}

// A rejected request can identify a URL load failure before any model content is delivered.
// Replace only the identified URL. An ambiguous or unrelated provider failure remains an error.
// The caller permits one corrected request, never a tool retry or a partial-stream replay.
func replaceFailedImageURL(ctx context.Context, encoded []byte, failure error,
	steering gent.SteeringMessages,
) ([]byte, bool, error) {
	var rejected *modelHTTPError
	if !errors.As(failure, &rejected) || (rejected.status != 400 && rejected.status != 422) {
		return nil, false, nil
	}
	known := rejected.code == "invalid_image_url" || rejected.code == "failed_to_download_image" ||
		rejected.code == "image_file_not_found"
	if rejected.code == "invalid_request_error" {
		known = strings.Contains(rejected.message, "Could not download image") ||
			strings.Contains(rejected.message, "Error fetching image")
	}
	if !known {
		return nil, false, nil
	}
	var body any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, false, err
	}
	urls := map[string]bool{}
	visitURLNodes(body, func(_ map[string]any, url string) { urls[url] = true })
	var selected string
	for url := range urls {
		if len(urls) == 1 || strings.Contains(rejected.message, url) {
			if selected != "" {
				return nil, false, nil
			}
			selected = url
		}
	}
	if selected == "" {
		return nil, false, nil
	}
	text := steering.Resolved().ImageURLLoadFailed + " URL: " + selected
	visitURLNodes(body, func(node map[string]any, url string) {
		if url != selected {
			return
		}
		kind := "text"
		if node["type"] == "input_image" {
			kind = "input_text"
		}
		clear(node)
		node["type"], node["text"] = kind, text
	})
	data, err := json.Marshal(body)
	if err != nil {
		return nil, false, err
	}
	if err := modelcontext.Record(ctx, map[string]any{"original": gent.ImageURLPart(selected),
		"prepared": gent.TextPart(text), "reason": "provider_image_url_load_failed",
		"error": gent.SaveError(failure)}); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func visitURLNodes(value any, visit func(map[string]any, string)) {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			visitURLNodes(item, visit)
		}
	case map[string]any:
		var url string
		switch value["type"] {
		case "input_image":
			url, _ = value["image_url"].(string)
		case "image_url":
			if image, ok := value["image_url"].(map[string]any); ok {
				url, _ = image["url"].(string)
			}
		case "image":
			if source, ok := value["source"].(map[string]any); ok && source["type"] == "url" {
				url, _ = source["url"].(string)
			}
		}
		if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
			visit(value, url)
			return
		}
		for _, item := range value {
			visitURLNodes(item, visit)
		}
	}
}

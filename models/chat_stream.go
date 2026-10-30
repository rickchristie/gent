package models

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/rickchristie/gent"
)

type chatEnvelope struct {
	Choices []struct {
		Index   int                        `json:"index"`
		Message json.RawMessage            `json:"message"`
		Delta   map[string]json.RawMessage `json:"delta"`
		Finish  string                     `json:"finish_reason"`
	} `json:"choices"`
	Usage chatUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatUsage struct {
	Input       int `json:"prompt_tokens"`
	Output      int `json:"completion_tokens"`
	Total       int `json:"total_tokens"`
	CacheHit    int `json:"prompt_cache_hit_tokens"`
	InputDetail struct {
		Cached int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	OutputDetail struct {
		Reasoning int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (m *Chat) readResponse(ctx context.Context, response *http.Response,
	stream *gent.StreamWithDuration, options *gent.CallOptions,
) (*gent.ContentResponse, error) {
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		var envelope chatEnvelope
		if err := decodeProviderJSON(response.Body, &envelope); err != nil {
			return nil, err
		}
		if envelope.Error != nil {
			return nil, errors.New(envelope.Error.Message)
		}
		if len(envelope.Choices) != 1 {
			return nil, errors.New("Chat requires one response choice")
		}
		choice := envelope.Choices[0]
		result, err := m.convertResponse(ctx, choice.Message, choice.Finish, envelope.Usage)
		if err != nil {
			return nil, err
		}
		return previewProviderResponse(ctx, stream, options, result)
	}
	accumulator := chatAccumulator{fields: make(map[string]json.RawMessage),
		calls: make(map[int]*chatCallDelta), text: make(map[string]*strings.Builder)}
	var usage chatUsage
	var finish string
	size := 0
	err := readSSE(response.Body, func(data []byte) (bool, error) {
		if bytes.Equal(data, []byte("[DONE]")) {
			if finish == "" {
				return false, errors.New("Chat stream has no finish reason")
			}
			return true, nil
		}
		size += len(data)
		if size > maxProviderBytes {
			return false, errors.New("Chat response exceeds 64 MiB")
		}
		var envelope chatEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return false, err
		}
		if envelope.Error != nil {
			return false, errors.New(envelope.Error.Message)
		}
		if envelope.Usage != (chatUsage{}) {
			usage = envelope.Usage
		}
		for _, choice := range envelope.Choices {
			if choice.Index != 0 || finish != "" {
				return false, errors.New("unexpected Chat response choice")
			}
			if err := accumulator.add(choice.Delta); err != nil {
				return false, err
			}
			for _, name := range []string{"reasoning_content", "reasoning", "content", "refusal"} {
				if raw := choice.Delta[name]; raw != nil && !bytes.Equal(raw, []byte("null")) {
					var text string
					if err := json.Unmarshal(raw, &text); err != nil {
						return false, err
					}
					kind := gent.MetaContentIntermediary
					if name == "reasoning" || name == "reasoning_content" {
						kind = gent.MetaContentReasoning
					}
					if err := sendProviderText(ctx, stream, options, kind, name, text); err != nil {
						return false, err
					}
				}
			}
			finish = choice.Finish
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	message, err := accumulator.finish()
	if err != nil {
		return nil, err
	}
	return m.convertResponse(ctx, message, finish, usage)
}

type chatAccumulator struct {
	fields map[string]json.RawMessage
	text   map[string]*strings.Builder
	calls  map[int]*chatCallDelta
	images []json.RawMessage
	detail []*chatReasoningBlock
}

type chatCallDelta struct {
	ID        string
	Type      string
	Name      strings.Builder
	Arguments strings.Builder
}

func (state *chatAccumulator) add(delta map[string]json.RawMessage) error {
	for field, raw := range delta {
		if bytes.Equal(raw, []byte("null")) {
			continue
		}
		switch field {
		case "content", "reasoning_content", "reasoning", "refusal":
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return err
			}
			if state.text[field] == nil {
				state.text[field] = &strings.Builder{}
			}
			state.text[field].WriteString(text)
		case "tool_calls":
			if err := state.addCalls(raw); err != nil {
				return err
			}
		case "images":
			var images []json.RawMessage
			if err := json.Unmarshal(raw, &images); err != nil {
				return err
			}
			state.images = append(state.images, images...)
		case "reasoning_details":
			if err := state.addReasoning(raw); err != nil {
				return err
			}
		default:
			state.fields[field] = raw
		}
	}
	return nil
}

func (state *chatAccumulator) addCalls(raw json.RawMessage) error {
	var deltas []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &deltas); err != nil {
		return err
	}
	for _, delta := range deltas {
		if delta.Index < 0 || delta.Index > 4096 {
			return errors.New("invalid Chat tool index")
		}
		call := state.calls[delta.Index]
		if call == nil {
			call = &chatCallDelta{}
			state.calls[delta.Index] = call
		}
		if delta.ID != "" {
			if call.ID != "" && call.ID != delta.ID {
				return errors.New("Chat tool call changed its ID")
			}
			call.ID = delta.ID
		}
		if delta.Type != "" {
			call.Type = delta.Type
		}
		call.Name.WriteString(delta.Function.Name)
		call.Arguments.WriteString(delta.Function.Arguments)
	}
	return nil
}

func (state *chatAccumulator) finish() (json.RawMessage, error) {
	message := make(map[string]any, len(state.fields)+len(state.text)+3)
	for key, value := range state.fields {
		message[key] = value
	}
	message["role"] = "assistant"
	for key, text := range state.text {
		message[key] = text.String()
	}
	if len(state.calls) > 0 {
		indices := make([]int, 0, len(state.calls))
		for index := range state.calls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		calls := make([]gent.ModelToolCall, 0, len(indices))
		for _, index := range indices {
			call := state.calls[index]
			calls = append(calls, gent.ModelToolCall{ID: call.ID, Type: call.Type,
				FunctionCall: &gent.FunctionCall{Name: call.Name.String(),
					Arguments: call.Arguments.String()}})
		}
		message["tool_calls"] = calls
	}
	if len(state.images) > 0 {
		message["images"] = state.images
	}
	if len(state.detail) > 0 {
		details := make([]map[string]any, 0, len(state.detail))
		for _, block := range state.detail {
			details = append(details, block.value())
		}
		message["reasoning_details"] = details
	}
	return json.Marshal(message)
}

func (m *Chat) convertResponse(ctx context.Context, raw json.RawMessage, reason string,
	usage chatUsage,
) (*gent.ContentResponse, error) {
	var message struct {
		Content   string               `json:"content"`
		Reasoning string               `json:"reasoning_content"`
		Thought   string               `json:"reasoning"`
		Refusal   *string              `json:"refusal"`
		Calls     []gent.ModelToolCall `json:"tool_calls"`
		Images    []struct {
			Type string `json:"type"`
			URL  struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	choice := &gent.ContentChoice{
		Continuation: &gent.ProviderContinuation{Provider: m.provider, Items: []json.RawMessage{raw}},
		Completion:   gent.ModelCompletion{ProviderReason: reason}}
	switch reason {
	case "stop":
		choice.Completion.Status = gent.ModelStatusAnswer
	case "tool_calls":
		choice.Completion.Status = gent.ModelStatusToolCalls
	case "length", "max_tokens":
		choice.Completion.Status = gent.ModelStatusIncomplete
	case "content_filter":
		choice.Completion.Status = gent.ModelStatusRefused
		choice.Completion.Refusal = &gent.ModelRefusal{Code: reason}
	default:
		return nil, fmt.Errorf("unknown %s finish reason %q", m.provider, reason)
	}
	if message.Refusal != nil {
		choice.Completion.Status = gent.ModelStatusRefused
		choice.Completion.Refusal = &gent.ModelRefusal{Code: "refusal",
			Content: []gent.ContentPart{gent.TextContent{Text: *message.Refusal}}}
	}
	for _, call := range message.Calls {
		if call.ID == "" || call.Type != "function" || call.FunctionCall == nil ||
			call.FunctionCall.Name == "" {
			if choice.Completion.Status == gent.ModelStatusIncomplete {
				continue
			}
			return nil, errors.New("Chat function call requires id, type, and name")
		}
		choice.ToolCalls = append(choice.ToolCalls, call)
	}
	if message.Content != "" {
		choice.CandidateContent = []gent.ContentPart{gent.TextContent{Text: message.Content}}
		choice.CandidateSource = &gent.ContentSource{MessageIds: []string{"content"}}
		choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
			Type: gent.MetaContentIntermediary, MessageId: "content",
			Content: gent.CloneContentParts(choice.CandidateContent)})
	}
	for _, reasoning := range []struct{ id, text string }{
		{"reasoning_content", message.Reasoning}, {"reasoning", message.Thought},
	} {
		if reasoning.text != "" {
			choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
				Type: gent.MetaContentReasoning, MessageId: reasoning.id,
				Content: []gent.ContentPart{gent.TextContent{Text: reasoning.text}}})
		}
	}
	for index, image := range message.Images {
		if image.Type != "image_url" {
			return nil, fmt.Errorf("unsupported Chat image %q", image.Type)
		}
		part, err := resolveOutputImage(ctx, m.config.HTTPClient, image.URL.URL)
		if err != nil {
			return nil, err
		}
		choice.CandidateContent = append(choice.CandidateContent, part)
		if choice.CandidateSource == nil {
			choice.CandidateSource = &gent.ContentSource{}
		}
		id := fmt.Sprintf("image:%d", index)
		choice.CandidateSource.MessageIds = append(choice.CandidateSource.MessageIds, id)
		choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
			Type: gent.MetaContentIntermediary, MessageId: id, Content: []gent.ContentPart{part}})
	}
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	cached := usage.InputDetail.Cached
	if cached == 0 {
		cached = usage.CacheHit
	}
	if usage.Total == 0 {
		usage.Total = usage.Input + usage.Output
	}
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}, Info: &gent.GenerationInfo{
		InputTokens: usage.Input, OutputTokens: usage.Output, TotalTokens: usage.Total,
		CachedInputTokens: cached, ReasoningTokens: usage.OutputDetail.Reasoning,
	}}, nil
}

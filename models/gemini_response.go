package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rickchristie/gent"
)

type geminiCandidate struct {
	Index        int           `json:"index"`
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Token        string        `json:"continuationToken"`
}

type geminiEnvelope struct {
	Candidates []geminiCandidate `json:"candidates"`
	Feedback   struct {
		Reason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Usage struct {
		Input     int `json:"promptTokenCount"`
		Output    int `json:"candidatesTokenCount"`
		Total     int `json:"totalTokenCount"`
		Cached    int `json:"cachedContentTokenCount"`
		Reasoning int `json:"thoughtsTokenCount"`
		Tools     int `json:"toolUsePromptTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (m *Gemini) readResponse(ctx context.Context, response *http.Response,
	stream *gent.StreamWithDuration, options *gent.CallOptions, request map[string]any,
	prior *geminiContinuation,
) (*gent.ContentResponse, error) {
	var envelope geminiEnvelope
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		if err := decodeProviderJSON(response.Body, &envelope); err != nil {
			return nil, err
		}
		result, err := m.convertResponse(ctx, &envelope, options, request, prior)
		if err != nil {
			return nil, err
		}
		return previewProviderResponse(ctx, stream, options, result)
	}
	var parts []json.RawMessage
	size := 0
	err := readSSE(response.Body, func(data []byte) (bool, error) {
		size += len(data)
		if size > maxProviderBytes {
			return false, errors.New("Gemini response exceeds 64 MiB")
		}
		var event geminiEnvelope
		if err := json.Unmarshal(data, &event); err != nil {
			return false, err
		}
		if event.Error != nil {
			return false, errors.New(event.Error.Message)
		}
		if event.Usage != (geminiEnvelope{}).Usage {
			envelope.Usage = event.Usage
		}
		if event.Feedback.Reason != "" {
			envelope.Feedback = event.Feedback
			return true, nil
		}
		if len(event.Candidates) == 0 && event.Usage != (geminiEnvelope{}).Usage {
			return false, nil
		}
		if len(event.Candidates) != 1 || event.Candidates[0].Index != 0 {
			return false, errors.New("Gemini requires one response candidate")
		}
		candidate := event.Candidates[0]
		for _, raw := range candidate.Content.Parts {
			var part struct {
				Text    string `json:"text"`
				Thought bool   `json:"thought"`
			}
			if err := json.Unmarshal(raw, &part); err != nil {
				return false, err
			}
			kind := gent.MetaContentIntermediary
			if part.Thought {
				kind = gent.MetaContentReasoning
			}
			if err := sendProviderText(ctx, stream, options, kind,
				fmt.Sprintf("part:%d", len(parts)), part.Text); err != nil {
				return false, err
			}
			parts = append(parts, raw)
		}
		candidate.Content.Parts = parts
		envelope.Candidates = []geminiCandidate{candidate}
		return candidate.FinishReason != "", nil
	})
	if err != nil {
		return nil, err
	}
	return m.convertResponse(ctx, &envelope, options, request, prior)
}

func (m *Gemini) convertResponse(ctx context.Context, response *geminiEnvelope,
	options *gent.CallOptions, request map[string]any, prior *geminiContinuation,
) (*gent.ContentResponse, error) {
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}
	choice := &gent.ContentChoice{}
	if response.Feedback.Reason != "" {
		choice.Completion = gent.ModelCompletion{Status: gent.ModelStatusRefused,
			ProviderReason: response.Feedback.Reason,
			Refusal:        &gent.ModelRefusal{Code: response.Feedback.Reason}}
		return geminiResult(choice, response), nil
	}
	if len(response.Candidates) != 1 || response.Candidates[0].Index != 0 {
		return nil, errors.New("Gemini requires one response candidate")
	}
	candidate := response.Candidates[0]
	if prior != nil {
		parts := make([]json.RawMessage, 0, len(prior.Content.Parts)+len(candidate.Content.Parts))
		parts = append(parts, prior.Content.Parts...)
		candidate.Content.Parts = append(parts, candidate.Content.Parts...)
	}
	choice.Completion.ProviderReason = candidate.FinishReason
	switch candidate.FinishReason {
	case "STOP":
		choice.Completion.Status = gent.ModelStatusAnswer
	case "MAX_TOKENS", "MALFORMED_FUNCTION_CALL", "TOO_MANY_TOOL_CALLS":
		choice.Completion.Status = gent.ModelStatusIncomplete
	case "CONTINUATION":
		if candidate.Token == "" {
			return nil, errors.New("Gemini continuation has no token")
		}
		choice.Completion.Status = gent.ModelStatusContinue
		if options.MaxTokens > 0 {
			// Gemini requires increasing or omitting a cumulative token ceiling on continuation.
			// Do not silently increase a caller's explicit ceiling; let the loop retry shorter.
			choice.Completion.Status = gent.ModelStatusIncomplete
		}
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY",
		"IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "ESCALATION", "PUP_LIMITED_DISABLED":
		choice.Completion.Status = gent.ModelStatusRefused
		choice.Completion.Refusal = &gent.ModelRefusal{Code: candidate.FinishReason}
	case "LANGUAGE", "OTHER", "IMAGE_OTHER", "NO_IMAGE", "MALFORMED_RESPONSE",
		"MISSING_THOUGHT_SIGNATURE",
		"UNEXPECTED_TOOL_CALL":
		choice.Completion.Status = gent.ModelStatusFailed
	default:
		return nil, fmt.Errorf("unknown Gemini finish reason %q", candidate.FinishReason)
	}
	state := geminiContinuation{Content: candidate.Content}
	if choice.Completion.Status == gent.ModelStatusContinue {
		state.Token = candidate.Token
		raw, err := json.Marshal(request["contents"])
		if err != nil {
			return nil, err
		}
		state.RequestContents = raw
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	choice.Continuation = &gent.ProviderContinuation{Provider: "gemini", Items: []json.RawMessage{raw}}
	for index, raw := range candidate.Content.Parts {
		if err := m.convertPart(ctx, choice, index, raw); err != nil {
			return nil, err
		}
	}
	if choice.Completion.Status == gent.ModelStatusAnswer && len(choice.ToolCalls) > 0 {
		choice.Completion.Status = gent.ModelStatusToolCalls
	}
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	return geminiResult(choice, response), nil
}

func (m *Gemini) convertPart(ctx context.Context, choice *gent.ContentChoice,
	index int, raw json.RawMessage,
) error {
	var part struct {
		Text      *string `json:"text"`
		Thought   bool    `json:"thought"`
		Signature string  `json:"thoughtSignature"`
		Inline    *struct {
			MIME string `json:"mimeType"`
			Data string `json:"data"`
		} `json:"inlineData"`
		File *struct {
			MIME string `json:"mimeType"`
			URI  string `json:"fileUri"`
		} `json:"fileData"`
		Call *struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"functionCall"`
	}
	if err := json.Unmarshal(raw, &part); err != nil {
		return err
	}
	var content []gent.ContentPart
	switch {
	case part.Text != nil:
		content = []gent.ContentPart{gent.TextContent{Text: *part.Text}}
	case part.Inline != nil:
		image, err := decodeOutputImage(part.Inline.MIME, part.Inline.Data)
		if err != nil {
			return err
		}
		content = []gent.ContentPart{image}
	case part.File != nil:
		image, err := resolveOutputImage(ctx, m.config.HTTPClient, part.File.URI)
		if err != nil {
			return err
		}
		if part.File.MIME != "" {
			image.MIMEType = part.File.MIME
		}
		content = []gent.ContentPart{image}
	case part.Call != nil:
		if part.Call.Name == "" {
			if choice.Completion.Status == gent.ModelStatusIncomplete {
				return nil
			}
			return errors.New("Gemini function call has no name")
		}
		choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: part.Call.ID,
			Type: "function", FunctionCall: &gent.FunctionCall{
				Name: part.Call.Name, Arguments: string(part.Call.Args)}})
		return nil
	case part.Signature != "":
		return nil
	default:
		return errors.New("unsupported Gemini response part")
	}
	id := fmt.Sprintf("part:%d", index)
	kind := gent.MetaContentIntermediary
	if part.Thought {
		kind = gent.MetaContentReasoning
	} else {
		choice.CandidateContent = append(choice.CandidateContent, content...)
		if choice.CandidateSource == nil {
			choice.CandidateSource = &gent.ContentSource{}
		}
		choice.CandidateSource.MessageIds = append(choice.CandidateSource.MessageIds, id)
	}
	choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
		Type: kind, MessageId: id, Content: content})
	return nil
}

func geminiResult(choice *gent.ContentChoice, response *geminiEnvelope) *gent.ContentResponse {
	usage := response.Usage
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}, Info: &gent.GenerationInfo{
		InputTokens: usage.Input + usage.Tools, OutputTokens: usage.Output + usage.Reasoning,
		TotalTokens: usage.Total, CachedInputTokens: usage.Cached, ReasoningTokens: usage.Reasoning,
	}}
}

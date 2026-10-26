package models

import (
	"fmt"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

func lcgRequest(request *gent.ModelCallRequest) ([]llms.MessageContent, llms.CallOptions, error) {
	o := &request.Options
	options := llms.CallOptions{Model: o.Model, CandidateCount: o.CandidateCount,
		MaxTokens: o.MaxTokens, StopWords: o.StopWords,
		TopK: o.TopK, Seed: o.Seed, MinLength: o.MinLength,
		MaxLength: o.MaxLength, N: o.N, RepetitionPenalty: o.RepetitionPenalty,
		FrequencyPenalty: o.FrequencyPenalty, PresencePenalty: o.PresencePenalty,
		JSONMode: o.JSONMode, ResponseMIMEType: o.ResponseMIMEType,
		StreamingFunc: o.StreamingFunc, StreamingReasoningFunc: o.StreamingReasoningFunc}
	if o.Temperature != nil {
		options.Temperature = *o.Temperature
	}
	if o.TopP != nil {
		options.TopP = *o.TopP
	}
	if o.Metadata != nil {
		options.Metadata = make(map[string]any, len(o.Metadata))
		for key, value := range o.Metadata {
			if config, ok := value.(*gent.ThinkingConfig); ok {
				value = &llms.ThinkingConfig{Mode: llms.ThinkingMode(config.Mode),
					BudgetTokens: config.BudgetTokens, ReturnThinking: config.ReturnThinking,
					StreamThinking: config.StreamThinking, InterleaveThinking: config.InterleaveThinking}
			}
			options.Metadata[key] = value
		}
	}
	options.ToolChoice = o.ToolChoice
	if choice, ok := o.ToolChoice.(gent.ToolChoice); ok {
		options.ToolChoice = llms.ToolChoice{Type: choice.Type,
			Function: &llms.FunctionReference{Name: choice.Function.Name}}
	}
	for _, tool := range o.Tools {
		f := tool.Function
		options.Tools = append(options.Tools, llms.Tool{Type: tool.Type,
			Function: &llms.FunctionDefinition{Name: f.Name, Description: f.Description,
				Parameters: f.Parameters, Strict: f.Strict}})
	}
	messages := make([]llms.MessageContent, len(request.Messages))
	for i, message := range request.Messages {
		if message.Continuation != nil {
			return nil, options, fmt.Errorf("LangChainGo cannot replay %s continuation items",
				message.Continuation.Provider)
		}
		messages[i].Role = llms.ChatMessageType(message.Role)
		for _, part := range message.Parts {
			converted, err := lcgPart(part)
			if err != nil {
				return nil, options, err
			}
			messages[i].Parts = append(messages[i].Parts, converted)
		}
	}
	return messages, options, nil
}

func lcgPart(part gent.ContentPart) (llms.ContentPart, error) {
	switch p := part.(type) {
	case gent.TextContent:
		return llms.TextContent{Text: p.Text}, nil
	case gent.ImageURLContent:
		return llms.ImageURLContent{URL: p.URL, Detail: p.Detail}, nil
	case gent.BinaryContent:
		return llms.BinaryContent{MIMEType: p.MIMEType, Data: p.Data}, nil
	case gent.ModelToolCall:
		return llms.ToolCall{ID: p.ID, Type: p.Type,
			FunctionCall: &llms.FunctionCall{
				Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Arguments}}, nil
	case gent.ToolCallResponse:
		return llms.ToolCallResponse{ToolCallID: p.ToolCallID, Name: p.Name, Content: p.Content}, nil
	case gent.CachedContent:
		inner, err := lcgPart(p.ContentPart)
		if err != nil {
			return nil, err
		}
		return llms.CachedContent{ContentPart: inner, CacheControl: &llms.CacheControl{
			Type: p.CacheControl.Type, Duration: p.CacheControl.Duration}}, nil
	default:
		return nil, fmt.Errorf("LangChainGo cannot encode content %T", part)
	}
}

func gentCalls(calls []llms.ToolCall) []gent.ModelToolCall {
	if calls == nil {
		return nil
	}
	converted := make([]gent.ModelToolCall, len(calls))
	for i, call := range calls {
		converted[i] = gent.ModelToolCall{ID: call.ID, Type: call.Type}
		if call.FunctionCall != nil {
			converted[i].FunctionCall = &gent.FunctionCall{
				Name: call.FunctionCall.Name, Arguments: call.FunctionCall.Arguments}
		}
	}
	return converted
}

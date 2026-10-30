package gent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"
)

// ModelMessage is the provider-neutral request message. Continuation contains the exact ordered
// provider items for an assistant turn. An adapter must preserve them or reject the request.
type ModelMessage struct {
	Role         ChatMessageType
	Parts        []ContentPart
	Continuation *ProviderContinuation
}

type ProviderContinuation struct {
	Provider string
	Items    []json.RawMessage
}

type ChatMessageType string

const (
	ChatMessageTypeSystem ChatMessageType = "system"
	ChatMessageTypeHuman  ChatMessageType = "human"
	ChatMessageTypeAI     ChatMessageType = "ai"
	ChatMessageTypeTool   ChatMessageType = "tool"
)

// ContentPart is implemented by the supported text, media, and native call values.
type ContentPart interface{ isContentPart() }

type TextContent struct{ Text string }

func (TextContent) isContentPart()   {}
func (p TextContent) String() string { return p.Text }

type ImageURLContent struct {
	URL    string
	Detail string
}

func (ImageURLContent) isContentPart()   {}
func (p ImageURLContent) String() string { return p.URL }

type BinaryContent struct {
	MIMEType string
	Data     []byte
}

func (BinaryContent) isContentPart() {}
func (p BinaryContent) String() string {
	return "data:" + p.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(p.Data)
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ModelToolCall struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	FunctionCall *FunctionCall `json:"function,omitempty"`
}

func (ModelToolCall) isContentPart() {}

type ToolCallResponse struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
}

func (ToolCallResponse) isContentPart() {}

type ToolDefinition struct {
	Type     string              `json:"type"`
	Function *FunctionDefinition `json:"function,omitempty"`
}

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

type ToolChoice struct {
	Type     string             `json:"type"`
	Function *FunctionReference `json:"function,omitempty"`
}
type FunctionReference struct {
	Name string `json:"name"`
}

type CacheControl struct {
	Type     string
	Duration time.Duration
}
type CachedContent struct {
	ContentPart
	CacheControl *CacheControl
}

func (CachedContent) isContentPart() {}

func TextParts(role ChatMessageType, texts ...string) *ModelMessage {
	message := &ModelMessage{Role: role, Parts: []ContentPart{}}
	for _, text := range texts {
		message.Parts = append(message.Parts, TextContent{Text: text})
	}
	return message
}
func ImageURLPart(url string) ImageURLContent { return ImageURLContent{URL: url} }
func BinaryPart(mime string, data []byte) BinaryContent {
	return BinaryContent{MIMEType: mime, Data: data}
}

// CallOptions describes provider controls independently of an SDK. Unsupported settings must
// produce an adapter error; silently dropping a setting can change execution behavior.
// Temperature and TopP are optional. A pointer preserves an explicit zero.
type CallOptions struct {
	Model                  string
	CandidateCount         int
	MaxTokens              int
	Temperature            *float64
	StopWords              []string
	TopK                   int
	TopP                   *float64
	Seed                   int
	MinLength              int
	MaxLength              int
	N                      int
	RepetitionPenalty      float64
	FrequencyPenalty       float64
	PresencePenalty        float64
	JSONMode               bool
	Tools                  []ToolDefinition
	ToolChoice             any
	Metadata               map[string]any
	ResponseMIMEType       string
	ResponseSchema         map[string]any
	ResponseModalities     []string
	ImageMaxWidth          int
	ImageMaxHeight         int
	ImageMaxFileSizeBytes  int64
	ImageMIMETypes         []string
	StreamingFunc          func(context.Context, []byte) error         `json:"-"`
	StreamingReasoningFunc func(context.Context, []byte, []byte) error `json:"-"`
}

type CallOption func(*CallOptions)

func WithModel(model string) CallOption {
	return func(o *CallOptions) { o.Model = model }
}
func WithMaxTokens(n int) CallOption {
	return func(o *CallOptions) { o.MaxTokens = n }
}
func WithTemperature(n float64) CallOption {
	return func(o *CallOptions) { o.Temperature = &n }
}
func WithTopP(n float64) CallOption {
	return func(o *CallOptions) { o.TopP = &n }
}
func WithTools(tools []ToolDefinition) CallOption {
	return func(o *CallOptions) { o.Tools = tools }
}
func WithOptions(options *CallOptions) CallOption {
	return func(o *CallOptions) { *o = *options }
}

// WithStreamingFunc receives answer or unlabelled text. Read Stream.Chunks for all channels,
// including provider-labelled commentary, without mixing them into the answer callback.
func WithStreamingFunc(f func(context.Context, []byte) error) CallOption {
	return func(o *CallOptions) { o.StreamingFunc = f }
}
func WithStreamingReasoningFunc(f func(context.Context, []byte, []byte) error) CallOption {
	return func(o *CallOptions) { o.StreamingReasoningFunc = f }
}

type ThinkingMode string
type ThinkingConfig struct {
	Mode               ThinkingMode
	BudgetTokens       int
	ReturnThinking     bool
	StreamThinking     bool
	InterleaveThinking bool
}

func WithStreamThinking(enabled bool) CallOption {
	return func(o *CallOptions) {
		if o.Metadata == nil {
			o.Metadata = map[string]any{}
		}
		config, ok := o.Metadata["thinking_config"].(*ThinkingConfig)
		if !ok {
			config = &ThinkingConfig{Mode: "auto"}
			o.Metadata["thinking_config"] = config
		}
		config.StreamThinking = enabled
	}
}

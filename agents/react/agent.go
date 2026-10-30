package react

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/output"
	"github.com/rickchristie/gent/toolchain"
)

// ----------------------------------------------------------------------------
// Agent - ReAct Loop Implementation
// ----------------------------------------------------------------------------

// Agent sends native tool definitions, executes calls, and validates final assistant content.
// Requests contain the caller-authored Task and active history. Tools remain structured
// definitions; output controls come from Output.Configure, independently of prompt text.
type Agent[T any] struct {
	model                  gent.Model
	format                 gent.TextFormat
	toolChain              gent.ToolChain
	output                 gent.Output[T]
	callOptions            []gent.CallOption
	userSampling           gent.CallOptions
	repetitionConfig       agentutil.RepetitionConfig
	maxResponseChars       int
	responseTooLongMessage string
	recoveryVersion        string
}

// NewAgent creates a text-output agent. The caller supplies the complete starting Task.
func NewAgent(model gent.Model) *Agent[string] {
	return NewWithOutput(model, output.NewTextOutput())
}

func NewWithOutput[T any](model gent.Model, out gent.Output[T]) *Agent[T] {
	return &Agent[T]{model: model, output: out, format: format.NewXML(), toolChain: toolchain.New(),
		repetitionConfig:       agentutil.DefaultRepetitionConfig(),
		maxResponseChars:       agentutil.DefaultMaxResponseChars,
		responseTooLongMessage: agentutil.DefaultResponseTooLongMessage}
}

func (r *Agent[T]) Output() gent.Output[T] { return r.output }

// WithToolResultFormat selects the renderer for tool results and loop recovery messages.
func (r *Agent[T]) WithToolResultFormat(f gent.TextFormat) *Agent[T] {
	r.format = f
	return r
}

// WithToolChain sets the tool chain.
func (r *Agent[T]) WithToolChain(tc gent.ToolChain) *Agent[T] {
	r.toolChain = tc
	return r
}

// WithRepetitionConfig sets the streaming repetition detection
// configuration. See [agentutil.RepetitionConfig] for available options.
// Default: [agentutil.DefaultRepetitionConfig] (enabled, block size 400,
// threshold 3).
func (r *Agent[T]) WithRepetitionConfig(
	cfg agentutil.RepetitionConfig,
) *Agent[T] {
	r.repetitionConfig = cfg
	return r
}

// WithMaxResponseChars sets the maximum characters allowed in a single model response.
// When exceeded, the stream is cancelled and the accumulated content is inspected for
// repetition. If repetition is detected, the loop recovery path runs. If not, a "too long"
// reminder is injected instead. Default: [agentutil.DefaultMaxResponseChars] (16000).
// Set to 0 to disable.
func (r *Agent[T]) WithMaxResponseChars(n int) *Agent[T] {
	r.maxResponseChars = n
	return r
}

// WithResponseTooLongMessage sets the message injected when a response exceeds
// MaxResponseChars but is NOT a repetition loop.
// Default: [agentutil.DefaultResponseTooLongMessage].
func (r *Agent[T]) WithResponseTooLongMessage(msg string) *Agent[T] {
	r.responseTooLongMessage = msg
	return r
}

// WithCallOptions resolves the supplied functions once into reusable configuration. The saved
// compatibility manifest and every request therefore use the same option values.
func (r *Agent[T]) WithCallOptions(options ...gent.CallOption) *Agent[T] {
	resolved := gent.NewModelCallRequest(nil).Options
	for _, option := range options {
		option(&resolved)
	}
	r.userSampling = resolved
	defaults := gent.DefaultSamplingParams(r.model)
	if resolved.Temperature == nil {
		if value, send := defaults.Temperature.EffectiveValue(); send {
			resolved.Temperature = &value
		}
	}
	if resolved.TopP == nil {
		if value, send := defaults.TopP.EffectiveValue(); send {
			resolved.TopP = &value
		}
	}
	r.callOptions = []gent.CallOption{gent.WithOptions(&resolved)}
	return r
}

// RegisterTool adds a tool to the tool chain.
func (r *Agent[T]) RegisterTool(tool any) *Agent[T] {
	r.toolChain.RegisterTool(tool)
	return r
}

// next reuses a saved response or calls the model, then executes native tools or validates an
// answer. Tool calls take priority: results must be observed before the agent can finish.
func (r *Agent[T]) next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	frame, err := r.loadFrame(execCtx)
	if err != nil {
		return nil, err
	}

	if frame.Response == nil {
		messages, err := r.buildMessages(execCtx)
		if err != nil {
			return nil, err
		}
		streamId := fmt.Sprintf("iter-%d", execCtx.Iteration())
		response, repetition, err := r.callModel(execCtx, streamId, "llm-response", messages)
		if err != nil {
			return nil, fmt.Errorf("model call failed: %w", err)
		}
		frame.Response, frame.Repetition = response, repetition
		execCtx.ReleaseRequiredRecords()
		if err := saveFrame(execCtx, frame); err != nil {
			return nil, err
		}
	}
	return r.nextNative(execCtx, frame)
}

func (r *Agent[T]) handleResponseTooLong(
	execCtx *gent.ExecutionContext,
	response *gent.ContentResponse,
) (*gent.AgentLoopResult, error) {
	responseContent := ""
	if response != nil && len(response.Choices) > 0 {
		responseContent = gent.ContentText(response.Choices[0].CandidateContent)
	}

	// Expired record with truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Response exceeded max length")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	if err := execCtx.AppendRecord(expiredIter, true); err != nil {
		return nil, err
	}

	// Conciseness reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: r.responseTooLongMessage},
	})
	reminderIter := &gent.HistoryEntry{
		Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  gent.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{gent.TextContent{Text: reminder}},
		}},
	}
	if err := execCtx.AppendRecord(reminderIter, true); err != nil {
		return nil, err
	}

	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
}

// handleRepetition processes a detected repetition loop. For RepetitionRecover, it
// increments stats, expires the poisoned response, and injects a recovery reminder.
// For RepetitionTerminate, it returns a fatal error.
func (r *Agent[T]) handleRepetition(
	execCtx *gent.ExecutionContext,
	response *gent.ContentResponse, rep *agentutil.RepetitionResult,
) (*gent.AgentLoopResult, error) {
	// Update stats.
	execCtx.Stats().IncrCounter(gent.SCRepetitionLoopTotal, 1)
	execCtx.Stats().IncrGauge(gent.SGRepetitionLoopConsecutive, 1)

	if r.repetitionConfig.Action == agentutil.RepetitionTerminate {
		return nil, fmt.Errorf("model call failed: %w", rep.Err)
	}

	// Recover: build the reminder message.
	cfg := r.repetitionConfig
	filtered := ""
	if cfg.Filter != nil && rep.RepeatedBlock != "" {
		filtered = cfg.Filter(rep.RepeatedBlock, cfg.PoisonKeywords)
	}

	count := execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive)
	var reminderText string
	if filtered != "" {
		reminderText = cfg.RecoverMessage
		reminderText = strings.Replace(reminderText, "{block}", filtered, 1)
		reminderText = strings.Replace(
			reminderText, "{count}",
			fmt.Sprintf("%d", int(count)), 1,
		)
	} else {
		reminderText = cfg.RecoverPoisonedMessage
	}

	// Extract raw response content for the expired iteration.
	responseContent := ""
	if response != nil && len(response.Choices) > 0 {
		responseContent = gent.ContentText(response.Choices[0].CandidateContent)
	}

	// 1. Expired record with the truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Repetition loop detected")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	if err := execCtx.AppendRecord(expiredIter, true); err != nil {
		return nil, err
	}

	// 2. Recovery reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: reminderText},
	})
	reminderIter := &gent.HistoryEntry{
		Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  gent.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{gent.TextContent{Text: reminder}},
		}},
	}
	if err := execCtx.AppendRecord(reminderIter, true); err != nil {
		return nil, err
	}

	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
}

// buildIteration groups assistant text and human feedback. Tool results use nativeToolEntry.
func (r *Agent[T]) buildIteration(response, observation string) *gent.HistoryEntry {
	var messages []*gent.MessageContent

	// Assistant message (response)
	messages = append(messages, &gent.MessageContent{
		Role:  gent.ChatMessageTypeAI,
		Parts: []gent.ContentPart{gent.TextContent{Text: response}},
	})

	// Observation message (Human role) - only if there's an observation
	if observation != "" {
		messages = append(messages, &gent.MessageContent{
			Role:  gent.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{gent.TextContent{Text: observation}},
		})
	}

	return &gent.HistoryEntry{
		Messages: messages,
	}
}

// callModel calls the model via streaming and returns the complete response.
// Also returns a non-nil RepetitionResult if a loop was detected.
func (r *Agent[T]) callModel(
	execCtx *gent.ExecutionContext, streamId string,
	streamTopicId string, messages []*gent.ModelMessage,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	opts := r.effectiveCallOptions()
	tools, err := r.toolChain.NativeTools(execCtx)
	if err != nil {
		return nil, nil, err
	}
	// The registry owns the catalog for this model request.
	opts = append(opts, gent.WithTools(tools))
	r.warnRestrictiveSampling(execCtx, opts)
	return r.callModelStreaming(execCtx, streamId, streamTopicId, messages, opts)
}

// effectiveCallOptions returns the merged call options: model-appropriate defaults followed
// by user options. User options are applied last and override defaults.
func (r *Agent[T]) effectiveCallOptions() []gent.CallOption {
	params := gent.DefaultSamplingParams(r.model)

	var opts []gent.CallOption
	if v, ok := params.Temperature.EffectiveValue(); ok {
		opts = append(opts, gent.WithTemperature(v))
	}
	if v, ok := params.TopP.EffectiveValue(); ok {
		opts = append(opts, gent.WithTopP(v))
	}

	opts = append(opts, r.callOptions...)
	return opts
}

// warnRestrictiveSampling publishes a warning event when the effective temperature and
// top-p are both set to restrictive values. Vendor guidance (Anthropic, OpenAI) recommends
// adjusting one parameter at a time.
func (r *Agent[T]) warnRestrictiveSampling(
	execCtx *gent.ExecutionContext, opts []gent.CallOption,
) {
	if execCtx == nil {
		return
	}

	// Resolve effective values by applying all options.
	var resolved gent.CallOptions
	for _, opt := range opts {
		opt(&resolved)
	}

	// Both restrictive: temperature < 0.3 (but set) and top-p < 0.8 (but set).
	// Nil pointers mean unset.
	if resolved.Temperature != nil && *resolved.Temperature < 0.3 &&
		resolved.TopP != nil && *resolved.TopP < 0.8 {
		execCtx.PublishCommonEvent(
			"gent:sampling_params_restrictive",
			fmt.Sprintf(
				"temperature (%.2f) and top-p (%.2f) are both restrictive; "+
					"adjust one at a time",
				*resolved.Temperature, *resolved.TopP,
			),
			nil,
		)
	}

	// Only user options can set parameters that the model does not support.
	params := gent.DefaultSamplingParams(r.model)
	userResolved := r.userSampling
	if params.Temperature.Directive == gent.ParamForbidden &&
		userResolved.Temperature != nil {
		execCtx.PublishCommonEvent(
			"gent:sampling_param_forbidden",
			fmt.Sprintf(
				"temperature (%.2f) is forbidden for this model "+
					"and may cause an API error",
				*userResolved.Temperature,
			),
			nil,
		)
	}
	if params.TopP.Directive == gent.ParamForbidden &&
		userResolved.TopP != nil {
		execCtx.PublishCommonEvent(
			"gent:sampling_param_forbidden",
			fmt.Sprintf(
				"top-p (%.2f) is forbidden for this model "+
					"and may cause an API error",
				*userResolved.TopP,
			),
			nil,
		)
	}
}

// callModelStreaming calls the model with streaming and applies repetition detection.
// Returns the response plus an optional RepetitionResult if a loop or max-size was hit.
func (r *Agent[T]) callModelStreaming(
	execCtx *gent.ExecutionContext,
	streamId string, streamTopicId string,
	messages []*gent.ModelMessage, opts []gent.CallOption,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	request := gent.NewModelCallRequest(messages, opts...)
	if err := r.output.Configure(&request); err != nil {
		return nil, nil, err
	}
	stream, err := execCtx.GenerateContentStream(
		r.model,
		gent.ModelCall{StreamId: streamId, StreamTopicId: streamTopicId},
		request,
	)
	if err != nil {
		return nil, nil, err
	}

	acc := gent.NewStreamAccumulator()
	detector := agentutil.NewRepetitionDetector(r.repetitionConfig)
	totalChars := 0

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			// Completion accepts final model evidence before the executor can end the run.
			stream.Close()
			_, completionErr := stream.Response()
			if completionErr != nil {
				if errors.Is(completionErr, chunk.Err) {
					return nil, nil, completionErr
				}
				return nil, nil, errors.Join(chunk.Err, completionErr)
			}
			return nil, nil, chunk.Err
		}
		acc.Add(chunk)
		text := gent.MetaText(chunk.MetaContent, gent.MetaContentIntermediary)
		totalChars += len(text)

		// Real-time repetition detection.
		if result := detector.Feed(text); result != nil {
			return finishGuardedStream(execCtx, stream, acc, result)
		}

		// Max response size: truncate and inspect for repetition.
		if r.maxResponseChars > 0 && totalChars > r.maxResponseChars {
			// Two-step check: is the truncated content a loop or just too long?
			if repResult := detector.CheckAccumulated(); repResult != nil {
				return finishGuardedStream(execCtx, stream, acc, repResult)
			}
			return finishGuardedStream(execCtx, stream, acc,
				&agentutil.RepetitionResult{Err: agentutil.ErrMaxResponseSize})
		}
	}

	streamResponse, err := stream.Response()
	if err != nil {
		return nil, nil, err
	}
	// The final response can include content or choice data that is absent from chunks.
	// Use the accumulator only when repetition or size limits stop the stream early.
	// Check final-only content too. Some adapters stream no text when they return tool calls.
	if len(streamResponse.Choices) > 0 && streamResponse.Choices[0] != nil {
		choice := streamResponse.Choices[0]
		// Terminal provider state needs no text guard. nextNative validates the saved choice
		// before it records refusal or reports failure.
		if choice.Completion.Status == gent.ModelStatusRefused ||
			choice.Completion.Status == gent.ModelStatusFailed {
			return streamResponse, nil, nil
		}
		content := gent.ContentText(choice.CandidateContent)
		for _, call := range choice.ToolCalls {
			if call.FunctionCall != nil {
				content += call.FunctionCall.Arguments
			}
		}
		finalDetector := agentutil.NewRepetitionDetector(r.repetitionConfig)
		if repetition := finalDetector.Feed(content); repetition != nil {
			return streamResponse, repetition, nil
		}
		if r.maxResponseChars > 0 && len(content) > r.maxResponseChars {
			if repetition := finalDetector.CheckAccumulated(); repetition != nil {
				return streamResponse, repetition, nil
			}
			return streamResponse, &agentutil.RepetitionResult{Err: agentutil.ErrMaxResponseSize}, nil
		}
	}
	return streamResponse, nil, nil
}

// Compile-time check that Agent implements gent.Loop.
var _ gent.Loop[string] = (*Agent[string])(nil)

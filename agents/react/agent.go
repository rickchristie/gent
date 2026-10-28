package react

import (
	"errors"
	"fmt"
	"strings"

	"time"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/format"

	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
)

// ----------------------------------------------------------------------------
// Agent - ReAct AgentLoop Implementation
// ----------------------------------------------------------------------------

// Agent sends native tool definitions, executes calls, and validates final assistant content.
// Requests contain the system prompt, user task, and active history. Customize prompt sections
// with WithSystemPromptBuilder; tools remain structured definitions in the model request.
type Agent struct {
	behaviorAndContext     string
	criticalRules          string
	systemPromptBuilder    SystemPromptBuilder
	model                  gent.Model
	format                 gent.TextFormat
	toolChain              gent.ToolChain
	termination            gent.Termination
	timeProvider           gent.TimeProvider
	callOptions            []gent.CallOption
	userSampling           gent.CallOptions
	repetitionConfig       agentutil.RepetitionConfig
	maxResponseChars       int
	responseTooLongMessage string
	recoveryVersion        string
}

// NewAgent creates a new Agent with the given model and default settings.
// Defaults:
//   - Format: format.NewXML()
//   - ToolChain: toolchain.New()
//   - Termination: termination.NewText("answer")
//   - TimeProvider: gent.NewDefaultTimeProvider()
//   - SystemPromptBuilder: DefaultSystemPromptBuilder
func NewAgent(model gent.Model) *Agent {
	return &Agent{
		model:                  model,
		format:                 format.NewXML(),
		toolChain:              toolchain.New(),
		termination:            termination.NewText("answer"),
		timeProvider:           gent.NewDefaultTimeProvider(),
		systemPromptBuilder:    DefaultSystemPromptBuilder,
		repetitionConfig:       agentutil.DefaultRepetitionConfig(),
		maxResponseChars:       agentutil.DefaultMaxResponseChars,
		responseTooLongMessage: agentutil.DefaultResponseTooLongMessage,
	}
}

// WithBehaviorAndContext sets behavior instructions and context to include in the system prompt.
// This is passed to the SystemPromptBuilder and formatted as a "behavior" section.
// Use WithSystemPromptBuilder() to completely replace how the system prompt is built.
func (r *Agent) WithBehaviorAndContext(prompt string) *Agent {
	r.behaviorAndContext = prompt
	return r
}

// WithCriticalRules sets critical rules to include in the system prompt.
// Critical rules are placed in a separate "critical_rules" section.
func (r *Agent) WithCriticalRules(rules string) *Agent {
	r.criticalRules = rules
	return r
}

// WithSystemPromptBuilder sets a custom function to build the system prompt.
// Use this for full control over the system prompt structure.
// See DefaultSystemPromptBuilder for the expected behavior.
func (r *Agent) WithSystemPromptBuilder(builder SystemPromptBuilder) *Agent {
	r.systemPromptBuilder = builder
	return r
}

// WithFormat sets the text output format. Native agents use it to render prompts and tool results;
// they do not parse the assistant response with this format.
func (r *Agent) WithFormat(f gent.TextFormat) *Agent {
	r.format = f
	return r
}

// WithToolChain sets the tool chain.
func (r *Agent) WithToolChain(tc gent.ToolChain) *Agent {
	r.toolChain = tc
	return r
}

// WithTermination sets the termination handler.
func (r *Agent) WithTermination(t gent.Termination) *Agent {
	r.termination = t
	return r
}

// WithTimeProvider sets the time provider.
// Use this to inject a mock time provider for testing.
func (r *Agent) WithTimeProvider(tp gent.TimeProvider) *Agent {
	r.timeProvider = tp
	return r
}

// WithClock sets the clock used by all standard time helpers in prompt templates.
// Use WithTimeProvider to also change how dates and times are formatted.
func (r *Agent) WithClock(now func() time.Time) *Agent {
	return r.WithTimeProvider(gent.TimeProviderFunc(now))
}

// TimeProvider returns the current time provider.
func (r *Agent) TimeProvider() gent.TimeProvider {
	return r.timeProvider
}

// WithRepetitionConfig sets the streaming repetition detection
// configuration. See [agentutil.RepetitionConfig] for available options.
// Default: [agentutil.DefaultRepetitionConfig] (enabled, block size 400,
// threshold 3).
func (r *Agent) WithRepetitionConfig(
	cfg agentutil.RepetitionConfig,
) *Agent {
	r.repetitionConfig = cfg
	return r
}

// WithMaxResponseChars sets the maximum characters allowed in a single model response.
// When exceeded, the stream is cancelled and the accumulated content is inspected for
// repetition. If repetition is detected, the loop recovery path runs. If not, a "too long"
// reminder is injected instead. Default: [agentutil.DefaultMaxResponseChars] (16000).
// Set to 0 to disable.
func (r *Agent) WithMaxResponseChars(n int) *Agent {
	r.maxResponseChars = n
	return r
}

// WithResponseTooLongMessage sets the message injected when a response exceeds
// MaxResponseChars but is NOT a repetition loop.
// Default: [agentutil.DefaultResponseTooLongMessage].
func (r *Agent) WithResponseTooLongMessage(msg string) *Agent {
	r.responseTooLongMessage = msg
	return r
}

// WithCallOptions resolves the supplied functions once into reusable configuration. The saved
// compatibility manifest and every request therefore use the same option values.
func (r *Agent) WithCallOptions(options ...gent.CallOption) *Agent {
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
func (r *Agent) RegisterTool(tool any) *Agent {
	r.toolChain.RegisterTool(tool)
	return r
}

// next reuses a saved response or calls the model, then executes native tools or validates an
// answer. Tool calls take priority: results must be observed before the agent can finish.
func (r *Agent) next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
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
		if err := saveFrame(execCtx, frame); err != nil {
			return nil, err
		}
	}
	response, repResult := frame.Response, frame.Repetition

	// Handle repetition or max-size truncation.
	if repResult != nil {
		if errors.Is(repResult.Err, agentutil.ErrMaxResponseSize) {
			return r.handleResponseTooLong(execCtx, response)
		}
		return r.handleRepetition(execCtx, response, repResult)
	}
	return r.nextNative(execCtx, frame)
}

func (r *Agent) handleResponseTooLong(
	execCtx *gent.ExecutionContext,
	response *gent.ContentResponse,
) (*gent.AgentLoopResult, error) {
	responseContent := ""
	if response != nil && len(response.Choices) > 0 {
		responseContent = response.Choices[0].CandidateContent
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

	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: reminder}, nil
}

// handleRepetition processes a detected repetition loop. For RepetitionRecover, it
// increments stats, expires the poisoned response, and injects a recovery reminder.
// For RepetitionTerminate, it returns a fatal error.
func (r *Agent) handleRepetition(
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
		responseContent = response.Choices[0].CandidateContent
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

	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: reminder}, nil
}

// buildIteration groups assistant text and human feedback. Tool results use nativeToolEntry.
func (r *Agent) buildIteration(response, observation string) *gent.HistoryEntry {
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
func (r *Agent) callModel(
	execCtx *gent.ExecutionContext, streamId string,
	streamTopicId string, messages []gent.ModelMessage,
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
func (r *Agent) effectiveCallOptions() []gent.CallOption {
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
func (r *Agent) warnRestrictiveSampling(
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
func (r *Agent) callModelStreaming(
	execCtx *gent.ExecutionContext,
	streamId string, streamTopicId string,
	messages []gent.ModelMessage, opts []gent.CallOption,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	stream, err := execCtx.GenerateContentStream(
		r.model,
		gent.ModelCall{StreamId: streamId, StreamTopicId: streamTopicId},
		gent.NewModelCallRequest(messages, opts...),
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
			stream.Close()
			streamResp, _ := stream.Response()
			return acc.ResponseWithInfo(streamResp), result, nil
		}

		// Max response size: truncate and inspect for repetition.
		if r.maxResponseChars > 0 && totalChars > r.maxResponseChars {
			stream.Close()
			streamResp, _ := stream.Response()
			resp := acc.ResponseWithInfo(streamResp)

			// Two-step check: is the truncated content a loop or just too long?
			if repResult := detector.CheckAccumulated(); repResult != nil {
				return resp, repResult, nil
			}
			return resp, &agentutil.RepetitionResult{Err: agentutil.ErrMaxResponseSize}, nil
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
		content := choice.CandidateContent
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
var _ gent.Loop = (*Agent)(nil)

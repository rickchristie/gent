package toolchain

import (
	"fmt"

	"github.com/rickchristie/gent"
)

func recoveryMessage(
	providers map[string]gent.RecoveryMessageProvider, info *gent.RecoveryInfo,
) string {
	if provider := providers[info.Name]; provider != nil {
		return provider(info)
	}
	return ""
}

func (c *Registry) RecoveryMessage(info *gent.RecoveryInfo) string {
	return recoveryMessage(c.recovery, info)
}

func (c *SearchChain) RecoveryMessage(info *gent.RecoveryInfo) string {
	c.mu.RLock()
	provider := c.recovery[info.Name]
	c.mu.RUnlock()
	if provider != nil {
		return provider(info)
	}
	return ""
}

// WithRecoveryMessage supplies guidance for an interrupted program. It must be set before use.
func (w *JsToolChainWrapper) WithRecoveryMessage(
	provider gent.RecoveryMessageProvider,
) *JsToolChainWrapper {
	w.recoveryMessage = provider
	return w
}

func (w *JsToolChainWrapper) RecoveryMessage(info *gent.RecoveryInfo) string {
	if info.Name != nativeCodeTool {
		return w.wrapped.RecoveryMessage(info)
	}
	if w.recoveryMessage != nil {
		return w.recoveryMessage(info)
	}
	return ""
}

func (c *Registry) RecoveryConfig() any {
	definitions, err := nativeDefinitions(c.tools, c.printOutputSchema)
	if err != nil {
		panic(err)
	}
	return definitions
}

func (c *SearchChain) RecoveryConfig() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	definitions, err := nativeDefinitions(c.tools, c.printOutputSchema)
	if err != nil {
		panic(err)
	}
	return map[string]any{"tools": definitions, "search": c.nativeSearchTools(),
		"pinned": c.pinnedToolNames, "pageSize": c.pageSize, "noResults": c.noResultsMessage}
}

// unknownTool reports saved facts separately from application guidance. The callback sees a clone,
// so it cannot change the saved input or another callback's evidence.
func unknownTool(ctx *gent.ExecutionContext, call *gent.SavedToolCall, chain gent.ToolChain) (
	*gent.ToolCallResult, error,
) {
	input, err := ctx.Codecs().Decode(call.Input)
	if err != nil {
		return nil, err
	}
	info := &gent.RecoveryInfo{RunId: ctx.RunId(), ContextId: ctx.ContextId(),
		ToolCallId: call.Id, AttemptId: call.AttemptId, Name: call.Name, Args: call.Args, Input: input}
	owned, err := ctx.Codecs().Clone(info)
	if err != nil {
		return nil, err
	}
	guidance := chain.RecoveryMessage(owned.(*gent.RecoveryInfo))
	text := fmt.Sprintf("Tool %s (call %s):\n%s", call.Name, call.Id, gent.DefaultRecoveryMessage)
	if guidance != "" {
		text += "\n\nRecovery guidance:\n" + guidance
	}
	result := &gent.ToolCallResult{ToolCallId: call.Id, AttemptId: call.AttemptId,
		Name: call.Name, Args: call.Args, Input: input, Outcome: "unknown",
		Error: gent.ErrUncertainTool, Text: text, Instructions: guidance, SummaryCaptured: true}
	ctx.PublishAfterToolCall(call.Name, input, result, 0, result.Error,
		gent.WithToolCallId(call.Id), gent.WithToolCallAttempt(call.AttemptId))
	return result, ctx.RecordingError()
}

func skippedTool(plan *gent.ToolPlan) *gent.ToolCallResult {
	call := plan.Calls[plan.NextCall]
	return &gent.ToolCallResult{ToolCallId: call.Id, Name: call.Name, Args: call.Args,
		Outcome: "not_started", SummaryCaptured: true,
		Text: "This call did not start. Review the interrupted operation before making a new plan."}
}

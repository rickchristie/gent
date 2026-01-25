// Package loggers provides reusable logging hooks for integration testing.
package loggers

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
	"gopkg.in/yaml.v3"
)

// LoggerHook implements all hook interfaces to log everything that happens during execution.
// All structs are logged as YAML with block scalars for easy reading.
// Nothing is truncated - full content is always logged.
type LoggerHook struct {
	out io.Writer
}

// NewLoggerHook creates a new LoggerHook that writes to stdout.
func NewLoggerHook() *LoggerHook {
	return &LoggerHook{
		out: os.Stdout,
	}
}

// NewLoggerHookWithWriter creates a new LoggerHook that writes to the given writer.
func NewLoggerHookWithWriter(w io.Writer) *LoggerHook {
	return &LoggerHook{
		out: w,
	}
}

// logEvent logs an event header with timestamp.
func (h *LoggerHook) logEvent(name string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(h.out, "\n>>> [%s]: %s\n", name, timestamp)
}

// log writes a line without any prefix.
func (h *LoggerHook) log(format string, args ...any) {
	fmt.Fprintf(h.out, format+"\n", args...)
}

func (h *LoggerHook) logYAML(v any) {
	data, err := yaml.Marshal(v)
	if err != nil {
		h.log("(failed to marshal: %v)", err)
		return
	}
	fmt.Fprint(h.out, string(data))
}

// OnBeforeExecution logs execution start with original input.
func (h *LoggerHook) OnBeforeExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeExecutionEvent,
) {
	h.logEvent("BeforeExecution")
	h.log("================================================================================")
	h.log("EXECUTION STARTED")
	h.log("================================================================================")
	h.log("Name: %s", execCtx.Name())

	// Log task
	data := execCtx.Data()
	if data != nil {
		task := data.GetTask()
		if len(task) > 0 {
			h.log("")
			h.log("Task:")
			for _, part := range task {
				if tc, ok := part.(llms.TextContent); ok {
					h.log("  %s", tc.Text)
				}
			}
		}
	}
}

// OnAfterExecution logs execution completion with final stats.
func (h *LoggerHook) OnAfterExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterExecutionEvent,
) {
	h.logEvent("AfterExecution")
	h.log("================================================================================")
	h.log("EXECUTION COMPLETED")
	h.log("================================================================================")

	eventData := map[string]any{
		"termination_reason": string(event.TerminationReason),
	}
	if event.Error != nil {
		eventData["error"] = event.Error.Error()
	}
	h.logYAML(eventData)

	// Log final stats
	h.log("")
	h.log("Stats:")
	stats := execCtx.Stats()
	statsData := map[string]any{
		"total_iterations":    execCtx.Iteration(),
		"total_input_tokens":  stats.GetTotalInputTokens(),
		"total_output_tokens": stats.GetTotalOutputTokens(),
		"total_cost":          stats.GetTotalCost(),
		"total_tool_calls":    stats.GetToolCallCount(),
		"counters":            stats.Counters(),
		"gauges":              stats.Gauges(),
	}
	h.logYAML(statsData)
}

// OnBeforeIteration logs iteration start.
func (h *LoggerHook) OnBeforeIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeIterationEvent,
) {
	h.logEvent(fmt.Sprintf("BeforeIteration %d", event.Iteration))
	h.log("--------------------------------------------------------------------------------")
	h.log("ITERATION %d START", event.Iteration)
	h.log("--------------------------------------------------------------------------------")
}

// OnAfterIteration logs iteration end with the AgentLoopResult.
func (h *LoggerHook) OnAfterIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterIterationEvent,
) {
	h.logEvent(fmt.Sprintf("AfterIteration %d", event.Iteration))
	h.log("--------------------------------------------------------------------------------")
	h.log("ITERATION %d END", event.Iteration)
	h.log("--------------------------------------------------------------------------------")

	// Log iteration number, duration, and AgentLoopResult
	h.log("Duration: %s", event.Duration)
	h.log("")
	h.log("AgentLoopResult:")

	resultData := map[string]any{
		"action": string(event.Result.Action),
	}
	if event.Result.NextPrompt != "" {
		resultData["next_prompt"] = event.Result.NextPrompt
	}
	if len(event.Result.Result) > 0 {
		var resultParts []string
		for _, part := range event.Result.Result {
			if tc, ok := part.(llms.TextContent); ok {
				resultParts = append(resultParts, tc.Text)
			}
		}
		if len(resultParts) > 0 {
			resultData["result"] = resultParts
		}
	}
	h.logYAML(resultData)
}

// OnError logs errors that occur during execution.
func (h *LoggerHook) OnError(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.ErrorEvent,
) {
	h.logEvent("Error")
	h.logYAML(map[string]any{
		"iteration": event.Iteration,
		"error":     event.Err.Error(),
	})
}

// OnBeforeModelCall logs the request before a model call.
func (h *LoggerHook) OnBeforeModelCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeModelCallEvent,
) {
	h.logEvent(fmt.Sprintf("BeforeModelCall: %s", event.Model))

	// Log request messages
	if event.Request != nil {
		if messages, ok := event.Request.([]llms.MessageContent); ok {
			h.log("Request:")
			for i, msg := range messages {
				h.log("  [%d] Role: %s", i, msg.Role)
				for _, part := range msg.Parts {
					if tc, ok := part.(llms.TextContent); ok {
						h.log("      Content:")
						for _, line := range strings.Split(tc.Text, "\n") {
							h.log("        %s", line)
						}
					}
				}
			}
		}
	}
}

// OnAfterModelCall logs the response after a model call.
func (h *LoggerHook) OnAfterModelCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterModelCallEvent,
) {
	h.logEvent(fmt.Sprintf("AfterModelCall: %s (duration: %s)", event.Model, event.Duration))

	if event.Error != nil {
		h.log("Error: %v", event.Error)
		return
	}

	// Log response
	if event.Response != nil && len(event.Response.Choices) > 0 {
		for i, choice := range event.Response.Choices {
			h.log("Choice[%d]:", i)
			if choice.Content != "" {
				h.log("  Content:")
				for _, line := range strings.Split(choice.Content, "\n") {
					h.log("    %s", line)
				}
			}
			if choice.StopReason != "" {
				h.log("  StopReason: %s", choice.StopReason)
			}
		}
	}

	// Log token info
	if event.Response != nil && event.Response.Info != nil {
		info := event.Response.Info
		h.log("Tokens: input=%d, output=%d, total=%d",
			info.InputTokens, info.OutputTokens, info.TotalTokens)
	}
}

// OnBeforeToolCall logs the tool call before execution.
func (h *LoggerHook) OnBeforeToolCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	h.logEvent(fmt.Sprintf("BeforeToolCall: %s", event.ToolName))
	h.log("Args:")
	h.logYAML(event.Args)
}

// OnAfterToolCall logs the tool call result after execution.
func (h *LoggerHook) OnAfterToolCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterToolCallEvent,
) {
	h.logEvent(fmt.Sprintf("AfterToolCall: %s (duration: %s)", event.ToolName, event.Duration))

	if event.Error != nil {
		h.log("Error: %v", event.Error)
		return
	}

	h.log("Output:")
	h.logYAML(event.Output)
}

// Compile-time checks that LoggerHook implements all hook interfaces.
var (
	_ gent.BeforeExecutionHook = (*LoggerHook)(nil)
	_ gent.AfterExecutionHook  = (*LoggerHook)(nil)
	_ gent.BeforeIterationHook = (*LoggerHook)(nil)
	_ gent.AfterIterationHook  = (*LoggerHook)(nil)
	_ gent.ErrorHook           = (*LoggerHook)(nil)
	_ gent.BeforeModelCallHook = (*LoggerHook)(nil)
	_ gent.AfterModelCallHook  = (*LoggerHook)(nil)
	_ gent.BeforeToolCallHook  = (*LoggerHook)(nil)
	_ gent.AfterToolCallHook   = (*LoggerHook)(nil)
)

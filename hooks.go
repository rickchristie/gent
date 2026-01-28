package gent

// -----------------------------------------------------------------------------
// Executor Hook Interfaces
// -----------------------------------------------------------------------------
//
// Hooks allow observing and intercepting execution at various points.
// To use hooks:
//
//  1. Implement the desired hook interface(s)
//  2. Register with hooks.Registry
//  3. Pass the registry to executor.Config
//
// Example:
//
//	type LoggingHook struct {
//	    logger *log.Logger
//	}
//
//	func (h *LoggingHook) OnBeforeIteration(
//	    execCtx *ExecutionContext,
//	    e *BeforeIterationEvent,
//	) {
//	    h.logger.Printf("Starting iteration %d", e.Iteration)
//	}
//
//	func (h *LoggingHook) OnAfterModelCall(
//	    execCtx *ExecutionContext,
//	    e *AfterModelCallEvent,
//	) {
//	    h.logger.Printf(
//	        "Model %s: %d tokens in %v",
//	        e.Model,
//	        e.Response.Info.InputTokens,
//	        e.Duration,
//	    )
//	}
//
//	// Register and use
//	registry := hooks.NewRegistry()
//	registry.Register(&LoggingHook{logger: log.Default()})
//	exec := executor.New(agent, executor.Config{Hooks: registry})
//
// Hooks receive *ExecutionContext which provides access to the
// underlying context.Context via execCtx.Context(). Use this for
// cancellation checks or passing to external APIs.
//
// # Hook Execution Order
//
// Hooks are called in registration order. For paired hooks
// (Before/After), the After hook is always called if the Before
// hook was called, even on error.
//
// # Error Handling
//
// Hooks should NOT return errors. If a hook panics:
//   - Before hooks: Execution stops, panic propagates
//   - After hooks: Panic propagates after cleanup
//
// Implement proper error recovery if you need to handle errors
// gracefully.
//
// # Available Hooks
//
//   - Execution lifecycle: [BeforeExecutionHook], [AfterExecutionHook]
//   - Iteration lifecycle: [BeforeIterationHook], [AfterIterationHook]
//   - Model calls: [BeforeModelCallHook], [AfterModelCallHook]
//   - Tool calls: [BeforeToolCallHook], [AfterToolCallHook]
//   - Error handling: [ErrorHook]
// -----------------------------------------------------------------------------

// BeforeExecutionHook is implemented by hooks that want to be notified
// before execution starts.
//
// This hook is called once at the very beginning of Execute(), before
// any iterations run. Use it for:
//   - Initializing per-execution resources (timers, spans)
//   - Logging execution start with task information
//   - Setting up monitoring or tracing contexts
//
// Example:
//
//	func (h *MyHook) OnBeforeExecution(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.BeforeExecutionEvent,
//	) {
//	    h.startTime = time.Now()
//	}
type BeforeExecutionHook interface {
	// OnBeforeExecution is called once before the first iteration.
	OnBeforeExecution(
		execCtx *ExecutionContext,
		event *BeforeExecutionEvent,
	)
}

// AfterExecutionHook is implemented by hooks that want to be notified
// after execution terminates.
//
// This hook is always called if BeforeExecution was called, even when
// execution ends with an error. Use it for:
//   - Cleaning up per-execution resources
//   - Recording final metrics (duration, token usage)
//   - Closing monitoring spans
//
// Example:
//
//	func (h *MyHook) OnAfterExecution(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.AfterExecutionEvent,
//	) {
//	    duration := time.Since(h.startTime)
//	    stats := execCtx.Stats()
//	    h.metrics.RecordExecution(
//	        duration,
//	        stats.GetCounter(gent.KeyInputTokens),
//	    )
//	}
type AfterExecutionHook interface {
	// OnAfterExecution is called once after the loop terminates
	// (successfully or with error). This is always called if
	// OnBeforeExecution was called, even on error.
	OnAfterExecution(
		execCtx *ExecutionContext,
		event *AfterExecutionEvent,
	)
}

// BeforeIterationHook is implemented by hooks that want to be notified
// before each iteration.
//
// This hook supports persistent dynamic context injection. Hooks can
// modify the agent's scratchpad via execCtx.Data() to inject messages
// that persist across iterations:
//
//	func (h *ReminderHook) OnBeforeIteration(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.BeforeIterationEvent,
//	) {
//	    // Inject a reminder every 5 iterations
//	    if event.Iteration % 5 != 0 {
//	        return
//	    }
//	    data := execCtx.Data()
//	    pad := data.GetScratchPad()
//	    pad = append(pad, &gent.Iteration{
//	        Messages: []gent.MessageContent{
//	            {
//	                Role: llms.ChatMessageHuman,
//	                Parts: []gent.ContentPart{
//	                    llms.TextContent{
//	                        Text: "Remember: always validate inputs.",
//	                    },
//	                },
//	            },
//	        },
//	    })
//	    data.SetScratchPad(pad)
//	}
//
// Messages added to the scratchpad here will be included in all
// subsequent LLM calls and iterations, until removed by compaction.
// For ephemeral (single-call) injection, use [BeforeModelCallHook].
type BeforeIterationHook interface {
	// OnBeforeIteration is called before each AgentLoop.Next call.
	OnBeforeIteration(
		execCtx *ExecutionContext,
		event *BeforeIterationEvent,
	)
}

// AfterIterationHook is implemented by hooks that want to be notified
// after each iteration.
//
// This hook supports persistent dynamic context injection. Hooks can
// modify the agent's scratchpad via execCtx.Data() to inject messages
// that persist across iterations. This is useful when the injection
// depends on the iteration result:
//
//	func (h *SteeringHook) OnAfterIteration(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.AfterIterationEvent,
//	) {
//	    // After a tool-heavy iteration, remind the agent to summarize
//	    stats := execCtx.Stats()
//	    calls := stats.GetCounter(gent.KeyToolCalls)
//	    if calls > 10 {
//	        data := execCtx.Data()
//	        pad := data.GetScratchPad()
//	        pad = append(pad, &gent.Iteration{
//	            Messages: []gent.MessageContent{
//	                {
//	                    Role: llms.ChatMessageHuman,
//	                    Parts: []gent.ContentPart{
//	                        llms.TextContent{
//	                            Text: "Summarize your findings.",
//	                        },
//	                    },
//	                },
//	            },
//	        })
//	        data.SetScratchPad(pad)
//	    }
//	}
//
// Messages added to the scratchpad here will be included in all
// subsequent LLM calls and iterations, until removed by compaction.
// For ephemeral (single-call) injection, use [BeforeModelCallHook].
type AfterIterationHook interface {
	// OnAfterIteration is called after each AgentLoop.Next call.
	OnAfterIteration(
		execCtx *ExecutionContext,
		event *AfterIterationEvent,
	)
}

// ErrorHook is implemented by hooks that want to be notified of errors.
//
// If the hook panics, the panic will propagate. Hooks should implement
// proper error recovery if they need to handle errors gracefully.
type ErrorHook interface {
	// OnError is called when an error occurs during execution.
	// The error will still be returned from Execute.
	OnError(
		execCtx *ExecutionContext,
		event *ErrorEvent,
	)
}

// -----------------------------------------------------------------------------
// Model Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeModelCallHook is implemented by hooks that want to be notified
// before model calls.
//
// This hook supports ephemeral dynamic context injection. Hooks can
// modify event.Request to add, remove, or reorder messages for this
// specific LLM call only. The modified messages are sent to the model
// but are NOT persisted to the scratchpad or iteration history:
//
//	func (h *GuardrailHook) OnBeforeModelCall(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.BeforeModelCallEvent,
//	) {
//	    // Inject a reminder when tool errors are high
//	    stats := execCtx.Stats()
//	    errCount := stats.GetCounter(
//	        gent.KeyToolCallsErrorConsecutive,
//	    )
//	    if errCount < 2 {
//	        return
//	    }
//	    event.Request = append(event.Request,
//	        llms.MessageContent{
//	            Role: llms.ChatMessageHuman,
//	            Parts: []llms.ContentPart{
//	                llms.TextContent{
//	                    Text: "[SYSTEM] You have failed " +
//	                        "multiple tool calls. Re-read " +
//	                        "the error output carefully.",
//	                },
//	            },
//	        },
//	    )
//	}
//
// The injected messages only affect the current model call. They are
// not written to the scratchpad, so they will not appear in subsequent
// iterations. For persistent injection, use [BeforeIterationHook] or
// [AfterIterationHook] to modify the scratchpad via execCtx.Data().
type BeforeModelCallHook interface {
	// OnBeforeModelCall is called before each model API call.
	// Hooks can modify event.Request for ephemeral context injection.
	OnBeforeModelCall(
		execCtx *ExecutionContext,
		event *BeforeModelCallEvent,
	)
}

// AfterModelCallHook is implemented by hooks that want to be notified
// after model calls.
//
// If the hook panics, the panic will propagate. Hooks should implement
// proper error recovery if they need to handle errors gracefully.
type AfterModelCallHook interface {
	// OnAfterModelCall is called after each model API call completes.
	OnAfterModelCall(
		execCtx *ExecutionContext,
		event *AfterModelCallEvent,
	)
}

// -----------------------------------------------------------------------------
// Tool Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeToolCallHook is implemented by hooks that want to be notified
// before tool calls.
//
// Hooks can modify event.Args to change the tool input before
// execution.
type BeforeToolCallHook interface {
	// OnBeforeToolCall is called before each tool execution.
	// The hook can modify event.Args to change the input.
	OnBeforeToolCall(
		execCtx *ExecutionContext,
		event *BeforeToolCallEvent,
	)
}

// AfterToolCallHook is implemented by hooks that want to be notified
// after tool calls.
//
// If the hook panics, the panic will propagate. Hooks should implement
// proper error recovery if they need to handle errors gracefully.
type AfterToolCallHook interface {
	// OnAfterToolCall is called after each tool execution completes.
	OnAfterToolCall(
		execCtx *ExecutionContext,
		event *AfterToolCallEvent,
	)
}

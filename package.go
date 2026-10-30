// Package gent provides typed contracts for LLM agents, model calls, tools, output, and saved runs.
// Applications own models, prompts, storage, and deployment. The agents/react package supplies a
// standard loop; custom Loop[T] implementations can reuse the same execution and storage contracts.
//
// # Typed execution
//
// A Loop[T] performs one iteration and exposes Output[T]. The executor calls Next until the loop
// terminates, a limit is exceeded, the context is canceled, or an operation fails. The runner saves
// an accepted typed value and its delivery content before publishing final content or returning T.
//
//	model := models.NewOpenAI(apiKey, modelName)
//	out, err := output.NewJsonOutput[Order]()
//	if err != nil {
//	    return err
//	}
//	loop := react.NewWithOutput(model, out).RegisterTool(lookupOrder)
//	task := &gent.Task{Messages: []*gent.ModelMessage{
//	    gent.TextParts(gent.ChatMessageTypeSystem,
//	        "Help the customer find the order. " + out.Instructions()),
//	    gent.TextParts(gent.ChatMessageTypeHuman, "Where is order ORD-123?"),
//	}}
//	execCtx := gent.NewExecutionContext(ctx, "support", gent.NewBasicLoopData(task))
//	order, result, err := executor.NewRunner(loop, executor.DefaultConfig()).Execute(execCtx)
//	if err != nil {
//	    return err
//	}
//	if result.TerminationReason != gent.TerminationSuccess {
//	    return fmt.Errorf("agent ended with %s", result.TerminationReason)
//	}
//	fmt.Println(order.ID)
//
// react.NewAgent selects TextOutput and returns string. The output package also supplies
// ImageOutput, JsonOutput[T], and SchemaOutput[T]. Value and pointer result types remain distinct.
// Execute and Resume return (T, *ExecutionResult, error). Refusal is a separate terminal reason
// with ModelRefusal evidence; it does not return an accepted value or an operation error.
//
// # Prompts and content
//
// A Task contains ordered pointers to ModelMessage. The caller writes system instructions and user
// input. NewTask constructs one user message from ordered ContentPart values. TextContent,
// BinaryContent, and supported URL parts can be concrete values or pointers inside that slice.
// The model adapter maps roles and supported content to native fields. Unsupported input produces
// an error; Gent does not convert a file into another input type.
//
// ExecutionContext freezes the initial task. Later request hooks and caller edits cannot change
// that saved task. Compaction changes selected working history, never the original task. Put prior
// conversation in working history when it should be eligible for compaction.
//
// Output.Instructions is optional guidance for caller composition. Configure requests native output
// controls without changing messages. The agent does not insert system instructions, output tags,
// or prompt templates. Use TimeProviderFunc, DefaultTimeProvider, or MockTimeProvider in caller
// code
// when a date-dependent task needs a custom clock or deterministic test data.
//
// # Output validation
//
// Output.Process receives complete ordered candidate content after native completion and tool
// checks.
// Local validation always runs, including when native schema enforcement was used. Built-in outputs
// can then call an OutputCustomValidator[T] for application rules. Validators inspect the value;
// transformations belong in a custom output processor with its own validation contract.
//
// OutputResult[T] exposes Accepted, Value, Content, and Steering. Accepted results have matching
// value and delivery content and no steering. Expected rejection supplies useful steering with a
// nil error. Operation errors stop execution. There is no answer-section detection or separate
// Format method. A custom Loop[T] uses AcceptOutput to prepare its validated terminal result.
//
// The loop retains a rejected assistant candidate and native continuation, then appends steering.
// That conversation group stays selected until the next main model request, including through
// custom compaction. Further attempts use normal run limits. Incomplete native responses retain
// evidence and retry, but never execute partial tool calls or enter the output processor. Native
// refusal ends the run without correction.
//
// # Tools and tool chains
//
// Tools have typed input and output. DefineTool builds a tool from a function or bound method:
//
//	tool := gent.DefineTool("search", searchFunc).
//	    Description("Search for information.").
//	    Policy("Use the query supplied by the user.").MustBuild()
//
// ToolChain exposes native definitions, prepares a saved call plan, validates arguments, executes
// steps, and supplies recovery decisions. Registry, SearchChain, and JsToolChainWrapper use the
// same hooks and persistence contract. Tool outputs, media, policy, summaries, and recovery
// guidance
// remain structured. TextFormat only renders tool-result or summary sections when a chain needs
// text; it does not parse model output or build the agent's starting prompt.
//
// Tool schemas use the shared schema package. Parameter fields are required unless explicitly
// optional. Output inference also honors omitempty and omitzero. Native schemas do not replace
// local validation. See Tool, ToolChain, and the toolchain package for full contracts.
//
// # Events and observers
//
// Mutable subscribers can intercept lifecycle, model, tool, and output-validation events. Use
// BeforeModelCallSubscriber for request-owned dynamic context. Required output controls are checked
// after hooks so a hook cannot silently remove the output contract.
//
// RecordedEventObserver sees accepted events after storage accepts them. This is the appropriate
// boundary for durable trace or WebSocket delivery. Raw provider callbacks run before that barrier.
// ExecutionContext.Events supplies owned local event snapshots for inspection.
//
// SubscribeAll streams provisional MetaContent and accepted Content as ordered content parts.
// A provider stream can supply only provisional metadata and a response candidate. Final Content
// is a complete snapshot after terminal saving. Replace the answer rather than append it; source
// message IDs identify superseded previews. Child contexts retain separate final answers.
// Deduplicate replay by RunId and EventNumber. StreamAccumulator keeps these channels separate.
//
// # Statistics and limits
//
// ExecutionStats counters propagate from children; gauges are local. Use StatKey.Self for a
// per-context limit. An update that exceeds a limit records the event and cancels the context.
//
//	execCtx.SetLimits([]gent.Limit{
//	    {Type: gent.LimitExactKey, Key: gent.SCIterations.Self(), MaxValue: 20},
//	    {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 100000},
//	    {Type: gent.LimitKeyPrefix, Key: gent.SCToolCallsFor, MaxValue: 20},
//	})
//
// SCOutputRejectedTotal counts rejected candidates. Separate local and custom counters describe
// the validation stage. SCOutputCustomRejectedBy supports limits for named validators. Rejections
// consume ordinary iterations and tokens; there is no separate output retry counter.
//
// # History and recovery
//
// ExecutionContext owns append-only evidence and an ordered active selection of HistoryEntry IDs.
// AppendRecord and UpdateHistory keep observations and selection changes consistent. A compaction
// strategy can retain all image-bearing entries or only the latest without rewriting originals.
// Saved provider continuation retains native call IDs and opaque signed or encrypted fields.
//
// A RunRecorder accepts events and checkpoints atomically. Use CodecRegistry.Marshal and Unmarshal
// for saved runs. The compact byte codec retains owned media, named types, and nil versus empty
// slices. Register custom ValueCodec implementations before execution when a type needs them.
//
// Runner.Resume continues from the accepted checkpoint. A terminal restore returns the saved typed
// result without another model call, image download, output process, or validator callback. The
// output type, codec version, and configuration must match. Credentials are supplied again.
//
// Custom loops identify executable behavior with a recovery version and implement CheckpointState
// for mutable state. Custom LoopData, compaction components, and tool recovery use the same
// explicit
// contract. A saved tool-ready record does not prove an external effect completed. Tool.Recover
// resolves an uncertain effect; an already saved result takes precedence over a recovery callback.
//
// See docs/output.md, docs/models.md, and docs/history.md for examples and storage requirements.
package gent

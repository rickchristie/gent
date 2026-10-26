export const components = [
  {
    id: 'executor',
    name: 'Runner',
    kind: 'Implementation',
    layer: 'Execution',
    label: 'Keeps the loop moving',
    source: 'executor/executor.go',
    symbol: 'Runner',
    job: `Starts execution, runs iterations, checks cancellation and limits, and saves safe
      boundaries.`,
    why: 'The loop chooses the next step. The executor runs its lifecycle consistently.',
    implementations: `executor.NewRunner; the old Executor[Data] alias and New[Data] constructor
      still work.`,
    links: ['agent', 'context', 'checkpoint', 'limits'],
    code: `func (e *Runner) Execute(
    ctx *gent.ExecutionContext,
) (*gent.ExecutionResult, error)
func (e *Runner) Continue(ctx *gent.ExecutionContext) (*gent.ExecutionResult, error)
func (e *Runner) Resume(
    worker context.Context, saved gent.RunView, bindings gent.RestoreBindings,
) (*gent.ExecutionResult, error)`,
  },
  {
    id: 'agent',
    name: 'Loop',
    kind: 'Interface',
    layer: 'Execution',
    label: 'Chooses the next step',
    source: 'agent.go',
    symbol: 'Loop',
    job: `Builds a prompt, calls a model, interprets the response, uses tools, and decides to
      continue or return an answer.`,
    why: 'Gent supports custom loops. ReAct is one implementation, with a think–act–observe cycle.',
    implementations: 'agents/react.Agent; your own Next method can implement another pattern.',
    links: ['executor', 'context', 'model', 'tools', 'termination'],
  },
  {
    id: 'context',
    name: 'ExecutionContext',
    kind: 'Struct',
    layer: 'Execution',
    label: 'Holds this run together',
    source: 'context.go',
    symbol: 'ExecutionContext',
    job: `Connects the task, working context, history, journal, stats, limits, cancellation, and
      subscriptions for an execution.`,
    why: `The history refactor puts controlled state changes here so history and checkpoints agree.
      Its managed model call captures evidence before execution advances.`,
    implementations: 'NewExecutionContext; children share the run journal and propagate counters.',
    links: ['loopdata', 'history', 'journal', 'model', 'stats', 'checkpoint'],
    code: `// Selected ExecutionContext method signatures.
Task() (*Task, error)
Scratchpad() ([]*HistoryEntry, error)
AppendRecord(record *HistoryEntry, active bool) error
UpdateHistory(update HistoryUpdate) error
FlushProgress() error
CommitCheckpoint(phase ExecutionPhase) error
GenerateContentStream(model Model, call ModelCall, request ModelCallRequest) (Stream, error)`,
  },
  {
    id: 'loopdata',
    name: 'LoopData',
    kind: 'Interface',
    layer: 'Execution',
    label: 'Supplies the task and custom data',
    source: 'agent.go',
    symbol: 'LoopData',
    job: `Supplies the initial task. Custom implementations can carry application fields accessed
      through ExecutionContext.Data().`,
    why: `History and scratchpad mutations moved to ExecutionContext. Two independently mutable
      owners could disagree about what a checkpoint represents.`,
    implementations:
      'BasicLoopData; embed it and implement CheckpointState for resumable custom state.',
    links: ['task', 'context', 'extension'],
  },
  {
    id: 'task',
    name: 'Task',
    kind: 'Struct',
    layer: 'Execution',
    label: 'The original request',
    source: 'agent.go',
    symbol: 'Task',
    job: 'Carries input text and media. ExecutionContext freezes a typed copy at creation.',
    why: 'The original input remains stable even if the caller later edits its own data.',
    implementations: 'Read the owned snapshot with ctx.Task().',
    links: ['loopdata', 'record', 'codecs'],
  },
  {
    id: 'model',
    name: 'Model',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Talks to an LLM provider',
    source: 'model.go',
    symbol: 'Model',
    job: 'Accepts a standard Go context and a resolved request, then returns a stream.',
    why: `Provider-specific networking stays in the adapter. The managed ExecutionContext call owns
      Gent events, correlation IDs, accounting, persistence ordering, and saved-response reuse.`,
    implementations:
      'models.LCGWrapper adapts LangChainGo models; custom providers implement Model.',
    links: ['context', 'request', 'stream'],
  },
  {
    id: 'request',
    name: 'ModelCallRequest',
    kind: 'Struct',
    layer: 'Model & tools',
    label: 'Exactly what the provider sees',
    source: 'model.go',
    symbol: 'ModelCallRequest',
    job: 'Carries messages, resolved options, and the history selection that produced the request.',
    why: `Mutable before-hooks finish before the request is cloned and accepted. Recording an
      earlier prompt would leave an inaccurate explanation of the model response.`,
    implementations: 'NewModelCallRequest evaluates option functions once.',
    links: ['model', 'record', 'events', 'journal'],
  },
  {
    id: 'stream',
    name: 'Stream',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Delivers previews and provider completion',
    source: 'model.go',
    symbol: 'Stream',
    job: `Exposes Chunks, a complete Response, and Close. Ordered MetaContent carries intermediary,
      commentary, reasoning, or new string types. Response contains candidate text.`,
    why: `Close requests cancellation. Response waits for producer completion, so the last chunk and
      known usage cannot arrive after a supposedly completed step. The executor publishes one full
      Content event after validation and terminal save; source IDs link it to the preview.`,
    implementations: 'NewBoundedStreamWithDuration supports backpressure; producers must Complete.',
    links: ['model', 'context', 'trace'],
  },
  {
    id: 'format',
    name: 'TextFormat',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Finds sections in model output',
    source: 'format.go',
    symbol: 'TextFormat',
    job: `Understands the outer envelope, such as XML tags or Markdown headings, and extracts
      sections.`,
    why: 'The outer format and the content inside each section are separate choices.',
    implementations: 'format.XML and format.Markdown; DescribeStructure provides prompt guidance.',
    links: ['section', 'tools', 'termination', 'agent'],
  },
  {
    id: 'section',
    name: 'TextSection',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Understands a section’s contents',
    source: 'section.go',
    symbol: 'TextSection',
    job: 'Names a section, describes its syntax, and parses its content into a useful value.',
    why: 'A section can contain text, JSON, or YAML regardless of its outer envelope.',
    implementations: 'section.Text, section.JSON[T], section.YAML[T]; Termination embeds it.',
    links: ['format', 'tools', 'termination'],
  },
  {
    id: 'tools',
    name: 'ToolChain',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Turns proposed actions into tool calls',
    source: 'toolchain.go',
    symbol: 'ToolChain',
    job: `Publishes native definitions, prepares saved calls, checks schemas, transforms typed arguments,
      executes tools, and formats observations.`,
    why: `The model proposes a native call. The chain provides the checked route to a Go
      tool.`,
    implementations:
      'toolchain.Registry, SearchChain; JsToolChainWrapper adds JavaScript execution.',
    links: ['section', 'tool', 'toolplan', 'searchtools', 'toolresult'],
  },
  {
    id: 'tool',
    name: 'Tool',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Performs one concrete operation',
    source: 'tool.go',
    symbol: 'Tool',
    job: 'Defines a name, description, schema, typed Call, and an optional deduplication summary.',
    why: `Application operations stay ordinary Go code. ToolIdentityFromContext supplies stable call
      identity when an external service supports deduplication or reconciliation.`,
    implementations: 'ToolFunc wraps a function; a tool might look up a policy or submit a refund.',
    links: ['tools', 'toolresult', 'toolrecovery'],
  },
  {
    id: 'toolresult',
    name: 'ToolCallResult',
    kind: 'Struct',
    layer: 'Model & tools',
    label: 'Preserves the actual observation',
    source: 'toolchain.go',
    symbol: 'ToolCallResult',
    job: `Retains logical/attempt IDs, raw and typed input, typed output, errors, formatted text,
      media, instructions, and the captured abbreviation.`,
    why: `A later policy edit or changed tool summary must not rewrite the evidence behind an
      answer.`,
    implementations:
      'ToolChainResult groups results; native history pairs them with provider call IDs.',
    links: ['tool', 'record', 'toolplan', 'history'],
  },
  {
    id: 'toolplan',
    name: 'ToolPlan',
    kind: 'Struct',
    layer: 'Recovery',
    label: 'Remembers which tool comes next',
    source: 'tool_plan.go',
    symbol: 'ToolPlan',
    job: `Stores ordered calls, distinct logical IDs, the next-call cursor, a prepared call, and
      results.`,
    why: `Equal arguments in two intentional list positions are still two separate calls. Arguments
      alone cannot identify work during recovery.`,
    implementations:
      'Saved with SetToolPlan and read with SavedToolPlan; SearchChain also saves seen tools.',
    links: ['tools', 'toolrecovery', 'checkpoint'],
  },
  {
    id: 'termination',
    name: 'Termination',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Decides whether an answer is acceptable',
    source: 'termination.go',
    symbol: 'Termination',
    job: 'Parses the answer section and returns continue, rejected with feedback, or accepted.',
    why: `An answer-shaped string is not automatically a valid result. Acceptance is an explicit
      step.`,
    implementations:
      'termination.Text and termination.JSON[T], optionally using an AnswerValidator.',
    links: ['section', 'validator', 'agent', 'checkpoint'],
  },
  {
    id: 'validator',
    name: 'AnswerValidator',
    kind: 'Interface',
    layer: 'Model & tools',
    label: 'Applies application answer rules',
    source: 'termination.go',
    symbol: 'AnswerValidator',
    job: 'Checks a parsed answer and returns acceptance or feedback for another iteration.',
    why: 'Application criteria, such as required citations, belong outside the generic agent loop.',
    implementations: 'Implement Name and Validate; rejected answers update rejection statistics.',
    links: ['termination', 'stats', 'events'],
  },
  {
    id: 'record',
    name: 'HistoryEntry',
    kind: 'Struct',
    layer: 'History',
    label: 'A container for messages',
    source: 'agent.go',
    symbol: 'HistoryEntry',
    job: `Groups messages with origin, metadata, and optional expiry. It replaces the old content
      type named Iteration.`,
    why: `One execution iteration can produce several records. A content record is not a loop
      counter.`,
    implementations: 'Used for observations, feedback, injected instructions, and summaries.',
    links: ['message', 'entry', 'history', 'context'],
  },
  {
    id: 'message',
    name: 'MessageContent',
    kind: 'Struct',
    layer: 'History',
    label: 'A role, content parts, and metadata',
    source: 'agent.go',
    symbol: 'MessageContent',
    job: 'Carries text or media for a conversation role, plus typed metadata such as tool results.',
    why: `Conversation text alone cannot retain the typed evidence required for exact
      reconstruction.`,
    implementations:
      'ContentPart includes text and media implementations; codecs preserve dynamic types.',
    links: ['record', 'codecs', 'toolresult'],
  },
  {
    id: 'entry',
    name: 'EntryId',
    kind: 'Named type',
    layer: 'History',
    label: 'Identifies one piece of evidence',
    source: 'history.go',
    symbol: 'EntryId',
    job: 'Identifies a history entry in selections, replacements, and compaction records.',
    why: 'A replacement gets a new ID. Earlier evidence remains available under its original ID.',
    implementations: 'NewHistoryEntry assigns an ID. ContextDraft.Active keeps the prompt order.',
    links: ['record', 'history', 'change', 'compaction'],
  },
  {
    id: 'history',
    name: 'HistorySnapshot',
    kind: 'Struct',
    layer: 'History',
    label: 'All evidence and context transitions',
    source: 'history.go',
    symbol: 'HistorySnapshot',
    job: `Retains original records, every summary, every compaction decision, and selection
      changes.`,
    why: `A summary can omit a fact. Keeping the original makes that loss inspectable instead of
      permanent.`,
    implementations: 'ContextAt(contextId, revision) reconstructs the ordered active entry IDs.',
    links: ['entry', 'change', 'context', 'bundle'],
  },
  {
    id: 'change',
    name: 'HistoryChange',
    kind: 'Struct',
    layer: 'History',
    label: 'An explicit state transition',
    source: 'history.go',
    symbol: 'HistoryChange',
    job: `Records previous/new revisions, appended entries, selections, custom state replacements,
      and a reason.`,
    why: 'The old text Diff is removed. Structured changes can be validated and replayed exactly.',
    implementations:
      'UpdateHistory queues a change; accepted StateChangeEvent carries it to observers.',
    links: ['history', 'stateevent', 'journal'],
  },
  {
    id: 'compaction',
    name: 'CompactionStrategy',
    kind: 'Interface',
    layer: 'History',
    label: 'Proposes a smaller working context',
    source: 'compaction.go',
    symbol: 'CompactionStrategy',
    job: `Receives the active records and proposes appended entries plus a complete resulting
      selection.`,
    why: `A strategy proposes; the executor accepts the decision, history, baseline, and checkpoint
      together. Original evidence is preserved.`,
    implementations: `compaction.SlidingWindowStrategy and SummarizationStrategy;
      summaries can consume earlier summaries.`,
    links: ['trigger', 'history', 'checkpoint', 'model'],
  },
  {
    id: 'trigger',
    name: 'CompactionTrigger',
    kind: 'Interface',
    layer: 'History',
    label: 'Decides when context needs shrinking',
    source: 'compaction.go',
    symbol: 'CompactionTrigger',
    job: `Checks whether compaction should run and records the baseline after a successful
      decision.`,
    why: `Restoring its per-run baseline prevents repeating the same completed compaction after
      takeover.`,
    implementations:
      'compaction.StatThresholdTrigger; custom triggers can implement CheckpointState.',
    links: ['compaction', 'stats', 'extension'],
  },
  {
    id: 'journal',
    name: 'Run recording',
    kind: 'Context methods',
    layer: 'Persistence',
    label: 'Accepts and exports the run’s record',
    source: 'context.go',
    symbol: 'ExecutionContext',
    job: `Accepts queued activity, accounting, and optional checkpoints. Export returns the complete
      accepted RunView.`,
    why: 'One owner keeps event numbers, history, and checkpoints consistent.',
    implementations: 'ExecutionContext owns acceptance. WithRecorder binds application storage.',
    links: ['journalimpl', 'recorder', 'batch', 'bundle', 'trace'],
    code: `// ExecutionContext methods.
FlushProgress() error
CommitCheckpoint(phase ExecutionPhase) error
Export() (RunView, error)`,
  },
  {
    id: 'journalimpl',
    name: 'RecordedEventObserver',
    kind: 'Interface',
    layer: 'Persistence',
    label: 'Receives accepted evidence',
    source: 'context_recording.go',
    symbol: 'RecordedEventObserver',
    job: 'Receives owned events after storage accepts them, with the registry that decodes them.',
    why: 'A display must not announce a state change before the save succeeds.',
    implementations: 'trace.Sequencer; bind it with WithObserver.',
    links: ['journal', 'recorder', 'batch', 'trace'],
  },
  {
    id: 'recorder',
    name: 'Recorder',
    kind: 'Interface',
    layer: 'Persistence',
    label: 'Connects Gent to application storage',
    source: 'run.go',
    symbol: 'Recorder',
    job: `Commits a SaveBatch to shared storage and returns success only after the transaction
      commits.`,
    why: `The application supplies its database, lease checks, stale-writer fencing, and idempotent
      batch acknowledgment. Gent does not choose those systems.`,
    implementations:
      'Implement Save. A nil recorder means in-memory acceptance, not crash durability.',
    links: ['journal', 'batch', 'receipt'],
  },
  {
    id: 'batch',
    name: 'SaveBatch',
    kind: 'Struct',
    layer: 'Persistence',
    label: 'One atomic storage unit',
    source: 'run.go',
    symbol: 'SaveBatch',
    job: `Contains a stable digest ID, previous cursor, numbered events, accounting, and optionally
      the matching checkpoint.`,
    why: `A progress-only batch can advance observation without advancing the control checkpoint.
      A timeout may mean acknowledgment was lost after a real commit.`,
    implementations:
      'ApplyBatch is the pure reducer; application transactions still provide atomicity.',
    links: ['recorder', 'journal', 'change', 'checkpoint'],
  },
  {
    id: 'receipt',
    name: 'PersistenceError',
    kind: 'Struct',
    layer: 'Persistence',
    label: 'Keeps an uncertain save available',
    source: 'run.go',
    symbol: 'PersistenceError',
    job: 'Retains the exact failed batch and the error returned by storage.',
    why: 'An acknowledgment can be lost after a successful write. New work must stop.',
    implementations: 'Reconcile the saved batch with storage, then restore a fresh context.',
    links: ['journal', 'recorder', 'batch', 'bindings'],
  },
  {
    id: 'bundle',
    name: 'RunView',
    kind: 'Struct',
    layer: 'Persistence',
    label: 'The complete accepted run',
    source: 'run.go',
    symbol: 'RunView',
    job: `Contains full history and activity, the latest checkpoint, absolute runtime accounting,
      allocation high-water marks, and lastAppliedEventNumber.`,
    why: `The last checkpoint may be older than saved progress. A viewer and a replacement worker
      need that later evidence too.`,
    implementations: 'ExecutionContext.Export returns an owned copy of accepted evidence.',
    links: ['history', 'journal', 'checkpoint', 'trace'],
  },
  {
    id: 'codecs',
    name: 'CodecRegistry',
    kind: 'Struct',
    layer: 'Persistence',
    label: 'Keeps typed data portable',
    source: 'history_codec.go',
    symbol: 'CodecRegistry',
    job: `Encodes and decodes explicitly identified, versioned values without losing dynamic types,
      large integers, media bytes, metadata, or cache durations.`,
    why: `Plain JSON into any loses important Go type information. Unknown types, cycles, or
      unsupported versions fail instead of silently degrading a recoverable run.`,
    implementations:
      'RegisterType for exported values; ValueCodec for private/custom representations.',
    links: ['valuecodec', 'message', 'extension', 'bundle'],
    code: `codecs := gent.NewCodecRegistry()
if err := codecs.RegisterType("app/policy", 1, Policy{}); err != nil {
    return err
}
encoded, err := codecs.Encode(value)
if err != nil {
    return err
}
restored, err := codecs.Decode(encoded)
if err != nil {
    return err
}
// Use the owned, typed restored value.`,
  },
  {
    id: 'valuecodec',
    name: 'ValueCodec',
    kind: 'Interface',
    layer: 'Persistence',
    label: 'Defines a custom wire representation',
    source: 'history_codec.go',
    symbol: 'ValueCodec',
    job: 'Supplies a stable type/version and custom Encode/Decode methods for application values.',
    why: `Private fields and custom content need an explicit contract. Decoders must return
      owned values.`,
    implementations: 'Register a sample and codec with CodecRegistry.Register.',
    links: ['codecs', 'extension'],
  },
  {
    id: 'checkpoint',
    name: 'Checkpoint',
    kind: 'Struct',
    layer: 'Recovery',
    label: 'A safe place to continue execution',
    source: 'checkpoint.go',
    symbol: 'Checkpoint',
    job: `Saves a control phase, matching history revision/cursor, context state, compatibility, and
      a terminal outcome when finished.`,
    why: `An arbitrary saved event is not necessarily a resumable program position. Gent records
      explicit boundaries rather than attempting to serialize a Go stack.`,
    implementations: `Initial, iteration-ready, decision-ready, tool-ready/completed,
      iteration-completed, compaction-completed, and terminal phases.`,
    links: ['bindings', 'extension', 'bundle', 'toolplan'],
  },
  {
    id: 'bindings',
    name: 'RestoreBindings',
    kind: 'Struct',
    layer: 'Recovery',
    label: 'Supplies the replacement worker’s runtime',
    source: 'checkpoint.go',
    symbol: 'RestoreBindings',
    job: `Rebinds storage, observers, codecs, custom data and components, configuration, and tool
      recovery.`,
    why: `Credentials, provider clients, functions, and executable behavior are recreated on the new
      worker. A bundle stores portable state, not live objects.`,
    implementations: 'RestoreExecutionContext validates the bundle; Runner.Continue resumes it.',
    links: ['checkpoint', 'extension', 'toolrecovery', 'codecs'],
  },
  {
    id: 'extension',
    name: 'CheckpointState',
    kind: 'Interface',
    layer: 'Recovery',
    label: 'Makes custom mutable state resumable',
    source: 'checkpoint.go',
    symbol: 'CheckpointState',
    job: 'Defines explicit type/version/save/restore operations for custom state.',
    why: `A recovery version identifies behavior; it cannot recreate hidden mutable fields. Save and
      restore methods copy local state without publishing events or performing external work.`,
    implementations:
      'BindCheckpointState and RestoreBindings.Components; custom LoopData can implement it.',
    links: ['loopdata', 'bindings', 'codecs'],
  },
  {
    id: 'toolrecovery',
    name: 'ToolRecovery',
    kind: 'Interface',
    layer: 'Recovery',
    label: 'Resolves uncertain external work',
    source: 'checkpoint.go',
    symbol: 'ToolRecovery',
    job: `Returns completed with a full result, retry allowed, or unresolved for a prepared tool
      call whose outcome was not durably recorded.`,
    why: `A worker may die after a write but before saving its result. The default is
      ErrUncertainTool; Gent cannot promise exactly-once external side effects.`,
    implementations:
      'ToolRecoveryFunc; tools can pass their logical ID to external idempotency systems.',
    links: ['toolplan', 'toolresult', 'bindings'],
  },
  {
    id: 'stats',
    name: 'ExecutionStats',
    kind: 'Struct',
    layer: 'Guardrails',
    label: 'Counts work and measures current state',
    source: 'stats.go',
    symbol: 'ExecutionStats',
    job: `Tracks monotonic counters and local gauges. Counters propagate from child to parent;
      $self: counters exclude children. Gauges do not propagate.`,
    why: `Recovery imports accepted accounting directly. Replaying history must not charge token
      usage again, and restarting a worker must not reset a run’s budget.`,
    implementations:
      'IncrCounter; IncrGauge, SetGauge, ResetGauge; stats_keys.go defines Gent keys.',
    links: ['limits', 'context', 'checkpoint'],
    code: `ctx.Stats().GetCounter(gent.SCInputTokens)        // this context + descendants
ctx.Stats().GetCounter(gent.SCInputTokens.Self()) // this context only
ctx.Stats().GetGauge(gent.SGToolCallsErrorConsecutive) // local current streak`,
  },
  {
    id: 'limits',
    name: 'Limit',
    kind: 'Struct',
    layer: 'Guardrails',
    label: 'Stops work when a budget is exceeded',
    source: 'limit.go',
    symbol: 'Limit',
    job: `Matches an exact stat key or a prefix. Most limits cancel when a stat becomes greater than
      MaxValue. Exact iteration limits stop after a completed iteration at greater-than-or-equal.`,
    why: `Limits are checked during updates, not only between requests. Parent cancellation reaches
      children; an absolute deadline still includes time spent without a worker.`,
    implementations: 'DefaultLimits; SetLimits; StatKey.Self for per-context budgets.',
    links: ['stats', 'executor', 'context'],
  },
  {
    id: 'events',
    name: 'Events & Registry',
    kind: 'Contracts & implementation',
    layer: 'Observation',
    label: 'Hooks and lifecycle observation',
    source: 'events/registry.go',
    symbol: 'Registry',
    job: `Dispatches typed lifecycle events to subscribers. Model, tool, and system-prompt
      before-hooks may edit effective inputs; other lifecycle observers receive copies.`,
    why: `Execution hooks and accepted journal observation have different timing. Subscribe a
      hook to the executor registry; bind accepted observation using WithObserver.`,
    implementations: 'events.Registry and subscriber interfaces in subscribers.go.',
    links: ['request', 'stateevent', 'trace', 'stats'],
    code: `exec.Subscribe(subscriber)       // execution hooks and lifecycle callbacks
// Pass gent.WithObserver(seq) to NewExecutionContext for accepted display events.
if err := ctx.FlushProgress(); err != nil { // acceptance barrier for custom code
    return err
}`,
  },
  {
    id: 'stateevent',
    name: 'StateChangeEvent',
    kind: 'Struct',
    layer: 'Observation',
    label: 'Announces an accepted history change',
    source: 'events.go',
    symbol: 'StateChangeEvent',
    job: 'Pairs numbered event metadata with a structured HistoryChange.',
    why: `It replaces CommonDiffEvent. There is no text Diff field or diff computation.
      Callbacks run after acknowledgment and outside commit locks.`,
    implementations: 'StateChangeEventSubscriber.OnStateChangeEvent.',
    links: ['change', 'journal', 'events', 'trace'],
  },
  {
    id: 'trace',
    name: 'trace.Sequencer',
    kind: 'Implementation',
    layer: 'Observation',
    label: 'Builds a display from accepted evidence',
    source: 'trace/sequencer.go',
    symbol: 'Sequencer',
    job: `Projects accepted event numbers into snapshots, recent events,
      and live subscriptions.`,
    why: `Redaction, retention, and slow viewers affect the display, while the canonical journal
      retains lossless evidence. Display delivery is not durable storage.`,
    implementations: 'NewSequencer / RestoreSequencer; Snapshot, EventsAfter, Subscribe.',
    links: ['journal', 'bundle', 'recordedevent', 'stateevent'],
    code: `seq, err := trace.NewSequencer(runId, trace.Config{})
if err != nil {
    return err
}
// Pass gent.WithObserver(seq) when constructing the ExecutionContext.
snapshot := seq.Snapshot()
events, unsubscribe := seq.Subscribe()
defer unsubscribe()
bundle, err := ctx.Export()
if err != nil {
    return err
}
// Display snapshot, consume events, or inspect the complete bundle.`,
  },
  {
    id: 'recordedevent',
    name: 'RecordedEvent',
    kind: 'Struct',
    layer: 'Observation',
    label: 'The canonical numbered wire event',
    source: 'run.go',
    symbol: 'RecordedEvent',
    job: `Identifies the run, context, iteration, attempt, related model/tool call, and typed
      payload or structured history change.`,
    why: `trace.Event is a display projection with the same number, not an interchangeable recovery
      record. The run schema is version 5; trace views use the run schema version.`,
    implementations:
      'Events are retained in RunView.Activity, including progress after the checkpoint.',
    links: ['trace', 'bundle', 'codecs'],
  },
  {
    id: 'searchtools',
    name: 'ToolSearcher / IndexableTool',
    kind: 'Interfaces',
    layer: 'Model & tools',
    label: 'Discovers tools on demand',
    source: 'tool_search.go',
    symbol: 'ToolSearcher',
    job: `Indexes tool metadata and searches for relevant tool names. SearchChain exposes search and
      optionally a separate schema lookup to the model.`,
    why: `Large tool catalogs can be discovered in pages. Recovery preserves completed results and
      which tool definitions were already shown within a plan.`,
    implementations:
      'BM25, regex, and index searchers; IndexableTool supplies domain/category metadata.',
    links: ['tools', 'searchindex', 'toolplan'],
  },
  {
    id: 'searchindex',
    name: 'SearchIndex',
    kind: 'Interface',
    layer: 'Supporting libraries',
    label: 'Retrieves relevant documents',
    source: 'search/search.go',
    symbol: 'SearchIndex',
    job: 'Indexes documents and returns scored matches through a generic search contract.',
    why: `Search is useful independently of an agent loop. A tool can use it and then save the
      actual returned text as evidence in the run.`,
    implementations: `FlatIndex with embeddings, BleveIndex for lexical search, FusedIndex for
      combined results; ChunkAdapter and BleveAdapter map application documents.`,
    links: ['embedder', 'policy', 'tool'],
  },
  {
    id: 'embedder',
    name: 'Embedder',
    kind: 'Interface',
    layer: 'Supporting libraries',
    label: 'Turns text into vectors for retrieval',
    source: 'search/embedder.go',
    symbol: 'Embedder',
    job: `Produces embeddings used by semantic search; document adapters and chunking decide what
      text should be embedded.`,
    why: 'Retrieval models and the agent’s text-generating Model perform different jobs.',
    implementations: 'The search package includes an ONNX implementation and model configuration.',
    links: ['searchindex', 'policy', 'model'],
  },
  {
    id: 'policy',
    name: 'Policy tools',
    kind: 'Implementations',
    layer: 'Supporting libraries',
    label: 'A practical retrieval building block',
    source: 'policy/search.go',
    symbol: 'PolicySearchTool',
    job: `Searches a policy library and retrieves a selected policy through tools usable by an
      agent.`,
    why: `A policy ID alone is insufficient historical evidence if the policy changes. Save the text
      returned by the tool with its identifiers.`,
    implementations:
      'policy.PolicySearchTool and GetPolicyTool; Policy supplies the document model.',
    links: ['searchindex', 'tool', 'toolresult'],
  },
  {
    id: 'clock',
    name: 'TimeProvider',
    kind: 'Interface',
    layer: 'Guardrails',
    label: 'Makes time-dependent prompts controllable',
    source: 'time_provider.go',
    symbol: 'TimeProvider',
    job: `Supplies the time used while building prompts, allowing deterministic tests and explicit
      application configuration.`,
    why: `A captured request keeps its original time-dependent text. A compatible fresh request can
      use the replacement worker’s time provider.`,
    implementations: 'WithClock accepts a function. WithTimeProvider also supports custom formats.',
    links: ['agent', 'request', 'bindings'],
  },
];

export const steps = [
  {
    title: 'Start with a request.',
    active: 'executor',
    phase: 'initial',
    iteration: 0,
    text: `The application supplies a task and fresh model/tool bindings. The executor runs its
      before-execution hooks, then accepts an initial checkpoint.`,
    note: 'The task is frozen. This generation gets its own run ID.',
    entries: [],
    activity: 'Execution started → initial checkpoint',
    tokens: 0,
    tools: 0,
  },
  {
    title: 'Open iteration one.',
    active: 'agent',
    phase: 'iteration_ready',
    iteration: 1,
    text: `ReAct builds the next request from the task, system instructions, and selected context.
      The executor saves an iteration-ready boundary.`,
    note: 'An iteration is one pass through the loop, not one message record.',
    entries: [],
    activity: 'BeforeIteration → iteration-ready checkpoint',
    tokens: 0,
    tools: 0,
  },
  {
    title: 'Record the effective request.',
    active: 'context',
    phase: 'iteration_ready',
    iteration: 1,
    text: `The managed model call runs mutable before-hooks, freezes the resulting messages and
      options, and accepts the request before asking the provider.`,
    note: 'If this save fails, the provider call does not start.',
    entries: [],
    activity: 'Model request accepted · logical call m1',
    tokens: 0,
    tools: 0,
  },
  {
    title: 'Ask the model.',
    active: 'model',
    phase: 'iteration_ready',
    iteration: 1,
    text: `The provider streams a response proposing a policy lookup. Gent records emitted chunks,
      then the final response and its known usage.`,
    note: 'Saved progress can advance while the control checkpoint stays where it was.',
    entries: [],
    activity: 'Model chunks → model response accepted',
    tokens: 24,
    tools: 0,
  },
  {
    title: 'Read the native tool call.',
    active: 'tools',
    phase: 'decision_ready',
    iteration: 1,
    text: `ReAct reads the structured tool calls and saves its decision state so a replacement
      worker can use this completed response without asking the model again.`,
    note: 'The toolchain decodes JSON arguments and creates an ordered tool plan.',
    entries: ['Model: look up policy P10'],
    activity: 'Decision and tool plan saved',
    tokens: 24,
    tools: 0,
  },
  {
    title: 'Prepare the tool call.',
    active: 'tools',
    phase: 'tool_ready',
    iteration: 1,
    text: `The chain validates arguments. Before-tool hooks finish, then the logical call ID and
      effective typed input are saved before Tool.Call begins.`,
    note: 'A crash after this boundary can leave an uncertain external outcome.',
    entries: ['Model: look up policy P10'],
    activity: 'Tool-ready checkpoint · logical call t1',
    tokens: 24,
    tools: 1,
  },
  {
    title: 'Keep the actual evidence.',
    active: 'tools',
    phase: 'tool_completed',
    iteration: 1,
    text: `The policy tool returns “Refunds are allowed within 14 days.” Its full result,
      identifiers, metadata, and formatted observation are saved together with the completed
      tool step.`,
    note: 'A later policy edit cannot change what this run observed.',
    entries: ['Model: look up policy P10', 'Tool: P10 · refunds within 14 days'],
    activity: 'Tool result accepted → tool-completed checkpoint',
    tokens: 24,
    tools: 1,
  },
  {
    title: 'Make the observation available.',
    active: 'history',
    phase: 'iteration_completed',
    iteration: 1,
    text: `ReAct adds the observation to history and the active context. It returns LAContinue.
      The executor accepts the completed iteration.`,
    note: `The next model request includes this observation. Optional compaction happens between
      iterations before another request.`,
    entries: ['Model: look up policy P10', 'Tool: P10 · refunds within 14 days'],
    activity: 'History change → iteration-completed checkpoint',
    tokens: 24,
    tools: 1,
  },
  {
    title: 'Build the next request.',
    active: 'model',
    phase: 'iteration_ready',
    iteration: 2,
    text: `The executor opens iteration two. ReAct uses the original task and selected evidence.
      The managed boundary records this new request before the next provider call.`,
    note: 'The model can now answer using the returned policy text.',
    entries: ['Model: look up policy P10', 'Tool: P10 · refunds within 14 days'],
    activity: 'Iteration two → new model request accepted',
    tokens: 24,
    tools: 1,
  },
  {
    title: 'Validate the answer.',
    active: 'termination',
    phase: 'decision_ready',
    iteration: 2,
    text: `The model proposes an answer. Termination parses it and applies an optional validator.
      A rejection supplies feedback for another iteration; acceptance permits a final result.`,
    note: 'Limits and cancellation can end the run before an accepted answer.',
    entries: ['Model: look up policy P10', 'Tool: P10 · refunds within 14 days'],
    activity: 'Model response → decision saved → answer validation',
    tokens: 56,
    tools: 1,
  },
  {
    title: 'Finish with a permanent result.',
    active: 'journal',
    phase: 'terminal',
    iteration: 2,
    text: `ReAct records the accepted answer in history without adding it to another working
      context. The executor saves the final Content event, outcome, and terminal checkpoint together.`,
    note: 'After the save, replace the preview using SourceMessageIds. Restore reuses this answer.',
    entries: ['Model: look up policy P10', 'Tool: P10 · refunds within 14 days'],
    activity: 'Answer in history → execution finished → terminal checkpoint',
    tokens: 56,
    tools: 1,
  },
];

export const recipes = [
  {
    id: 'start',
    label: 'Start a run',
    text: `Your application supplies the model, task, and tools. The default context has an
      in-memory journal. Handle the error returned by Execute, including when the result is nil.`,
    code: `agent := react.NewAgent(model).
    WithRecoveryVersion("refund-agent/v1").
    RegisterTool(policyTool)

ctx := gent.NewExecutionContext(worker, "refund", gent.NewBasicLoopData(task))
exec := executor.NewRunner(agent, executor.DefaultConfig())
result, err := exec.Execute(ctx)
if err != nil {
    return err
}
// The application delivers result.Output.`,
  },
  {
    id: 'record',
    label: 'Record & observe',
    text: `Implement Recorder.Save for the application’s transactional store. Use WithRecorder for
      storage and WithObserver for the sequencer. The registry supplies mutable hooks.`,
    code: `codecs := gent.NewCodecRegistry()
seq, err := trace.NewSequencer(runID, trace.Config{
    Codecs: codecs,
    IncludeModelRequests: true,
    IncludeModelResponses: true,
})
if err != nil {
    return err
}
ctx := gent.NewExecutionContext(worker, "refund", gent.NewBasicLoopData(task),
    gent.WithRunId(runID), gent.WithRecorder(recorder),
    gent.WithObserver(seq), gent.WithCodecs(codecs))

events, unsubscribe := seq.Subscribe()
defer unsubscribe()
// Consume accepted display events. Use ctx.Export() for a full lossless read.`,
  },
  {
    id: 'restore',
    label: 'Restore a worker',
    text: `Create fresh compatible model/tool/agent objects. Supply custom components and compaction
      bindings when used. Credentials and executable behavior are supplied by the new worker.`,
    code: `codecs := gent.NewCodecRegistry()
if err := freshAgent.RegisterCodecs(codecs); err != nil {
    return err
}
seq, err := trace.RestoreSequencer(bundle, trace.Config{Codecs: codecs})
if err != nil {
    return err
}
freshRunner := executor.NewRunner(freshAgent, executor.DefaultConfig())
result, err := freshRunner.Resume(worker, bundle, gent.RestoreBindings{
    Recorder: replacementRecorder,
    Observers: []gent.RecordedEventObserver{seq},
    Codecs: codecs,
    ToolRecovery: recovery,
})
if err != nil {
    return err
}
// The application delivers result.Output.`,
  },
  {
    id: 'custom',
    label: 'Customize the loop',
    text: `Loop.Next remains yours. Use the owned context APIs and managed model call. Custom
      continuation requires explicit saved state and a recovery version.`,
    code: `type SessionData struct {
    *gent.BasicLoopData
    // Application fields; implement CheckpointState to restore mutable state.
}

type Loop interface {
    Next(*gent.ExecutionContext) (*gent.AgentLoopResult, error)
}

// In Next:
// 1. Read ctx.Task() and ctx.Scratchpad().
// 2. Call ctx.GenerateContentStream(model, call, request).
// 3. Consume chunks and await stream.Response().
// 4. Append observations through ctx.AppendRecord / ctx.UpdateHistory.
// 5. Return LAContinue or LATerminate with the result.
// Save custom continuation state at boundaries your loop can actually resume.`,
  },
];

export const questions = [
  {
    question:
      'Compaction leaves only one summary in the next prompt. What happened to the originals?',
    options: [
      'They remain in immutable history.',
      'They were replaced by the summary.',
      'They only remain in a connected viewer.',
    ],
    answer: 0,
    feedback: `Working context is a selection. History retains originals, summaries, and the
      decisions that changed that selection.`,
  },
  {
    question: 'A recorder reports a timeout. Is it safe to start the next external operation?',
    options: [
      'Yes, the event already has a number.',
      'No. Reconcile the exact failed batch first.',
      'Yes, if a viewer saw the update.',
    ],
    answer: 1,
    feedback: `A number is assigned before saving. A timeout might hide a successful commit, so the
      identical batch must be reconciled and execution restored safely.`,
  },
  {
    question:
      'A worker dies after submitting a refund but before saving the tool result. What next?',
    options: [
      'Submit the refund again automatically.',
      'Treat the run as successfully finished.',
      'Ask ToolRecovery to reconcile the logical call.',
    ],
    answer: 2,
    feedback: `Saved tool-ready state cannot prove whether the external effect happened. Completed,
      retry allowed, and unresolved are explicit recovery decisions.`,
  },
  {
    question:
      'The checkpoint is at 120, but saved progress ends at 128. What is the next event number?',
    options: [
      '121, because that follows the checkpoint.',
      '129, because all saved activity counts.',
      '1, because a new worker owns the run.',
    ],
    answer: 1,
    feedback: `Run identity and the full accepted cursor survive worker replacement. A new attempt
      identifies the replacement worker without resetting the run.`,
  },
];

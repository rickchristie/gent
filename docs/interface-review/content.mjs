// Copy explains the intent and behavioral obligations; the build supplies the actual Go API.
// Keep every interface explicit so a new contract cannot quietly inherit a vague description.
export const groups = [
  'Execution',
  'Model & content',
  'Tools & answers',
  'History & recovery',
  'Events & observation',
  'Compaction',
  'Search',
];

export const descriptions = {};
function describe(id, group, purpose, why, rules, implementations, related, question) {
  descriptions[id] = {
    group,
    purpose,
    why,
    rules,
    implementations,
    related,
    question,
  };
}

describe(
  'gent.Loop',
  'Execution',
  'Choose what one agent iteration does.',
  'The executor owns lifecycle and limits; the loop owns the reasoning pattern.',
  [
    'Return LAContinue or LATerminate through AgentLoopResult; return an error on failure.',
    'Use ExecutionContext for managed model calls, history updates, and acceptance barriers.',
    'A custom resumable loop also needs CheckpointState and an executor RecoveryVersion.',
  ],
  ['agents/react.Agent'],
  ['gent.LoopData', 'gent.ExecutionContext', 'executor.Runner', 'gent.CheckpointState', 'gent.Output'],
  'Which continuation state does a custom loop need to save?',
);


describe(
  'gent.LoopData',
  'Execution',
  'Supply the initial task and carry custom application data.',
  'History and the active selection have one owner: ExecutionContext.',
  [
    'GetTask is the complete interface now. Scratchpad and history edits go through the context.',
    'ExecutionContext freezes the task; custom mutable data needs its own synchronization.',
    'Implement CheckpointState when application fields must survive worker replacement.',
  ],
  ['gent.BasicLoopData'],
  ['gent.ExecutionContext', 'gent.CheckpointState', 'gent.HistoryEntry'],
  'Does any caller still expect LoopData to own or mutate the scratchpad?',
);

describe(
  'gent.TimeProvider',
  'Execution',
  'Provide time values for caller-authored prompts.',
  'An application clock makes time formatting and tests predictable.',
  [
    'Now, Today, Date, Time, Format, Weekday, and RelativeDate supply formatted time values.',
    'TimeProviderFunc wraps a clock function. The caller includes its values in Task messages.',
    'This is a prompt clock; execution deadlines and runtime accounting have separate ownership.',
  ],
  ['gent.DefaultTimeProvider', 'gent.TimeProviderFunc', 'gent.MockTimeProvider'],
  ['gent.Loop'],
  'Should a custom prompt clock change the application recovery version?',
);

describe(
  'gent.Model',
  'Model & content',
  'Adapt a provider request to a Gent stream.',
  'Provider networking stays separate from Gent recording, IDs, hooks, and accounting.',
  [
    'The adapter receives a standard context.Context and a resolved ModelCallRequest.',
    'Call through ExecutionContext.GenerateContentStream for managed capture and recovery.',
    'Honor cancellation and finish the producer before Response completes; avoid duplicate events.',
  ],
  ['Gent native provider adapters'],
  ['gent.Stream', 'gent.ModelCallRequest', 'gent.ModelNamer', 'gent.ExecutionContext'],
  'Is every agent model call using the managed context boundary?',
);

describe(
  'gent.Stream',
  'Model & content',
  'Deliver streaming chunks and the final provider result.',
  'Cancellation and producer completion are different events; final evidence must not arrive late.',
  [
    'Chunks carries MetaContent; Response returns candidate content, completion state, tools, and usage.',
    'The executor publishes one full Content event after validation and terminal save.',
    'SourceMessageIds link the final answer to previews; saved replay keeps message IDs.',
    'Close requests cancellation and is safe to repeat.',
    'Built-in stream producers must Complete after stopping, including on cancellation.',
    'The managed boundary accepts each chunk before delivering it to live consumers.',
  ],
  ['gent.StreamWithDuration'],
  ['gent.Model', 'gent.RecordedEventObserver', 'gent.ExecutionContext'],
  'Can a custom producer leave Response blocked after cancellation?',
);

describe(
  'gent.ModelNamer',
  'Model & content',
  'Optionally expose the model name.',
  'Sampling defaults can depend on model identity without widening the provider interface.',
  [
    'DefaultSamplingParams checks this optional capability.',
    'Without a name, Gent uses generic defaults. Callers can supply explicit options.',
  ],
  ['Gent native provider adapters'],
  ['gent.Model', 'gent.RecoveryConfig'],
  'Is the model identity sufficient to distinguish hidden provider configuration?',
);

describe(
  'gent.ContentPart',
  'Model & content',
  'Carry typed text, media, or provider content.',
  'Gent owns content values so adapters cannot silently lose provider data.',
  [
    'Gent supplies sealed text, media, native call, and result values.',
    'Opaque assistant continuation items travel with their provider identity.',
    'Owned history copies include nested parts, not just the surrounding slice.',
  ],
  ['Gent content values'],
  ['gent.ValueCodec', 'gent.CodecRegistry', 'gent.HistoryEntry'],
  'Are all application content parts registered for durable encoding?',
);

describe(
  'gent.TextFormat',
  'Model & content',
  'Render named text sections for tool results.',
  'A display format can organize tool observations without defining the answer protocol.',
  ['FormatSections renders the supplied sections. It does not parse model responses.'],
  ['format.XML', 'format.Markdown'],
  ['gent.ToolChain'],
  'Which rendering makes the tool result clear to the model?',
);


describe(
  'gent.Tool',
  'Tools & answers',
  'Execute one operation with typed input and output.',
  'Application business logic need not understand LLM formatting or reflection.',
  [
    'The chain validates the schema and converts arguments before Call receives typed input.',
    'The builder supplies schemas, search metadata, policy, strict mode, and output descriptions.',
    'Recover returns a decision, optional steering, and the recovered output when completed.',
    'Saved results and saved recovery decisions take precedence over another recovery check.',
    'DeduplicateSummary abbreviates older observations; it does not suppress tool execution.',
    'Use ToolIdentityFromContext for an external service’s idempotency or reconciliation key.',
  ],
  ['gent.DefineTool'],
  ['gent.ToolChain', 'gent.ToolRecoveryResponse', 'gent.ToolCallResult', 'gent.IndexableTool'],
  'Which tools have external side effects that need reconciliation?',
);

describe(
  'gent.ToolChain',
  'Tools & answers',
  'Expose native definitions and execute saved calls.',
  'Tool registration, prompt catalogs, typed conversion, and result formatting share one boundary.',
  [
    'NativeTools selects definitions. PrepareCalls saves a plan; ExecuteNext runs one step.',
    'RegisterTool accepts heterogeneous generic tools and panics on invalid or duplicate entries.',
    'Native assistant calls and tool result messages retain provider call IDs.',
    'RegisterCodecs and RecoveryConfig support fresh-worker continuation.',
  ],
  ['toolchain.Registry', 'toolchain.SearchChain', 'toolchain.JsToolChainWrapper'],
  ['gent.Tool', 'gent.CheckpointState', 'gent.ToolRecoveryResponse', 'gent.ToolCallResult'],
  'Does the selected chain support the recovery guarantees the application expects?',
);

describe(
  'gent.ParallelToolChain',
  'Tools & answers',
  'Declare whether independent tool plans can run at the same time.',
  'Programmatic parallel calls must respect the supplied chain and its shared state.',
  [
    'Embeds ToolChain and adds SupportsParallelCalls.',
    'Each concurrent plan must have its own checkpoint key.',
    'A chain override that needs sequential execution must return false.',
  ],
  ['toolchain.Registry', 'toolchain.SearchChain'],
  ['gent.ToolChain', 'gent.ToolPlan'],
  'Can this chain safely run independent plans in one execution context?',
);

describe(
  'gent.Output',
  'Tools & answers',
  'Define and process the typed answer contract.',
  'Provider completion identifies the candidate; local processing decides acceptance.',
  [
    'Instructions is caller-controlled prompt text. Configure sets native output controls.',
    'Process receives ordered content parts and returns OutputResult[T] with explicit Steering.',
    'Built-ins always run local checks, then an optional OutputCustomValidator callback.',
    'An error stops execution. Rejection retains the candidate and steering for correction.',
    'The runner saves the typed value and delivery content before returning success.',
  ],
  ['output.TextOutput', 'output.ImageOutput', 'output.JsonOutput[T]', 'output.SchemaOutput[T]'],
  ['gent.OutputResult', 'gent.OutputValidationSubscriber', 'gent.Checkpoint', 'gent.Loop'],
  'Which domain rules must run after schema validation?',
);


describe(
  'gent.Recorder',
  'History & recovery',
  'Atomically accept one numbered SaveBatch.',
  'The application owns durable storage and distributed writer fencing.',
  [
    'Validate writer authority and PreviousEventNumber inside the same storage transaction.',
    'Accept an identical BatchId idempotently; reject conflicting or stale writes.',
    'Store events, history effects, accounting, and the optional checkpoint atomically.',
    'A failed or uncertain save stops execution. RetrySave reuses the exact batch identity.',
    'An acknowledgment retry does not restart the canceled context; resume on a fresh one.',
  ],
  ['Application storage adapter; examples/history/main.go'],
  ['gent.SaveBatch', 'gent.RunView', 'gent.RecordedEventObserver', 'gent.CodecRegistry'],
  'How does the storage transaction fence a worker whose lease has expired?',
);

describe(
  'gent.RecordedEventObserver',
  'Events & observation',
  'Receive owned events after acceptance.',
  'Read-only display and replay must not change the effective inputs accepted by storage.',
  [
    'ObserveCommitted receives canonical event numbers and the codec registry for decoding.',
    'It is separate from mutable execution hooks; callbacks run after acceptance.',
    'A display projection may redact or truncate without changing the canonical RunView.',
  ],
  ['trace.Sequencer'],
  ['gent.Recorder', 'gent.RecordedEvent', 'trace.Sequencer', 'trace.Redactor'],
  'Does the consumer distinguish observer delivery failure from storage failure?',
);

describe(
  'gent.CheckpointState',
  'History & recovery',
  'Save and restore a component’s mutable local state.',
  'A compatible configuration alone cannot reconstruct a loop’s continuation state.',
  [
    'Declare a stable type and version, and encode an owned state value.',
    'Bind named components before saving; restore into fresh matching components.',
    'Save and restore only local state; do not publish events or call external services.',
    'Custom mutable LoopData and resumable custom loops use this explicit contract.',
  ],
  ['Application stateful components'],
  ['gent.RestoreBindings', 'gent.ValueCodec', 'gent.RecoveryConfig', 'gent.LoopData'],
  'Which mutable fields would be lost if the process stopped after this checkpoint?',
);

describe(
  'gent.RecoveryConfig',
  'History & recovery',
  'Expose declarative configuration for compatibility.',
  'Takeover must detect behavior changes while rebinding clients, credentials, and functions.',
  [
    'RecoveryConfig returns a value that can be fingerprinted deterministically.',
    'Hidden implementation behavior belongs in an explicit application recovery version.',
    'This describes configuration; CheckpointState saves mutable continuation state.',
  ],
  [
    'compaction.SlidingWindowStrategy',
    'compaction.SummarizationStrategy',
    'compaction.StatThresholdTrigger',
  ],
  ['gent.CheckpointState', 'gent.RestoreBindings'],
  'What behavior is invisible to the declared configuration fingerprint?',
);


describe(
  'gent.ValueCodec',
  'History & recovery',
  'Encode an application value with stable type and version.',
  'Plain JSON cannot reconstruct concrete values hidden behind interfaces or private fields.',
  [
    'Register before constructing contexts or decoding saved JSON.',
    'Encoding is deterministic and local. Decoding returns fresh owned values.',
    'Unknown types, unsupported versions, cycles, callbacks, and channels fail explicitly.',
    'Use CodecRegistry.Marshal and Unmarshal for SaveBatch and RunView.',
  ],
  ['Application codecs'],
  ['gent.CodecRegistry', 'gent.CheckpointState', 'gent.ContentPart'],
  'Can this codec round-trip dynamic metadata without changing its concrete Go type?',
);

describe(
  'gent.CodedError',
  'History & recovery',
  'Give application errors a stable recovery classification.',
  'An error’s meaning can survive persistence without serializing its private implementation.',
  [
    'Embeds error and adds ErrorCode. Use an application namespace with stable semantics.',
    'SavedError preserves text, causes, and known classifications for errors.Is.',
    'Use an application value codec if private error fields must also be retained.',
  ],
  ['Application errors'],
  ['gent.ValueCodec', 'gent.RunView'],
  'Are error codes stable enough to drive decisions after deployment changes?',
);

describe(
  'gent.Event',
  'Events & observation',
  'Identify the framework’s typed event values.',
  'A common event family lets the registry dispatch typed lifecycle hooks.',
  [
    'The unexported event marker constrains direct implementations outside package gent.',
    'Use CommonEvent and PublishCommonEvent for application event payloads.',
    'Mutable hooks see EventNumber zero; accepted evidence receives canonical numbering.',
  ],
  ['gent.BaseEvent and framework event types'],
  ['gent.EventPublisher', 'gent.CommonEventSubscriber', 'gent.RecordedEvent'],
  'Is application observation reading accepted evidence or a live hook?',
);

describe(
  'gent.EventPublisher',
  'Events & observation',
  'Dispatch typed events with a recursion limit.',
  'ExecutionContext can publish hooks without depending on the registry implementation.',
  [
    'The executor installs its event registry on the context.',
    'Dispatch routes events to matching subscribers; MaxRecursion bounds nested dispatch.',
    'Hook dispatch itself is not the durable acceptance boundary.',
  ],
  ['events.Registry'],
  ['gent.Event', 'gent.RecordedEventObserver', 'gent.ExecutionContext'],
  'Could a subscriber publish recursively and exceed the configured limit?',
);

describe(
  'trace.Redactor',
  'Events & observation',
  'Transform sensitive values in the display projection.',
  'Trace presentation has different retention and disclosure needs from canonical recovery data.',
  [
    'Redact requests, responses, tools, prompts, changes, output, chunks, and errors.',
    'Implementations must be fast, non-blocking, and concurrency-safe.',
    'Redaction changes trace.Event, not gent.RecordedEvent or the full unredacted RunView.',
  ],
  ['trace.RedactorFuncs'],
  ['gent.RecordedEventObserver', 'trace.Sequencer', 'gent.RunView'],
  'Are display redaction and access controls for canonical storage both configured?',
);

const hooks = [
  ['BeforeExecution', 'Before the run’s initial lifecycle work.', 'Initialize execution policy.'],
  ['AfterExecution', 'When execution produces its completion event.', 'Inspect the run outcome.'],
  ['BeforeIteration', 'Before Loop.Next for an iteration.', 'Inspect or prepare loop policy.'],
  ['AfterIteration', 'After an iteration returns.', 'Inspect the iteration result.'],
  [
    'BeforeModelCall',
    'Before the effective provider request is frozen and accepted.',
    'May modify Messages and resolved Options on ModelCallRequest.',
  ],
  [
    'AfterModelCall',
    'After a managed model call finishes.',
    'Observe output, usage, and error on a copied lifecycle value.',
  ],
  [
    'BeforeToolCall',
    'Before effective typed tool input is saved and execution begins.',
    'May modify effective arguments; an authorized recovery retry reuses the saved input.',
  ],
  [
    'AfterToolCall',
    'After a tool invocation produces its result.',
    'Inspect the tool outcome on a copied lifecycle value.',
  ],
  [
    'ParseError',
    'When native tool argument parsing reports failure.',
    'Inspect parse error details; error counters and consecutive gauges are updated by publishing.',
  ],
  [
    'OutputValidation',
    'When output processing or a custom validation stage completes.',
    'Inspect acceptance, steering, and errors; rejected stages update output rejection accounting.',
  ],
  [
    'Error',
    'When an execution error is published.',
    'Inspect the error and its execution context.',
  ],
  [
    'CommonEvent',
    'When application code publishes a CommonEvent.',
    'Use the event name to distinguish application-specific payloads.',
  ],
  [
    'Compaction',
    'When compaction evidence is published.',
    'Inspect the compaction decision; accepted history remains the canonical source.',
  ],
  [
    'LimitExceeded',
    'When a statistic exceeds a configured limit.',
    'Inspect the exceeded limit; publication precedes cancellation with a cause.',
  ],
];
for (const [name, when, behavior] of hooks) {
  describe(
    `gent.${name}Subscriber`,
    'Events & observation',
    when,
    'Small subscriber interfaces let one receiver opt into only the events it needs.',
    [
      behavior,
      'Register the receiver with events.Registry; matching interfaces are detected automatically.',
      'Use RecordedEventObserver for accepted events. Lifecycle hooks are not storage commits.',
    ],
    ['Application subscribers; events.Registry dispatch'],
    ['gent.EventPublisher', 'gent.RecordedEventObserver', 'gent.ExecutionContext'],
    `Does this ${name} hook need mutation, lifecycle observation, or accepted evidence?`,
  );
}

describe(
  'gent.StateChangeEventSubscriber',
  'Events & observation',
  'Observe an accepted structured history transition.',
  'Consumers need explicit revisions and selections instead of inferring changes from list length.',
  [
    'Receives HistoryChange with previous/new revision, appended entries, selections, and reason.',
    'Runs after acceptance, outside commit locks; callbacks may publish more events.',
    'This subscriber is the acceptance-aware exception among the typed lifecycle subscriptions.',
  ],
  ['Application subscribers; events.Registry dispatch'],
  ['gent.HistoryChange', 'gent.HistoryUpdate', 'gent.RecordedEventObserver'],
  'Can the consumer handle reorder and same-length replacements?',
);

describe(
  'gent.CompactionTrigger',
  'Compaction',
  'Decide when the working context should be compacted.',
  'Trigger policy is separate from the strategy that chooses retained or summarized entries.',
  [
    'The executor checks after the first iteration, before BeforeIterationEvent.',
    'NotifyCompacted can return an error and updates a successful compaction’s baseline.',
    'Stateful trigger baselines must participate in checkpoint state for continuation.',
  ],
  ['compaction.StatThresholdTrigger'],
  ['gent.CompactionStrategy', 'gent.CheckpointState', 'gent.ExecutionStats'],
  'Does the trigger compare absolute gauges or counter deltas?',
);

describe(
  'gent.CompactionStrategy',
  'Compaction',
  'Propose summary entries and a new active selection.',
  'Compaction changes the next model context while keeping original evidence append-only.',
  [
    'Receives owned CompactionInput entries and returns a HistoryUpdate proposal.',
    'Do not mutate the context’s selected records directly.',
    'The executor accepts the decision, selection, trigger state, stats, and checkpoint together.',
    'A compaction error is fatal and recorded; it is not silently ignored.',
  ],
  ['compaction.SlidingWindowStrategy', 'compaction.SummarizationStrategy'],
  ['gent.CompactionTrigger', 'gent.HistoryUpdate', 'gent.HistoryEntry', 'gent.Checkpoint'],
  'Does the proposal preserve pinned records and enough provenance to explain the summary?',
);

describe(
  'gent.IndexableTool',
  'Search',
  'Describe a tool for discovery indexes.',
  'Search metadata is optional and separate from executing a tool.',
  [
    'Searchable tools implement both Tool and IndexableTool on the same receiver.',
    'Name and Description are shared; domain, categories, keywords, and queries support retrieval.',
  ],
  ['Application searchable tools'],
  ['gent.Tool', 'gent.ToolSearcher', 'search.SearchIndex'],
  'Do synthetic queries describe how people ask for the operation?',
);

describe(
  'gent.ToolSearcher',
  'Search',
  'Index and rank tool names for a search query.',
  'The search chain owns paging while backends provide different retrieval strategies.',
  [
    'Id identifies query_type; SearchGuidance explains the engine’s query syntax.',
    'IndexAll replaces or refreshes indexed tool metadata.',
    'Search returns all matches in ranked order. Its errors are surfaced to the model.',
  ],
  ['toolchain.RegexToolSearcher', 'toolchain.IndexToolSearcher', 'toolchain.FusedToolSearcher'],
  ['gent.IndexableTool', 'search.SearchIndex', 'search.Fuser', 'gent.ToolChain'],
  'Is the backend accidentally paginating before the search chain does?',
);

describe(
  'search.SearchIndex',
  'Search',
  'Store domain documents and retrieve ranked results.',
  'The search package is independent of agents, tools, and policies.',
  [
    'Implementations must be safe for concurrent use.',
    'Add replaces an existing ID; Swap atomically replaces the full collection.',
    'Search returns top-K SearchResult values. Score meaning depends on the backend.',
  ],
  ['search.FlatIndex', 'search.BleveIndex', 'search.FusedIndex'],
  ['search.ChunkAdapter', 'search.BleveAdapter', 'search.Fuser', 'gent.ToolSearcher'],
  'Does a bulk registry refresh use Swap to avoid inconsistent intermediate search results?',
);

describe(
  'search.Embedder',
  'Search',
  'Encode queries and passages into dense vectors.',
  'Query and document preprocessing can differ while sharing one tokenizer.',
  [
    'Embeds TokenCounter and exposes single-query, single-text, and batch-text embedding.',
    'MaxTokens sets the chunking ceiling; Dimensions describes the output vector size.',
    'Implementations are concurrent-safe. Close releases resources.',
  ],
  ['search.NewOnnxEmbedder; application embedders'],
  ['search.TokenCounter', 'search.ChunkAdapter', 'search.SearchIndex'],
  'Does the chunker use the same tokenizer and token ceiling as embedding?',
);

describe(
  'search.TokenCounter',
  'Search',
  'Count tokens without running embedding inference.',
  'Chunk boundaries should respect the tokenizer actually used by the embedding model.',
  [
    'TokenCount is model-specific, not a character or whitespace count.',
    'Embedder embeds this smaller interface; TokenCounterFunc adapts a function.',
  ],
  ['search.TokenCounterFunc', 'Embedder implementations'],
  ['search.Embedder', 'search.ChunkAdapter'],
  'Are tests checking token boundaries with a representative counting function?',
);

describe(
  'search.ChunkAdapter',
  'Search',
  'Convert a domain document into token-aware chunks.',
  'Domain structure and retrieval boundaries belong outside the generic vector index.',
  [
    'Chunks receives both TokenCounter and the model’s maximum token length.',
    'Preserve useful source text in each Chunk for result snippets.',
  ],
  ['toolchain.ToolChunkAdapter', 'Application document adapters'],
  ['search.TokenCounter', 'search.Embedder', 'search.SearchIndex'],
  'Which domain context must be repeated across chunks?',
);

describe(
  'search.BleveAdapter',
  'Search',
  'Map documents and queries into Bleve’s text-search model.',
  'The domain chooses fields and boosts; the index handles storage and top-K retrieval.',
  [
    'Mapping defines the schema once; Convert must produce fields that match it.',
    'Query builds the query structure, not the page size.',
    'Snippet highlighting depends on mapping and query configuration.',
  ],
  ['toolchain.ToolBleveAdapter', 'Application document adapters'],
  ['search.SearchIndex', 'search.BleveIDFProvider'],
  'Do query fields, analyzers, and converted documents agree?',
);

describe(
  'search.BleveIDFProvider',
  'Search',
  'Optionally declare fields for BM25 maximum normalization.',
  'Fusion needs comparable scores without amplifying weak keyword matches.',
  [
    'Implemented alongside BleveAdapter when theoretical-maximum normalization is wanted.',
    'The analyzer must match the mapping. Include searched text fields and their boosts.',
    'Exclude exact-match keyword fields from this calculation.',
  ],
  ['toolchain.ToolBleveAdapter'],
  ['search.BleveAdapter', 'search.Fuser'],
  'Are field boosts and analyzers consistent with the actual query?',
);

describe(
  'search.Fuser',
  'Search',
  'Combine ranked results from multiple named search sources.',
  'Different relevance scales need an explicit composition policy.',
  [
    'Handle missing documents in a source and empty source result sets.',
    'Account for different score scales such as BM25 and cosine similarity.',
    'WeightedLinearFuser uses configured weights and optional source normalization.',
  ],
  ['search.WeightedLinearFuser'],
  ['search.SearchIndex', 'search.BleveIDFProvider', 'gent.ToolSearcher'],
  'Are weights and normalization meaningful for the configured backends?',
);

export const supporting = {
  'gent.Task': [
    'Execution', 'Hold the caller-authored message sequence.',
    'System instructions and user text or media remain outside the compacted working history.',
  ],
  'gent.ModelMessage': [
    'Model & content', 'Carry one role, ordered parts, and native continuation data.',
    'Task and request messages use pointers. Accepted snapshots own their mutable data.',
  ],
  'gent.OutputResult': [
    'Tools & answers', 'Return explicit acceptance, typed value, delivery content, or steering.',
    'Expected rejection is data. Unexpected processing failure uses error and stops the run.',
  ],
  'gent.OutputValidationResult': [
    'Tools & answers', 'Return the optional custom validator decision.',
    'Domain validation can reject with steering after all built-in local checks pass.',
  ],
  'gent.ToolRecoveryResponse': [
    'History & recovery',
    'Keep a recovery decision, its steering message, and any recovered output together.',
    'Gent saves this response before using it. A message alone never permits another attempt.',
  ],
  'gent.ToolBuilder': [
    'Tools & answers',
    'Build a tool from a typed function and explicit field settings.',
    'Build checks schemas and copies metadata; a built definition can be shared between runs.',
  ],
  'gent.ExecutionContext': [
    'Execution',
    'Own execution, history, acceptance, limits, and children.',
    'The central concrete hub. Its public method inventory is extracted from every source file.',
  ],
  'executor.Runner': [
    'Execution',
    'Run the lifecycle and cross safe continuation boundaries.',
    'NewRunner infers T from Loop[T]. Execute and Resume return T, execution metadata, and error.',
  ],
  'gent.HistoryEntry': [
    'History & recovery',
    'Preserve one identified piece of run evidence.',
    'Messages, summaries, replacements, and compaction decisions remain append-only.',
  ],
  'gent.HistorySnapshot': [
    'History & recovery',
    'Read accepted entries and selection transitions.',
    'ContextAt reconstructs the ordered selection at an accepted history revision.',
  ],
  'gent.HistoryUpdate': [
    'History & recovery',
    'Propose appended entries and the complete active order.',
    'ExpectedDraftRevision detects stale local edits, which fail with ErrHistoryConflict.',
  ],
  'gent.HistoryChange': [
    'History & recovery',
    'Describe an accepted history revision transition.',
    'Explicit selections make reorder, replacement, expiry, and removal observable.',
  ],
  'gent.SaveBatch': [
    'History & recovery',
    'Bundle evidence, accounting, and an optional checkpoint.',
    'The recorder must accept all of this transaction together with its expected previous cursor.',
  ],
  'gent.RecordedEvent': [
    'Events & observation',
    'Retain the lossless numbered activity record.',
    'Payload is encoded with the registry; Change carries structured history edits.',
  ],
  'gent.RunView': [
    'History & recovery',
    'Hold the complete accepted state and latest checkpoint.',
    'Its accepted cursor may be newer than the checkpoint’s resumable control cursor.',
  ],
  'gent.Checkpoint': [
    'History & recovery',
    'Identify where control can continue.',
    'Includes context state, compatibility, accounting, and an optional terminal outcome.',
  ],
  'gent.RestoreBindings': [
    'History & recovery',
    'Supply fresh code and services to a restored run.',
    'The application rebinds clients, credentials, codecs, components, and recovery decisions.',
  ],
  'gent.CodecRegistry': [
    'History & recovery',
    'Preserve concrete dynamic types across owned copies.',
    'Register explicit stable names and versions; decode never guesses or loads executable code.',
  ],
  'gent.ModelCallRequest': [
    'Model & content',
    'Capture effective messages, options, and source selection.',
    'Before-hooks finish before this request is frozen, accepted, and delivered to the provider.',
  ],
  'gent.ToolPlan': [
    'Tools & answers',
    'Remember ordered calls, the next cursor, and completed results.',
    'Logical identity comes from the planned position, not a hash of tool arguments.',
  ],
  'gent.ToolCallResult': [
    'Tools & answers',
    'Preserve complete tool evidence and its captured abbreviation.',
    'Raw/typed input, typed output, IDs, text, media, instructions, and errors remain available.',
  ],
  'gent.ExecutionStats': [
    'Execution',
    'Maintain propagated counters and local gauges.',
    'Counters have $self: values; gauges stay local. Replay imports stats without recounting.',
  ],
  'gent.Limit': [
    'Execution',
    'Apply an exact-key or key-prefix limit to a statistic.',
    'Every stats update checks applicable limits and cancels execution when exceeded.',
  ],
  'trace.Sequencer': [
    'Events & observation',
    'Project accepted canonical events for display.',
    'Snapshot, EventsAfter, and Subscribe support viewers; RunView remains the lossless source.',
  ],
};

export const phases = [
  [
    'initial',
    'Prepare the run',
    'The task is owned; configuration and state bindings are ready.',
    'No agent iteration has completed.',
    ['gent.LoopData', 'gent.CheckpointState', 'executor.Runner'],
  ],
  [
    'iteration_ready',
    'Start an iteration',
    'The next loop iteration can begin from a stable boundary.',
    'Accept requests before provider calls. Chunks and usage can advance the journal.',
    ['gent.Loop', 'gent.Model', 'gent.Stream'],
  ],
  [
    'decision_ready',
    'Keep the model decision',
    'ReAct has saved the parsed decision and can continue without generating it again.',
    'A completed response beyond the checkpoint can be reused. Partial attempts stay visible.',
    ['gent.Model', 'gent.Output', 'gent.ModelCallRequest'],
  ],
  [
    'tool_ready',
    'Accept effective tool input',
    'A plan position and its final typed input are saved before an external tool call.',
    'After a crash the outcome may be uncertain. Reconcile it before retrying.',
    ['gent.ToolChain', 'gent.ToolRecoveryResponse', 'gent.ToolPlan'],
  ],
  [
    'tool_completed',
    'Keep the tool result',
    'The completed result and plan cursor can be reused in order.',
    'The next planned tool has its own logical ID, even if its name and arguments are identical.',
    ['gent.ToolCallResult', 'gent.ToolChain'],
  ],
  [
    'iteration_completed',
    'Accept the iteration',
    'Iteration evidence and working-context changes reach a continuation boundary.',
    'Rejected answers supply feedback for another iteration. Accepted answers lead to terminal.',
    ['gent.HistoryUpdate', 'gent.Output', 'gent.Loop'],
  ],
  [
    'compaction_completed',
    'Keep the compaction decision',
    'Selection, summaries, provenance, and trigger baseline commit together between iterations.',
    'This boundary is optional. It does not follow every iteration. Originals remain in history.',
    ['gent.CompactionTrigger', 'gent.CompactionStrategy', 'gent.HistorySnapshot'],
  ],
  [
    'terminal',
    'Return the saved outcome',
    'Completion, failure, or intentional run cancellation has a saved outcome.',
    'Restore the saved outcome without provider or tool work. Custom codecs may still be needed.',
    ['gent.Checkpoint', 'gent.RunView', 'executor.Runner'],
  ],
];

export const boundaries = [
  [
    'One owner for context',
    'LoopData → ExecutionContext',
    'LoopData supplies GetTask. Read owned task/context values; edit through history APIs.',
    'Draft revisions protect local edits. Accepted history revisions advance only at acceptance.',
    ['gent.LoopData', 'gent.ExecutionContext', 'gent.HistoryUpdate'],
  ],
  [
    'Evidence and control are separate',
    'FlushProgress ≠ CommitCheckpoint',
    'Accepted chunks and activity can advance RunView beyond the last resumable checkpoint.',
    'Continue numbering after the full accepted cursor, never just the checkpoint cursor.',
    ['gent.Recorder', 'gent.RunView', 'gent.Checkpoint'],
  ],
  [
    'Hooks and accepted observation',
    'Mutable input → frozen evidence → observers',
    'Model, tool, and system-prompt before-hooks mutate input; other lifecycle values are copies.',
    'RecordedEventObserver and StateChangeEventSubscriber see evidence after acceptance.',
    [
      'gent.BeforeModelCallSubscriber',
      'gent.RecordedEventObserver',
      'gent.StateChangeEventSubscriber',
    ],
  ],
  [
    'Uncertain work stays explicit',
    'Logical call ID ≠ worker attempt ID',
    'Call identity survives takeover; the attempt changes. Tool-ready cannot prove a side effect.',
    'Tool.Recover returns a completed result, permits a retry, or leaves the operation unresolved.',
    ['gent.ToolRecoveryResponse', 'gent.ToolChain', 'gent.ToolPlan'],
  ],
  [
    'Display is a projection',
    'RecordedEvent → trace.Event',
    'Sequencer projects accepted evidence. Display redaction and retention leave the run intact.',
    'Reconnect from RunView, discard duplicates, apply contiguous updates, and reload on gaps.',
    ['gent.RecordedEvent', 'gent.RecordedEventObserver', 'trace.Redactor'],
  ],
  [
    'Recovery has declared limits',
    'Fresh bindings + compatible state',
    'Custom loops need state and versions. Active children cannot resume. JavaScript returns partial evidence.',
    'Worker cancellation keeps a recovery boundary. CancelRun means terminal cancellation.',
    ['gent.CheckpointState', 'gent.RecoveryConfig', 'gent.RestoreBindings'],
  ],
];

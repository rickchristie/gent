// These are small teaching models, not a JavaScript implementation of Gent. Their observable
// transitions mirror the documented boundaries so failure cases can be explored without APIs.
export const lessons = [
  {
    id: 'loop',
    title: 'Follow a request',
    verb: 'Run the loop',
    question: 'Who decides what happens next?',
    takeaway: 'The loop decides. The executor repeats. The context records what actually happened.',
    contracts: [
      'gent.Loop',
      'gent.LoopData',
      'gent.TimeProvider',
      'gent.Model',
      'gent.Stream',
      'gent.ModelNamer',
      'gent.ContentPart',
      'gent.TextFormat',
      'gent.Tool',
      'gent.ToolChain',
      'gent.ParallelToolChain',
      'gent.Output',
    ],
  },
  {
    id: 'memory',
    title: 'Change its memory',
    verb: 'Edit the context',
    question: 'If a message leaves the prompt, did it disappear?',
    takeaway: 'History preserves the evidence. Working context selects what the next model sees.',
    contracts: ['gent.CompactionTrigger', 'gent.CompactionStrategy'],
  },
  {
    id: 'save',
    title: 'Break a save',
    verb: 'Lose the acknowledgment',
    question: 'When is evidence safe for an observer to see?',
    takeaway: 'Gent accepts the save before notifying recorded observers or calling the provider.',
    contracts: [
      'gent.Recorder',
      'gent.Event',
      'gent.EventPublisher',
      'gent.RecordedEventObserver',
      'trace.Redactor',
      'gent.BeforeExecutionSubscriber',
      'gent.AfterExecutionSubscriber',
      'gent.BeforeIterationSubscriber',
      'gent.AfterIterationSubscriber',
      'gent.BeforeModelCallSubscriber',
      'gent.AfterModelCallSubscriber',
      'gent.BeforeToolCallSubscriber',
      'gent.AfterToolCallSubscriber',
      'gent.ParseErrorSubscriber',
      'gent.OutputValidationSubscriber',
      'gent.ErrorSubscriber',
      'gent.CommonEventSubscriber',
      'gent.CompactionSubscriber',
      'gent.LimitExceededSubscriber',
      'gent.StateChangeEventSubscriber',
    ],
  },
  {
    id: 'restart',
    title: 'Replace the worker',
    verb: 'Resolve an uncertain tool',
    question: 'The worker died. Was the refund sent?',
    takeaway:
      'A tool-ready checkpoint proves intent. Recovery needs evidence of the external result.',
    contracts: [
      'gent.CheckpointState',
      'gent.RecoveryConfig',
      'gent.ValueCodec',
      'gent.CodedError',
    ],
  },
  {
    id: 'search',
    title: 'Find the right tool',
    verb: 'Mix retrieval strategies',
    question: 'How does “money back” find a tool named refund_order?',
    takeaway: 'Adapters describe the domain. Indexes retrieve. Fusion ranks. The tool chain pages.',
    contracts: [
      'gent.IndexableTool',
      'gent.ToolSearcher',
      'search.SearchIndex',
      'search.Embedder',
      'search.TokenCounter',
      'search.ChunkAdapter',
      'search.BleveAdapter',
      'search.BleveIDFProvider',
      'search.Fuser',
    ],
  },
];

export function loopFrames(route, format) {
  const nativeCall = JSON.stringify(
    {
      id: 'call-1',
      type: 'function',
      function: {
        name: 'lookup_order',
        arguments: route === 'malformed' ? '{' : '{"order_id":42}',
      },
    },
    null,
    2,
  );
  const answer = route === 'rejected' ? 'Probably delivered.' : 'Order 42 ships on Friday.';
  const response = route === 'tool' || route === 'malformed' ? nativeCall : answer;
  const prompt =
    format === 'xml'
      ? '<instructions>Use tools to check the order.</instructions>'
      : '# instructions\nUse tools to check the order.';
  const frames = [
    [
      'Task',
      'gent.LoopData',
      'The application supplies the task.',
      'ExecutionContext freezes the task. Compaction changes history, not the original task.',
      'Where is order 42?',
      'Start the iteration →',
    ],
    [
      'Decide',
      'gent.Loop',
      'The runner calls Loop.Next.',
      'The loop builds a request from the task and selected history. It chooses the agent pattern.',
      `${prompt}\nTask + active history + native definitions → model request`,
      'Call the model →',
    ],
    [
      'Generate',
      'gent.Model',
      'The managed context accepts the effective request first.',
      'Stream delivers provisional MetaContent; provider completion supplies a candidate.',
      response,
      'Read the response →',
    ],
    [
      'Read',
      route === 'tool' || route === 'malformed' ? 'gent.ToolChain' : 'gent.Output',
      'Read native calls or candidate assistant text.',
      'ToolChain prepares structured calls. Output checks a candidate after native completion.',
      response,
      route === 'tool' ? 'Execute the tool →' : 'Check the response →',
    ],
  ];
  if (route === 'malformed') {
    frames.push([
      'Retry',
      'gent.ToolChain',
      'The tool arguments are not valid JSON.',
      'The parse failure becomes feedback. Another iteration can run within the executor’s limits.',
      'parse error → feedback → LAContinue',
      'Try a different response',
    ]);
  } else if (route === 'tool') {
    frames.push([
      'Act',
      'gent.ToolChain',
      'The chain validates, converts, and calls the tool.',
      'Tool accepts typed input. The chain owns JSON decoding, schema checks, and result formatting.',
      'lookup_order({order_id: 42}) → {status: "ships Friday"}',
      'Keep the observation →',
    ]);
    frames.push([
      'Remember',
      'gent.Tool',
      'The tool result becomes a history entry.',
      'The next prompt selects this observation. Tool use continues the loop toward an answer.',
      `HistoryEntry H1: Order 42 ships Friday.
Next working context: [H1]`,
      'Ask again with the observation →',
    ]);
    frames.push([
      'Answer',
      'gent.Output',
      'The new answer is checked before ending.',
      'Output processes the answer and runs local checks plus the optional custom validator.',
      'Order 42 ships on Friday. → accepted → LATerminate',
      'Iteration complete',
    ]);
  } else {
    frames.push([
      'Validate',
      'gent.Output',
      route === 'rejected'
        ? 'The answer parses, but the validator rejects it.'
        : 'The answer passes validation.',
      route === 'rejected'
        ? 'Feedback asks for evidence. A valid answer format can still require another iteration.'
        : 'The executor saves Content with the terminal outcome, then publishes the accepted answer.',
      route === 'rejected'
        ? '“Use the order tool; do not guess.” → LAContinue'
        : `${answer} → LATerminate`,
      'Try a different response',
    ]);
  }
  return frames;
}

export function newMemory() {
  return {
    entries: [
      {
        id: 'H1',
        kind: 'original',
        text: 'Policy: refunds allowed within 14 days.',
      },
      { id: 'H2', kind: 'original', text: 'Order 42 was bought 10 days ago.' },
      { id: 'H3', kind: 'original', text: 'The receipt has been verified.' },
      {
        id: 'H4',
        kind: 'original',
        text: 'The user requested the original payment method.',
      },
    ],
    selections: [['H1', 'H2', 'H3', 'H4']],
    reason: 'Four observations are selected for the next prompt.',
  };
}

export function changeMemory(state, action, pinned = false) {
  const active = state.selections.at(-1);
  const entries = state.entries.map((entry) => ({ ...entry }));
  let selected = [...active];
  let reason;
  const revision = state.selections.length + 1;
  if (action === 'reverse') {
    selected.reverse();
    reason = 'Same entries, new order. A selection transition records the change.';
  } else if (action === 'window') {
    selected = active.slice(-2);
    if (pinned && active.includes('H1') && !selected.includes('H1')) selected.unshift('H1');
    entries.push({
      id: `C${revision}`,
      kind: 'decision',
      text: `Sliding window: [${active.join(', ')}] → [${selected.join(', ')}].`,
    });
    reason = 'Older entries leave the working context. Their original evidence stays in history.';
  } else if (action === 'summary') {
    const candidates = active.filter((id) => !pinned || id !== 'H1').slice(0, 2);
    if (candidates.length < 2) return state;
    const id = `S${revision}`;
    const text = candidates.map((id) => entries.find((entry) => entry.id === id).text).join(' ');
    entries.push({
      id,
      kind: 'summary',
      text: `Summary of ${candidates.join(' + ')}: ${text}`,
    });
    selected = [id, ...active.filter((id) => !candidates.includes(id))];
    entries.push({
      id: `C${revision}`,
      kind: 'decision',
      text: `Summarization: [${active.join(', ')}] → [${selected.join(', ')}].`,
    });
    reason = 'A summary and its provenance are appended. Compaction never erases the originals.';
  } else {
    throw new Error(`Unknown memory action: ${action}`);
  }
  return { entries, selections: [...state.selections, selected], reason };
}

export function newSave(outcome = 'lost') {
  return {
    outcome,
    step: 0,
    stored: 120,
    accepted: 120,
    checkpoint: 120,
    batch: 'batch-17',
    provider: 'waiting',
    observer: 'waiting',
    attempts: 0,
  };
}

export function advanceSave(state) {
  if (state.step < 2) return { ...state, step: state.step + 1 };
  if (state.step === 2) {
    const success = state.outcome === 'success';
    return {
      ...state,
      step: 3,
      attempts: 1,
      stored: state.outcome === 'rejected' ? 120 : 121,
      accepted: success ? 121 : 120,
      provider: success ? 'allowed' : 'stopped',
      observer: success ? 'event 121' : 'waiting',
    };
  }
  if (state.step === 3 && state.outcome === 'lost') {
    return {
      ...state,
      step: 4,
      accepted: 121,
      attempts: 2,
      observer: 'event 121',
    };
  }
  return state;
}

export function newRecovery(point = 'after-effect') {
  return {
    point,
    call: 'refund-42/call-7',
    attempt: 'worker-A',
    effects: point === 'before-effect' ? 0 : 1,
    restored: false,
    status: 'interrupted',
  };
}

export function recoverWorker(state, action, options) {
  if (options.version !== 'v1') return { ...state, status: 'incompatible version' };
  if (!options.codec) return { ...state, status: 'missing value codec' };
  const restored = { ...state, attempt: 'worker-B', restored: true };
  if (state.point === 'after-result') return { ...restored, status: 'saved result reused' };
  if (action === 'restore' || action === 'unresolved') {
    return {
      ...restored,
      status: 'unknown outcome; model receives recovery guidance',
    };
  }
  if (action === 'completed') {
    if (state.effects === 0) return { ...restored, status: 'no external receipt to return' };
    return { ...restored, status: 'reconciled with receipt' };
  }
  if (action === 'retry') {
    const effects = options.idempotent && state.effects > 0 ? state.effects : state.effects + 1;
    return { ...restored, effects, status: 'retry explicitly allowed' };
  }
  throw new Error(`Unknown recovery action: ${action}`);
}

export const searchFixtures = {
  refund: {
    label: 'refund order',
    scores: [
      [0.95, 0.9],
      [0.4, 0.35],
      [0.05, 0.08],
    ],
  },
  meaning: {
    label: 'get my money back',
    scores: [
      [0, 0.94],
      [0, 0.32],
      [0, 0.06],
    ],
  },
  travel: {
    label: 'cancel my trip',
    scores: [
      [0, 0.12],
      [0.05, 0.08],
      [0.96, 0.97],
    ],
  },
};
export const searchTools = [
  {
    id: 'refund_order',
    description: 'Refund an order to its original payment method.',
  },
  {
    id: 'lookup_order',
    description: 'Look up an order, its status, and purchase date.',
  },
  { id: 'cancel_reservation', description: 'Cancel a travel reservation.' },
];

export function rankTools(query, mode, weight) {
  const fixture = searchFixtures[query];
  return searchTools
    .map((tool, index) => {
      const [keyword, semantic] = fixture.scores[index];
      const score =
        mode === 'keyword'
          ? keyword
          : mode === 'semantic'
            ? semantic
            : (1 - weight) * keyword + weight * semantic;
      return { ...tool, keyword, semantic, score };
    })
    .filter((tool) => tool.score > 0)
    .sort((a, b) => b.score - a.score);
}

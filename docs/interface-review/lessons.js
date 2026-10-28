// Bundled with lesson-models.mjs inside one closure. Keep source contracts in the same payload
// as the reference so the teaching view cannot silently drift to an older API signature.
const lessonData = JSON.parse(document.getElementById('gent-data').textContent);
const contracts = new Map(lessonData.entries.map((item) => [item.id, item]));
const el = (id) => document.getElementById(id);
const safe = (value) =>
  String(value).replace(
    /[&<>"']/g,
    (char) =>
      ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
      })[char],
  );
const lessonState = {
  lesson: 'loop',
  explored: new Set(),
  answers: {},
  loop: { route: 'tool', format: 'xml', step: 0 },
  memory: newMemory(),
  memoryRevision: 0,
  pinPolicy: false,
  save: newSave(),
  recovery: newRecovery(),
  recoveryOptions: { version: 'v1', codec: true, idempotent: true },
  search: { query: 'meaning', mode: 'keyword', weight: 0.7, page: 0 },
};
const quiz = {
  loop: {
    question: 'A valid answer is rejected by the domain validator. What happens next?',
    choices: ['The executor stops successfully', 'Feedback goes into another iteration'],
    correct: 1,
    explain:
      'Parsing is not acceptance. Termination can reject an answer and return feedback; ' +
      'the loop continues within its limits.',
  },
  memory: {
    question: 'After compaction, can we still inspect the original observations?',
    choices: ['Yes — originals remain in history', 'No — the summary replaces them'],
    correct: 0,
    explain:
      'Compaction replaces the active selection. History keeps the originals, the summary, ' +
      'and the decision that connects them.',
  },
  save: {
    question: 'RetrySave reconciles a lost acknowledgment. Can the canceled context keep running?',
    choices: ['Yes — it resumes the provider call', 'No — restore a fresh context'],
    correct: 1,
    explain:
      'The exact batch is reconciled, and accepted observers can catch up. Cancellation ' +
      'still holds. A fresh restore is required to continue.',
  },
  restart: {
    question: 'A checkpoint says “tool ready.” Does that prove the refund was never sent?',
    choices: ['Yes — rerun it immediately', 'No — reconcile the external effect'],
    correct: 1,
    explain:
      'A process can fail after sending a refund but before saving its result. Tool.Recover ' +
      'can supply a completed result or allow retry. Unresolved calls give the model recovery guidance.',
  },
  search: {
    question: 'Who decides which ranked tools are shown on the next results page?',
    choices: ['The tool chain', 'The embedder'],
    correct: 0,
    explain:
      'ToolSearcher returns all names in ranked order. The search tool chain owns paging. ' +
      'Embedder only converts text and queries to vectors.',
  },
};

function contractButton(id, label) {
  return `<button type="button" class="contract-chip" data-contract="${safe(id)}"
    >${safe(label || contracts.get(id)?.name || id)} <span>↗</span></button>`;
}

function option(value, label, selected) {
  return `<option value="${value}" ${value === selected ? 'selected' : ''}>${label}</option>`;
}

function choice(action, value, label, current) {
  return `<button type="button" data-action="${action}" data-value="${value}"
    aria-pressed="${value === current}">${label}</button>`;
}

function renderRail() {
  el('lesson-rail').innerHTML = lessons
    .map(
      (lesson, index) => `
    <button type="button" data-lesson="${lesson.id}"
      ${lesson.id === lessonState.lesson ? 'aria-current="step"' : ''}>
      <span class="lesson-number">
        ${lessonState.explored.has(lesson.id) ? '✓' : `0${index + 1}`}</span>
      <span>${lesson.title}<small>${lesson.verb}</small></span>
    </button>`,
    )
    .join('');
  el('lesson-coverage').textContent =
    `${lessonState.explored.size} / 5 explored · ` +
    `all ${lessonData.interfaceCount} interfaces connected`;
}

function renderLesson() {
  renderRail();
  const lesson = lessons.find((item) => item.id === lessonState.lesson);
  const index = lessons.indexOf(lesson);
  el('lesson-body').innerHTML = `
    <header class="experiment-heading">
      <div><p class="eyebrow">EXPERIMENT 0${index + 1}</p><h2>${lesson.question}</h2></div>
      <button type="button" class="text-button" data-action="reset">Reset experiment ↺</button>
    </header>
    <div id="experiment"></div>
    <div class="lesson-bottom">
      <div class="lesson-takeaway"><span class="eyebrow">WHAT TO NOTICE</span>
        <p>${lesson.takeaway}</p></div>
      <div class="prediction" id="prediction"></div>
    </div>
    <details class="lesson-contracts">
      <summary>The ${lesson.contracts.length} interfaces behind this experiment</summary>
      <p class="small">Select a contract to connect its responsibility to what you just saw.</p>
      <div class="lesson-contract-grid">${lesson.contracts
        .map((id) => {
          const item = contracts.get(id);
          return `<div>${contractButton(id)}<p>${safe(item.purpose)}</p></div>`;
        })
        .join('')}</div>
    </details>
    <div class="lesson-next"><span class="small">Change something. Predict. Compare.</span>
      <button type="button" class="text-button"
        data-lesson="${lessons[(index + 1) % lessons.length].id}">
        ${index === 4 ? 'Back to the agent loop' : `Next: ${lessons[index + 1].title}`} →
      </button></div>`;
  ({
    loop: renderLoop,
    memory: renderMemory,
    save: renderSaveExperiment,
    restart: renderRestart,
    search: renderSearch,
  })[lesson.id]();
  renderPrediction();
}

function renderPrediction() {
  const id = lessonState.lesson;
  const item = quiz[id];
  const answer = lessonState.answers[id];
  el('prediction').innerHTML = `<span class="eyebrow">CHECK YOUR MENTAL MODEL</span>
    <p>${item.question}</p><div class="answer-choices">${item.choices
      .map(
        (label, index) =>
          `<button type="button" data-action="answer" data-value="${index}"
        aria-pressed="${answer === index}">${label}</button>`,
      )
      .join('')}</div>
    ${
      answer === undefined
        ? ''
        : `<p class="answer-feedback" role="status">
      <strong>${answer === item.correct ? 'Exactly.' : 'Try that assumption again.'}</strong>
      ${item.explain}</p>`
    }`;
}

function explore() {
  lessonState.explored.add(lessonState.lesson);
  renderRail();
}

function updateExperiment(markup) {
  // Keep a dragged range input attached. Replacing it on input would break pointer capture and
  // turn a continuous slider into a one-click control, even though keyboard tests still passed.
  const active = document.activeElement;
  if (active?.type !== 'range' || !el('experiment').contains(active)) {
    el('experiment').innerHTML = markup;
    return;
  }
  const next = document.createElement('div');
  next.innerHTML = markup;
  el('experiment')
    .querySelector('.experiment-stage')
    .replaceWith(next.querySelector('.experiment-stage'));
  for (const id of ['memory-revision-label', 'weight-label']) {
    if (el(id) && next.querySelector(`#${id}`)) {
      el(id).textContent = next.querySelector(`#${id}`).textContent;
    }
  }
  for (const button of el('experiment').querySelectorAll('.experiment-controls [data-action]')) {
    const replacement = [...next.querySelectorAll('[data-action]')].find(
      (item) =>
        item.dataset.action === button.dataset.action &&
        item.dataset.value === button.dataset.value,
    );
    if (replacement) button.disabled = replacement.disabled;
  }
}

function renderLoop() {
  const state = lessonState.loop;
  const frames = loopFrames(state.route, state.format);
  const [label, contract, title, body, output, next] = frames[state.step];
  const done = state.step === frames.length - 1;
  updateExperiment(`
    <div class="experiment-layout">
      <aside class="experiment-controls">
        <p class="control-kicker">YOU CONTROL THE MODEL’S REPLY</p>
        <p class="try-this">Try a tool call. Then make the model guess, or break its arguments.</p>
        <button type="button" class="primary-button advance" data-action="loop-next"
          ${done ? 'disabled' : ''}>${done ? 'Experiment complete ✓' : next}</button>
        <button type="button" class="text-button" data-action="loop-back"
          ${state.step === 0 ? 'disabled' : ''}>← Previous step</button>
        <label for="loop-reply">Model behavior</label>
        <select id="loop-reply">${[
          ['tool', 'Look up the order with a tool'],
          ['accepted', 'Give an acceptable answer'],
          ['rejected', 'Guess without evidence'],
          ['malformed', 'Return invalid tool arguments'],
        ]
          .map(([value, text]) => option(value, text, state.route))
          .join('')}</select>
        <label for="loop-format">Prompt format</label>
        <select id="loop-format">${option('xml', 'XML sections', state.format)}
          ${option('markdown', 'Markdown sections', state.format)}</select>
        <div class="control-note">Prompt sections change. Native calls and answer rules stay the same.
          ${contractButton('gent.TextFormat')}${contractButton('gent.TextSection')}</div>
      </aside>
      <div class="experiment-stage loop-stage">
        <div class="stage-topline"><span class="live-dot"></span> ORDER 42
          <span>STEP ${state.step + 1} / ${frames.length}</span></div>
        <div class="loop-track" aria-label="Request path">${frames
          .map(
            (frame, index) => `
          <button type="button" data-action="loop-step" data-value="${index}"
            class="flow-stop
              ${index === state.step ? 'active' : index < state.step ? 'passed' : ''}"
            aria-current="${index === state.step ? 'step' : 'false'}">
            <span class="flow-dot">${index < state.step ? '✓' : index + 1}</span>
            <span>${frame[0]}</span></button>`,
          )
          .join('')}</div>
        <div class="loop-scene" aria-live="polite">
          <div class="scene-symbol" aria-hidden="true">${
            ['↳', '⟳', '↔', '⌗', '↗', '≡', '✓'][Math.min(state.step, 6)]
          }</div>
          <div><p class="eyebrow">${safe(label)} / THE CURRENT RESPONSIBILITY</p>
            <h3 id="loop-headline">${title}</h3><p>${body}</p>
            ${contractButton(contract)}
            ${state.step === 2 ? contractButton('gent.Stream', 'Follow the stream contract') : ''}
          </div>
        </div>
        <div class="payload"><span class="eyebrow">
          ${state.step < 2 ? 'INPUT' : 'WHAT MOVES NEXT'}</span>
          <pre id="loop-output">${safe(output)}</pre></div>
        <div class="ownership-strip"><span><b>Runner</b> repeats & enforces limits</span>
          <span><b>Loop</b> decides</span>
          <span><b>ExecutionContext</b> owns evidence</span></div>
      </div>
    </div>`);
}

function renderMemory() {
  const state = lessonState.memory;
  const revision = lessonState.memoryRevision;
  const active = state.selections[revision];
  const latest = revision === state.selections.length - 1;
  const entries = state.entries;
  updateExperiment(`
    <div class="experiment-layout">
      <aside class="experiment-controls">
        <p class="control-kicker">EDIT WHAT THE NEXT MODEL SEES</p>
        <p class="try-this">Compact the prompt. Watch the history and active counts diverge.</p>
        <label class="check-control"><input id="pin-policy" type="checkbox"
          ${lessonState.pinPolicy ? 'checked' : ''}> Keep policy H1 in the selection</label>
        <div class="stack-buttons">${[
          ['window', 'Keep the last two'],
          ['summary', 'Summarize older entries'],
          ['reverse', 'Reverse the prompt order'],
        ]
          .map(
            ([value, label]) => `<button type="button" class="light-button"
          data-action="memory-edit" data-value="${value}" ${latest ? '' : 'disabled'}>
          ${label} →</button>`,
          )
          .join('')}</div>
        <label for="memory-revision">Inspect selection revision
          <strong id="memory-revision-label">${revision + 1} / ${state.selections.length}</strong>
        </label>
        <input id="memory-revision" type="range" min="0" max="${state.selections.length - 1}"
          value="${revision}" ${state.selections.length === 1 ? 'disabled' : ''}>
        <p class="small">Scrub backward to inspect earlier selections. History shows all evidence
          collected so far. Edit the latest revision to continue.</p>
        <div class="control-note">${contractButton('gent.CompactionTrigger', 'When to compact')}
          ${contractButton('gent.CompactionStrategy', 'How to compact')}</div>
      </aside>
      <div class="experiment-stage memory-stage">
        <div class="memory-columns">
          <div class="history-column"><div class="column-heading"><span>APPEND-ONLY HISTORY</span>
            <b id="history-count">${entries.length}</b></div>
            <p class="small">Everything we can still inspect.</p>
            <div class="history-records">${entries
              .map(
                (entry) => `
              <div class="history-record ${active.includes(entry.id) ? 'selected' : ''}"
                data-record="${entry.id}"><span>${entry.id}</span><div>
                <small>${entry.kind}${active.includes(entry.id) ? ' · in prompt' : ''}</small>
                <p>${safe(entry.text)}</p></div></div>`,
              )
              .join('')}</div>
          </div>
          <div class="prompt-column"><div class="column-heading"><span>NEXT MODEL PROMPT</span>
            <b id="active-count">${active.length}</b></div>
            <p class="small">Selected IDs, in this exact order.</p>
            <div class="prompt-order" id="prompt-order">${active
              .map(
                (id, index) => `
              <div class="prompt-entry"><span>${index + 1}</span><div><b>${id}</b>
                <p>${safe(entries.find((entry) => entry.id === id).text)}</p></div></div>`,
              )
              .join('')}
            </div>
            <div class="selection-code"><code id="selected-ids">[${active.join(', ')}]</code></div>
          </div>
        </div>
        <p class="experiment-status" id="memory-status" role="status">
          ${latest ? state.reason : `Inspecting selection revision ${revision + 1}.`}</p>
        <div class="under-the-hood">${contractButton('gent.ExecutionContext')}
          <span>owns the draft → HistoryUpdate proposes an edit → save acceptance records it.</span>
        </div>
      </div>
    </div>`);
}

function renderSaveExperiment() {
  const state = lessonState.save;
  const lost = state.outcome === 'lost';
  const rejected = state.outcome === 'rejected';
  const explanations = [
    'A before-model hook changes the request. The provider has not been called yet.',
    'The effective request is frozen in batch-17. Save expects the previous accepted cursor: 120.',
    'Recorder.Save receives the batch. Gent waits for its acknowledgment before delivery.',
    rejected
      ? 'The writer lease is stale. Storage rejects this batch; the provider stays stopped.'
      : lost
        ? 'Storage committed event 121, but its acknowledgment was lost. Gent still knows 120.'
        : 'Gent accepts event 121, notifies observers, then allows delivery to the provider.',
    'RetrySave sent the identical batch-17. No duplicate event was added. Gent accepts 121, ' +
      'but this canceled context remains stopped; continuation needs a fresh restore.',
  ];
  const labels = [
    'Freeze the effective request →',
    'Send batch-17 →',
    rejected
      ? 'Reject the stale writer →'
      : lost
        ? 'Commit, then lose the reply →'
        : 'Commit and acknowledge →',
    'Retry the exact same batch →',
  ];
  const canAdvance = state.step < 3 || (state.step === 3 && lost);
  const request = state.step === 0 ? 'draft: order 42' : 'frozen: order 42 + approved context';
  updateExperiment(`
    <div class="experiment-layout">
      <aside class="experiment-controls">
        <p class="control-kicker">CHOOSE WHERE THE SAVE BREAKS</p>
        <p class="try-this">Lose the reply after commit. Predict which cursors move on retry.</p>
        <label for="recorder-outcome">Recorder outcome</label>
        <select id="recorder-outcome">${[
          ['lost', 'Commit, then lose acknowledgment'],
          ['success', 'Commit and acknowledge'],
          ['rejected', 'Reject a stale writer'],
        ]
          .map(([value, label]) => option(value, label, state.outcome))
          .join('')}</select>
        <div class="batch-slip"><span class="eyebrow">THE SAVE ENVELOPE</span>
          <b>batch-17</b><dl><dt>Expected cursor</dt><dd>120</dd><dt>New event</dt><dd>121</dd>
          <dt>Save attempts</dt><dd id="save-attempts">${state.attempts}</dd></dl></div>
        <button type="button" class="primary-button advance" data-action="save-next"
          ${canAdvance ? '' : 'disabled'}>
          ${canAdvance ? labels[state.step] : 'Observe the result ✓'}
        </button>
        <div class="control-note">This is a progress save. The resumable control checkpoint stays
          at 120 while the event high-water mark can advance.
          ${contractButton('gent.Recorder')}</div>
      </aside>
      <div class="experiment-stage save-stage">
        <div class="save-cursors"><div><span>Storage cursor</span><b id="stored-cursor">
          ${state.stored}</b><small>Durable evidence</small></div>
          <div class="${state.stored !== state.accepted ? 'cursor-behind' : ''}">
            <span>Gent’s accepted cursor</span><b id="accepted-cursor">${state.accepted}</b>
            <small>What this context knows</small></div>
          <div><span>Checkpoint cursor</span><b id="checkpoint-cursor">${state.checkpoint}</b>
            <small>Where control can resume</small></div></div>
        <div class="save-sequence" aria-label="Save transaction sequence">
          <div class="sequence-node ${state.step <= 1 ? 'lit' : ''}">
            <span class="sequence-number">1</span><b>Mutable hook</b><p>${request}</p>
            ${contractButton('gent.BeforeModelCallSubscriber', 'BeforeModelCall')}</div>
          <span class="sequence-arrow" aria-hidden="true">→</span>
          <div class="sequence-node ${state.step >= 2 ? 'lit' : ''}">
            <span class="sequence-number">2</span><b>Atomic save</b>
            <p>${
              state.step < 2
                ? 'Waiting for a frozen batch'
                : state.stored === 121
                  ? 'Event 121 is stored'
                  : state.step === 3
                    ? 'Rejected: stale fence'
                    : 'Writing batch-17…'
            }</p>
            ${contractButton('gent.Recorder', 'Recorder.Save')}</div>
          <span class="sequence-arrow ${state.accepted === 121 ? '' : 'blocked'}"
            aria-hidden="true">${state.accepted === 121 ? '→' : '⊣'}</span>
          <div class="sequence-node ${state.accepted === 121 ? 'lit' : ''}">
            <span class="sequence-number">3</span><b>Accepted observer</b>
            <p id="observer-state">${
              state.observer === 'waiting' ? 'Nothing delivered' : 'Event 121 delivered once'
            }</p>
            ${contractButton('gent.RecordedEventObserver', 'ObserveCommitted')}</div>
        </div>
        <div class="provider-gate ${state.provider}"><span aria-hidden="true">
          ${state.provider === 'allowed' ? '↗' : '⊣'}</span><div><b>Provider delivery</b>
          <p id="provider-state">${
            state.provider === 'allowed'
              ? 'Allowed after acceptance'
              : state.provider === 'stopped'
                ? 'Stopped — context canceled'
                : 'Waiting — the model has not been called'
          }</p></div></div>
        <p class="experiment-status" id="save-status" role="status">${explanations[state.step]}</p>
        <div class="under-the-hood">${contractButton('gent.StateChangeEventSubscriber')}
          <span>and recorded observers see accepted evidence. Before hooks can change inputs.</span>
        </div>
      </div>
    </div>`);
}

function renderRestart() {
  const state = lessonState.recovery;
  const options = lessonState.recoveryOptions;
  const resolved = [
    'saved result reused',
    'reconciled with receipt',
    'retry explicitly allowed',
  ].includes(state.status);
  const descriptions = {
    interrupted:
      'Worker A disappeared after saving a tool intent. Restore a new worker to inspect it.',
    'incompatible version':
      'RecoveryConfig does not match the saved behavior version. Restore stops ' +
      'before any tool runs. Switch back to v1 to continue.',
    'missing value codec':
      'The saved typed value cannot be reconstructed. Register its ValueCodec ' +
      'before continuing; a JSON-shaped lookalike is not the same type.',
    'unknown outcome; model receives recovery guidance':
      'The checkpoint proves intent, not outcome. Gent saves an unknown-outcome result with ' +
      'recovery guidance. The model can check the refund service before making a new plan.',
    'saved result reused':
      'The durable result already proves completion. Gent reuses it without ' +
      'calling Tool.Recover or repeating the external effect.',
    'no external receipt to return':
      'This scenario has no completed external refund to reconcile. ' +
      'A completed status must carry the full real result; it cannot invent success.',
    'reconciled with receipt':
      'Tool.Recover returned completed with the full receipt. The result ' +
      'is accepted and the tool is not executed again.',
    'retry explicitly allowed': options.idempotent
      ? 'Tool.Recover allowed retry. The external service deduplicates the stable logical ' +
        'call ID. The new execution attempt does not create another refund.'
      : 'Tool.Recover explicitly allowed retry, but the service does not deduplicate. A previous ' +
        'refund can be sent twice. Gent does not promise exactly-once external effects.',
  };
  updateExperiment(`
    <div class="experiment-layout">
      <aside class="experiment-controls">
        <p class="control-kicker">CRASH BETWEEN INTENT AND EVIDENCE</p>
        <p class="try-this">Restore after the refund. Retry with service deduplication off.</p>
        <label for="crash-point">Worker A crashes…</label>
        <select id="crash-point">${[
          ['before-effect', 'Before sending the refund'],
          ['after-effect', 'After refund, before save'],
          ['after-result', 'After the result was saved'],
        ]
          .map(([value, label]) => option(value, label, state.point))
          .join('')}</select>
        <label class="check-control"><input id="idempotent-service" type="checkbox"
          ${options.idempotent ? 'checked' : ''}> Service deduplicates logical call ID</label>
        <details class="compatibility-controls"><summary>Change restore compatibility</summary>
          <label for="recovery-version">Application behavior version</label>
          <select id="recovery-version">${option('v1', 'v1 — matches saved run', options.version)}
            ${option('v2', 'v2 — incompatible change', options.version)}</select>
          <label class="check-control"><input id="receipt-codec" type="checkbox"
            ${options.codec ? 'checked' : ''}> Receipt ValueCodec registered</label>
        </details>
        <button type="button" class="primary-button advance" data-action="restore"
          ${resolved ? 'disabled' : ''}>Restore worker B →</button>
        <div class="control-note">The logical call ID survives. The worker attempt changes.
          ${contractButton('gent.ToolChain')}
          ${contractButton('gent.RecoveryConfig')}</div>
      </aside>
      <div class="experiment-stage restart-stage">
        <div class="restart-scene">
          <div class="worker old"><span class="worker-icon" aria-hidden="true">×</span>
            <b>Worker A</b><small>Interrupted</small></div>
          <div class="handoff"><span>same logical call</span><code>${state.call}</code>
            <span class="handoff-arrow" aria-hidden="true">╌╌╌╌╌╌→</span>
            <small id="attempt-id">attempt: ${state.attempt}</small></div>
          <div class="worker ${state.restored ? 'live' : 'waiting'}">
            <span class="worker-icon" aria-hidden="true">${state.restored ? '↗' : '?'}</span>
            <b>Worker B</b><small>${state.restored ? 'Restored' : 'Not restored'}</small></div>
        </div>
        <div class="recovery-evidence">
          <div><span class="eyebrow">WHAT GENT HAS SAVED</span>
            <b>${
              state.point === 'after-result'
                ? 'tool_completed + full result'
                : 'tool_ready + intent'
            }</b>
            <p>${
              state.point === 'after-result'
                ? 'A result is durable.'
                : 'Input and logical call are durable. There is no accepted result.'
            }</p></div>
          <div class="effect-count ${state.effects > 1 ? 'duplicate' : ''}">
            <span class="eyebrow">EXTERNAL REFUNDS</span>
            <strong id="refund-count">${state.effects}</strong>
            <small>${state.effects > 1 ? 'Duplicate effect' : 'Visible to the experimenter'}</small>
          </div>
        </div>
        <div class="recovery-actions"><span class="eyebrow">YOU ARE THE TOOL RECOVERY ADAPTER</span>
          <div>${[
            ['completed', 'Return completed + receipt'],
            ['retry', 'Explicitly allow retry'],
            ['unresolved', 'Leave unresolved'],
          ]
            .map(
              ([action, label]) => `
            <button type="button" class="light-button" data-action="recover" data-value="${action}"
              ${!state.restored || resolved ? 'disabled' : ''}>${label}</button>`,
            )
            .join('')}</div></div>
        <p class="recovery-result" id="recovery-result" role="status">${safe(state.status)}</p>
        <p class="experiment-status" id="recovery-status">${descriptions[state.status]}</p>
        <div class="under-the-hood">${contractButton('gent.Tool')}
          <span>reconciles effects.
            ${contractButton('gent.CheckpointState')} restores local state.</span>
        </div>
      </div>
    </div>`);
}

function renderSearch() {
  const state = lessonState.search;
  const ranked = rankTools(state.query, state.mode, state.weight);
  const current = ranked[state.page];
  updateExperiment(`
    <div class="experiment-layout">
      <aside class="experiment-controls">
        <p class="control-kicker">SAY THE SAME THING DIFFERENTLY</p>
        <p class="try-this">“Money back” has no keyword match here. Try semantic search.</p>
        <label for="search-query">User request</label>
        <select id="search-query">${Object.entries(searchFixtures)
          .map(([value, fixture]) => option(value, fixture.label, state.query))
          .join('')}</select>
        <label>Retrieval strategy</label><div class="choice-stack">${[
          ['keyword', 'Exact terms'],
          ['semantic', 'Similar meaning'],
          ['fused', 'Blend both'],
        ]
          .map(([value, label]) => choice('search-mode', value, label, state.mode))
          .join('')}</div>
        <label for="semantic-weight">Semantic weight <strong id="weight-label">
          ${Math.round(state.weight * 100)}%</strong></label>
        <input id="semantic-weight" type="range" min="0" max="100" step="10"
          value="${state.weight * 100}" ${state.mode === 'fused' ? '' : 'disabled'}>
        <p class="small">Blend = keyword × (1 − weight) + semantic × weight.
          These are illustrative normalized scores, not measured BM25 or embedding outputs.</p>
        <div class="control-note">${contractButton('search.Embedder', 'Meaning → vectors')}
          ${contractButton('search.Fuser', 'Scores → one ranking')}</div>
      </aside>
      <div class="experiment-stage search-stage">
        <div class="query-banner"><span class="eyebrow">QUERY</span>
          <h3>“${searchFixtures[state.query].label}”</h3></div>
        <div class="retrieval-path"><div>${contractButton('gent.IndexableTool', 'Describe tools')}
          <span>name, description, schema</span></div><b aria-hidden="true">→</b>
          <div>${contractButton('search.SearchIndex', 'Retrieve candidates')}
            <span>${
              state.mode === 'keyword'
                ? 'term relevance'
                : state.mode === 'semantic'
                  ? 'vector similarity'
                  : 'two candidate lists'
            }</span></div><b aria-hidden="true">→</b>
          <div>${contractButton('gent.ToolSearcher', 'Return ranked names')}
            <span>all matches, before paging</span></div></div>
        <div class="rank-heading"><span>RANKED CANDIDATES</span>
          <span>KEYWORD / MEANING / FINAL</span></div>
        <div class="ranking" id="ranking">${
          ranked.length
            ? ranked
                .map(
                  (tool, index) => `
          <div class="ranked-tool" data-tool="${tool.id}">
            <span class="rank-position">0${index + 1}</span><div><b>${tool.id}</b>
              <p>${tool.description}</p><div class="score-track"><span
                style="width:${Math.round(tool.score * 100)}%"></span></div></div>
            <div class="score-numbers"><span>${tool.keyword.toFixed(2)}</span>
              <span>${tool.semantic.toFixed(2)}</span><b>${tool.score.toFixed(2)}</b></div>
          </div>`,
                )
                .join('')
            : `<div class="no-matches"><span aria-hidden="true">∅</span>
            <h3>No keyword candidates.</h3>
            <p>Try “Similar meaning” to connect this phrase to refund_order.
            </p></div>`
        }</div>
        <div class="tool-page"><div><span class="eyebrow">THE TOOL CHAIN’S RESULTS PAGE</span>
          <b id="tool-page-name">${current ? current.id : 'No tools to show'}</b>
          <span class="small">One tool per page in this experiment.</span></div>
          <div><button type="button" class="text-button" data-action="search-page" data-value="-1"
              ${state.page === 0 ? 'disabled' : ''} aria-label="Previous tool page">←</button>
            <span id="tool-page-number">
              ${ranked.length ? state.page + 1 : 0} / ${ranked.length}</span>
            <button type="button" class="text-button" data-action="search-page" data-value="1"
              ${state.page >= ranked.length - 1 ? 'disabled' : ''}
              aria-label="Next tool page">→</button></div></div>
        <details class="adapter-note"><summary>Why so many search interfaces?</summary>
          <p>An adapter describes your data; an index retrieves it. This keeps the generic search
            package independent of tools. ChunkAdapter supplies embedding chunks; BleveAdapter
            supplies indexed fields. TokenCounter bounds chunks, and BleveIDFProvider can supply
            statistics for score normalization.</p>
          <div>${[
            'search.ChunkAdapter',
            'search.BleveAdapter',
            'search.TokenCounter',
            'search.BleveIDFProvider',
          ]
            .map((id) => contractButton(id))
            .join('')}</div></details>
      </div>
    </div>`);
}

function showContract(id) {
  const item = contracts.get(id);
  if (!item) throw new Error(`Unknown lesson contract: ${id}`);
  const rules = item.rules.map((rule) => `<li>${safe(rule)}</li>`).join('');
  el('lesson-contract-content').innerHTML = `<h2 id="lesson-contract-title">${safe(item.name)}</h2>
    <p class="contract-purpose">${safe(item.purpose)}</p><p>${safe(item.why)}</p>
    ${rules ? `<ul>${rules}</ul>` : ''}
    <details><summary>See the exact Go contract</summary><pre><code id="lesson-contract-code">
      ${safe(item.code)}</code></pre>
      <span class="small">${safe(item.file)}:${item.line}</span></details>
    <div class="contract-dialog-actions"><button type="button" class="primary-button"
      data-open="${safe(id)}" data-close-contract>
        Open reference & leave a review note →</button></div>`;
  el('lesson-contract').showModal();
}

function resetExperiment() {
  switch (lessonState.lesson) {
    case 'loop':
      lessonState.loop.step = 0;
      break;
    case 'memory':
      lessonState.memory = newMemory();
      lessonState.memoryRevision = 0;
      break;
    case 'save':
      lessonState.save = newSave(lessonState.save.outcome);
      break;
    case 'restart':
      lessonState.recovery = newRecovery(lessonState.recovery.point);
      break;
    case 'search':
      lessonState.search = {
        query: 'meaning',
        mode: 'keyword',
        weight: 0.7,
        page: 0,
      };
      break;
  }
  renderLesson();
}

// Re-render only the experiment, not the lesson rail or the page, after a local interaction.
// Restore focus to a surviving control so keyboard users can step repeatedly through the lesson.
function withFocus(render) {
  const active = document.activeElement;
  const id = active?.id;
  const action = active?.dataset.action;
  const value = active?.dataset.value;
  render();
  const candidates = action ? [...el('experiment').querySelectorAll('[data-action]')] : [];
  const target = id
    ? el(id)
    : candidates.find(
        (button) => button.dataset.action === action && button.dataset.value === value,
      );
  if (target && !target.disabled) target.focus({ preventScroll: true });
}

document.addEventListener('click', (event) => {
  const button = event.target.closest('button');
  if (!button) return;
  if (button.dataset.contract) showContract(button.dataset.contract);
  if (button.hasAttribute('data-close-contract')) el('lesson-contract').close();
  if (button.dataset.lesson) {
    lessonState.lesson = button.dataset.lesson;
    const params = new URLSearchParams({
      view: 'learn',
      lesson: lessonState.lesson,
    });
    history.replaceState(null, '', `#${params}`);
    renderLesson();
    el('lesson-rail').querySelector(`[data-lesson="${lessonState.lesson}"]`).focus({
      preventScroll: true,
    });
  }
  const action = button.dataset.action;
  const value = button.dataset.value;
  if (!action) return;
  if (action === 'reset') return resetExperiment();
  if (action === 'answer') {
    lessonState.answers[lessonState.lesson] = Number(value);
    renderPrediction();
    el('prediction').querySelector(`[data-value="${value}"]`).focus({ preventScroll: true });
    return;
  }
  if (action.startsWith('loop-')) {
    const frames = loopFrames(lessonState.loop.route, lessonState.loop.format);
    const step = lessonState.loop.step;
    lessonState.loop.step =
      action === 'loop-step'
        ? Number(value)
        : Math.min(frames.length - 1, Math.max(0, step + (action === 'loop-next' ? 1 : -1)));
    withFocus(renderLoop);
  }
  if (action === 'memory-edit') {
    lessonState.memory = changeMemory(lessonState.memory, value, lessonState.pinPolicy);
    lessonState.memoryRevision = lessonState.memory.selections.length - 1;
    withFocus(renderMemory);
  }
  if (action === 'save-next') {
    lessonState.save = advanceSave(lessonState.save);
    withFocus(renderSaveExperiment);
  }
  if (action === 'restore' || action === 'recover') {
    lessonState.recovery = recoverWorker(
      lessonState.recovery,
      action === 'restore' ? 'restore' : value,
      lessonState.recoveryOptions,
    );
    withFocus(renderRestart);
  }
  if (action === 'search-mode') {
    lessonState.search.mode = value;
    lessonState.search.page = 0;
    withFocus(renderSearch);
  }
  if (action === 'search-page') {
    lessonState.search.page += Number(value);
    withFocus(renderSearch);
  }
  explore();
});

el('lesson-body').addEventListener('change', (event) => {
  const { id, value, checked } = event.target;
  if (id === 'loop-reply' || id === 'loop-format') {
    lessonState.loop[id === 'loop-reply' ? 'route' : 'format'] = value;
    lessonState.loop.step = 0;
    withFocus(renderLoop);
  }
  if (id === 'pin-policy') lessonState.pinPolicy = checked;
  if (id === 'recorder-outcome') {
    lessonState.save = newSave(value);
    withFocus(renderSaveExperiment);
  }
  if (['crash-point', 'recovery-version', 'receipt-codec', 'idempotent-service'].includes(id)) {
    if (id === 'recovery-version') lessonState.recoveryOptions.version = value;
    if (id === 'receipt-codec') lessonState.recoveryOptions.codec = checked;
    if (id === 'idempotent-service') lessonState.recoveryOptions.idempotent = checked;
    lessonState.recovery = newRecovery(id === 'crash-point' ? value : lessonState.recovery.point);
    withFocus(renderRestart);
  }
  if (id === 'search-query') {
    lessonState.search.query = value;
    lessonState.search.page = 0;
    withFocus(renderSearch);
  }
});
el('lesson-body').addEventListener('input', (event) => {
  if (event.target.id === 'memory-revision') {
    lessonState.memoryRevision = Number(event.target.value);
    withFocus(renderMemory);
  }
  if (event.target.id === 'semantic-weight') {
    lessonState.search.weight = Number(event.target.value) / 100;
    lessonState.search.page = 0;
    withFocus(renderSearch);
    explore();
  }
});
el('close-lesson-contract').addEventListener('click', () => el('lesson-contract').close());

function readLessonRoute() {
  const requested = new URLSearchParams(location.hash.slice(1)).get('lesson');
  if (lessons.some((lesson) => lesson.id === requested)) lessonState.lesson = requested;
  renderLesson();
}
window.addEventListener('hashchange', readLessonRoute);
window.addEventListener('popstate', readLessonRoute);
readLessonRoute();

// Bundled after simulations.mjs inside a private closure by build.mjs.
const data = JSON.parse(document.getElementById('guide-data').textContent);
const byId = (id) => document.getElementById(id);
const all = (selector) => [...document.querySelectorAll(selector)];
const prose = (text) => text.replace(/\s+/g, ' ').trim();
const escapeHTML = (value) =>
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
const write = (id, value) => {
  byId(id).textContent = value;
};
const componentByID = new Map(data.components.map((component) => [component.id, component]));

function highlightGo(code) {
  const tokens = new RegExp(
    [
      /\/\/[^\n]*/.source,
      /"(?:\\.|[^"\\])*"/.source,
      /\b(?:type|interface|struct|func|return|if|nil|defer|error|const|var|for|range|true|false)\b/
        .source,
    ].join('|'),
    'g',
  );
  let result = '';
  let last = 0;
  for (const match of code.matchAll(tokens)) {
    result += escapeHTML(code.slice(last, match.index));
    const kind = match[0].startsWith('//')
      ? 'comment'
      : match[0].startsWith('"')
        ? 'string'
        : 'keyword';
    result += `<span class="code-${kind}">${escapeHTML(match[0])}</span>`;
    last = match.index + match[0].length;
  }
  return result + escapeHTML(code.slice(last));
}

const dialog = byId('detail-dialog');
let dialogOrigin;

function openDialog(eyebrow, html) {
  if (!dialog.open) dialogOrigin = document.activeElement;
  write('dialog-eyebrow', eyebrow);
  byId('dialog-content').innerHTML = html;
  write('dialog-current', byId('dialog-title').textContent);
  if (!dialog.open) dialog.showModal();
  document.body.style.overflow = 'hidden';
  dialog.scrollTop = 0;
  byId('close-dialog').focus({ preventScroll: true });
}

dialog.addEventListener('close', () => {
  document.body.style.overflow = '';
  if (dialogOrigin?.isConnected) dialogOrigin.focus({ preventScroll: true });
});
byId('close-dialog').addEventListener('click', () => dialog.close());
dialog.addEventListener('click', (event) => {
  const box = dialog.getBoundingClientRect();
  if (
    event.clientX < box.left ||
    event.clientX > box.right ||
    event.clientY < box.top ||
    event.clientY > box.bottom
  )
    dialog.close();
});

function inspectComponent(id) {
  const item = componentByID.get(id);
  if (!item) throw new Error(`Unknown component ${id}`);
  openDialog(
    `${item.layer} / ${item.kind}`,
    `
    <h2 id="dialog-title">${escapeHTML(item.name)}</h2>
    <p class="dialog-label">${escapeHTML(item.label)}</p>
    <div class="dialog-prose">
      <p>${escapeHTML(prose(item.job))}</p>
      <span class="small-label">WHY THIS JOB EXISTS</span>
      <p>${escapeHTML(prose(item.why))}</p>
      <span class="small-label">IN THIS REPOSITORY</span>
      <p>${escapeHTML(prose(item.implementations))}</p>
    </div>
    <pre class="dialog-code"><code>${highlightGo(item.code)}</code></pre>
    <p class="source-path">${escapeHTML(item.source)} · ${
      item.extracted
        ? 'Declaration extracted from this checkout; comments omitted here.'
        : 'Selected public API / usage sketch.'
    }</p>
    <div class="dialog-prose"><span class="small-label">CONNECTED PARTS</span></div>
    <div class="related-links">${item.links
      .map(
        (link) => `
      <button data-inspect="${link}">${escapeHTML(componentByID.get(link).name)}</button>
    `,
      )
      .join('')}</div>
  `,
  );
}

function renderMap(id) {
  const item = componentByID.get(id);
  all('[data-map]').forEach((button) => {
    const selected = button.dataset.map === id;
    button.classList.toggle('selected', selected);
    button.setAttribute('aria-pressed', String(selected));
  });
  byId('map-detail').innerHTML = `
    <div class="detail-heading"><h3>${escapeHTML(item.name)}</h3>
      <button class="text-link" data-inspect="${id}">Open the Go contract ↗</button></div>
    <p class="detail-label">${escapeHTML(item.kind.toUpperCase())} / ${escapeHTML(item.label)}</p>
    <p>${escapeHTML(prose(item.job))} ${escapeHTML(prose(item.why))}</p>
    <p class="collaborators">CONNECTED TO ${item.links
      .map(
        (link) => `
      <button data-inspect="${link}">${escapeHTML(componentByID.get(link).name)}</button>
    `,
      )
      .join(' · ')}</p>
  `;
}

let runIndex = 0;
let playTimer;
let motionPaused = matchMedia('(prefers-reduced-motion: reduce)').matches;

function renderRun() {
  const step = data.steps[runIndex];
  write('run-step-number', String(runIndex + 1).padStart(2, '0'));
  write('run-step-title', step.title);
  write('run-step-text', prose(step.text));
  write('run-step-note', prose(step.note));
  write('run-iteration', step.iteration);
  write('run-tokens', step.tokens);
  write('run-tools', step.tools);
  write('run-phase', step.phase);
  write('run-activity', step.activity);
  write('run-count', `${runIndex + 1} / ${data.steps.length}`);
  byId('run-slider').value = runIndex;
  byId('run-back').disabled = runIndex === 0;
  byId('run-next').disabled = runIndex === data.steps.length - 1;
  const active = { context: 'model', format: 'agent', history: 'agent', journal: 'executor' };
  all('#run-nodes button').forEach((button) => {
    button.classList.toggle(
      'active',
      button.dataset.inspect === (active[step.active] || step.active),
    );
  });
  byId('run-records').innerHTML = step.entries.length
    ? step.entries.map((entry) => `<div class="run-record">${escapeHTML(entry)}</div>`).join('')
    : '<div class="empty-record">No observations yet. The original task is kept separately.</div>';
}

function stopPlayback() {
  clearInterval(playTimer);
  playTimer = undefined;
  byId('run-play').setAttribute('aria-pressed', 'false');
  byId('run-play').querySelector('span').textContent = 'Play the run';
}

function setRun(index) {
  runIndex = Math.max(0, Math.min(data.steps.length - 1, index));
  renderRun();
  if (runIndex === data.steps.length - 1) stopPlayback();
}

byId('run-play').addEventListener('click', () => {
  if (playTimer) return stopPlayback();
  if (runIndex === data.steps.length - 1) setRun(0);
  byId('run-play').setAttribute('aria-pressed', 'true');
  byId('run-play').querySelector('span').textContent = 'Pause the run';
  // Leave time to read the explanation; manual stepping is always available.
  playTimer = setInterval(() => setRun(runIndex + 1), 9000);
});
byId('run-back').addEventListener('click', () => {
  stopPlayback();
  setRun(runIndex - 1);
});
byId('run-next').addEventListener('click', () => {
  stopPlayback();
  setRun(runIndex + 1);
});
byId('run-slider').addEventListener('input', (event) => {
  stopPlayback();
  setRun(Number(event.target.value));
});
document.addEventListener('visibilitychange', () => {
  if (document.hidden) stopPlayback();
});

function renderMotion() {
  document.documentElement.classList.toggle('motion-paused', motionPaused);
  document.documentElement.classList.toggle('js-motion', !motionPaused);
  byId('motion-toggle').setAttribute('aria-pressed', String(motionPaused));
  write('motion-label', motionPaused ? 'Resume motion' : 'Pause motion');
  byId('run-play').disabled = motionPaused;
  byId('run-play').title = motionPaused
    ? 'Resume motion above to enable playback; steps work anytime.'
    : '';
  if (motionPaused) stopPlayback();
}
byId('motion-toggle').addEventListener('click', () => {
  motionPaused = !motionPaused;
  renderMotion();
});
matchMedia('(prefers-reduced-motion: reduce)').addEventListener('change', (event) => {
  motionPaused = event.matches;
  renderMotion();
});

let responsePreset = 'tool';
function renderParser() {
  const xml = byId('format-select').value === 'xml';
  let output;
  let title;
  let explanation;
  let route;
  if (responsePreset === 'tool') {
    output = xml
      ? `<thought>Check the policy first.</thought>
<action>
- tool: policy
  args:
    id: P10
</action>`
      : `# thought
Check the policy first.

# action
- tool: policy
  args:
    id: P10`;
    title = 'A proposal becomes a call.';
    route = 'FORMAT → TOOLCHAIN → TOOL';
    explanation = `TextFormat extracts the action section. The YAML toolchain parses the call,
      validates its arguments, and prepares typed input. A saved tool-ready boundary precedes
      Tool.Call. The full returned result then becomes evidence and a model observation.`;
  } else if (responsePreset === 'answer') {
    output = xml
      ? `<thought>The policy has the answer.</thought>
<answer>
Refunds are allowed within 14 days.
</answer>`
      : `# thought
The policy has the answer.

# answer
Refunds are allowed within 14 days.`;
    title = 'An answer still needs acceptance.';
    route = 'FORMAT → TERMINATION → VALIDATOR';
    explanation = `TextFormat extracts the answer. Termination parses it and asks its optional
      AnswerValidator to evaluate it. An accepted answer ends the loop. A rejection is saved as
      feedback, giving the agent a reason to try another iteration.`;
  } else {
    output = 'I would probably ask someone about refunds.';
    title = 'No actionable section.';
    route = 'FORMAT → ERROR / FEEDBACK';
    explanation = `This example has neither a thought nor an action or answer section in the
      configured envelope. It cannot become a tool call or accepted answer. Parse errors are
      observable and counted. ReAct can supply corrective feedback, subject to configured limits.`;
  }
  write('parser-output', output);
  byId('parser-explanation').dataset.tone = responsePreset === 'invalid' ? 'warning' : 'success';
  byId('parser-explanation').innerHTML = `
    <span class="small-label">${route}</span><h3>${title}</h3>
    <p>${escapeHTML(prose(explanation))}</p>`;
  all('[data-response]').forEach((button) => {
    button.setAttribute('aria-pressed', String(button.dataset.response === responsePreset));
  });
}
byId('format-select').addEventListener('change', renderParser);

let historyState = createHistory();
function renderHistory() {
  const revision = historyState.revision;
  const selection = contextAt(historyState, revision);
  const activeIDs = new Set(selection.map((entry) => entry.id));
  write('history-revision', `${revision} / ${historyState.selections.length}`);
  byId('history-ledger').innerHTML = historyState.entries
    .map(
      (entry) => `
    <button class="history-entry ${entry.kind === 'Compaction' ? 'marker' : ''}
      ${activeIDs.has(entry.id) ? 'in-context' : ''}"
      data-entry="${entry.id}" aria-label="Inspect ${escapeHTML(entry.title)}">
      <span class="entry-id">${entry.id}</span><span><strong>${escapeHTML(entry.title)}</strong>
      <small>${entry.kind} · immutable</small>
      ${activeIDs.has(entry.id) ? '<span class="entry-selection">In context</span>' : ''}
      </span><span class="entry-arrow">↗</span></button>
  `,
    )
    .join('');
  byId('history-selection').innerHTML = selection
    .map(
      (entry) => `
    <div class="context-sheet"><span class="small-label">${entry.id} / ${entry.kind}</span>
      <p>${escapeHTML(prose(entry.text))}</p></div>`,
    )
    .join('');
  const explanations = [
    `Revision 1 selects the original request and complete policy observation. Later summaries do not
      change this earlier selection.`,
    `Revision 2 selects S1. It preserves the opened-product exception. A, B, and the compaction
      decision remain in history.`,
    `Revision 3 selects S2. The exception is missing from this shorter summary. Open B to recover
      the original evidence, or move the revision slider back.`,
  ];
  write('history-explanation', prose(explanations[revision - 1]));
  byId('history-slider').max = historyState.selections.length;
  byId('history-slider').value = revision;
  byId('history-compact').disabled = historyState.selections.length === 3;
  write(
    'history-compact',
    ['Compact once', 'Compact again', 'Two summaries saved'][historyState.selections.length - 1],
  );
}
byId('history-compact').addEventListener('click', () => {
  historyState = compactHistory(historyState);
  renderHistory();
});
byId('history-reset').addEventListener('click', () => {
  historyState = createHistory();
  renderHistory();
});
byId('history-slider').addEventListener('input', (event) => {
  historyState = { ...historyState, revision: Number(event.target.value) };
  renderHistory();
});

function inspectEntry(id) {
  const entry = historyState.entries.find((item) => item.id === id);
  openDialog(
    `History entry ${id} / ${entry.kind}`,
    `
    <h2 id="dialog-title">${escapeHTML(entry.title)}</h2>
    <p class="dialog-label">An immutable piece of the story.</p>
    <div class="dialog-prose"><p>${escapeHTML(prose(entry.text))}</p>
      <span class="small-label">PROVENANCE</span><p>${escapeHTML(prose(entry.source))}</p>
      <p>Inspecting an older entry does not alter the current selection. Gent keeps those concerns
        separate so a concise prompt can still be explained by its full original evidence.</p></div>
    <div class="related-links"><button data-inspect="entry">HistoryEntry ↗</button>
      <button data-inspect="compaction">CompactionStrategy ↗</button></div>
  `,
  );
}

let journalState = createJournal();

// Name each outcome before the explanation so uncertainty is visible without reading every line.
function showStatus(id, title, copy, tone = 'neutral') {
  const element = byId(id);
  element.dataset.tone = tone;
  element.innerHTML = `<strong class="status-heading">${escapeHTML(title)}</strong>
    <p>${escapeHTML(prose(copy))}</p>`;
}

function journalStatus() {
  const { phase, mode } = journalState;
  if (phase < 3) return { title: ['Ready to queue', 'Batch prepared', 'Number frozen'][phase] };
  if (phase === 4) return { title: 'Reconciled · restore before continuing', tone: 'warning' };
  if (mode === 'lost') return { title: 'Acknowledgment missing', tone: 'warning' };
  if (mode === 'rejected') return { title: 'Writer rejected', tone: 'warning' };
  return { title: 'Batch accepted', tone: 'success' };
}

function renderJournal() {
  const view = journalView(journalState);
  const status = journalStatus();
  write('journal-accepted', journalState.accepted);
  write('journal-stored', journalState.stored);
  write('journal-provider', view.provider);
  showStatus('journal-explanation', status.title, view.copy, status.tone);
  write('journal-next', view.next);
  byId('journal-next').disabled = Boolean(view.done);
  write(
    'journal-provider-label',
    view.provider === 'Allowed'
      ? 'Accepted request may execute'
      : view.provider === 'Stopped'
        ? 'This execution cannot advance'
        : 'No side effect yet',
  );
  write('journal-accepted-label', 'Export and accepted observers');
  write(
    'journal-store-label',
    journalState.stored > journalState.accepted
      ? 'Committed; acknowledgment missing'
      : 'Committed in storage',
  );
  write(
    'journal-batch-id',
    journalState.phase === 0
      ? 'NEXT EVENT 121 · CHECKPOINT STAYS AT 120'
      : `${journalState.batchID} · EVENT 121 · SAVE CALLS ${journalState.saveCalls}`,
  );
  all('[data-transaction]').forEach((node) => {
    node.classList.toggle('active', Number(node.dataset.transaction) === view.rail);
    node.classList.toggle('done', Number(node.dataset.transaction) < view.rail);
  });
  all('[data-journal-mode]').forEach((button) => {
    button.setAttribute('aria-pressed', String(button.dataset.journalMode === journalState.mode));
  });
}
byId('journal-next').addEventListener('click', () => {
  journalState = advanceJournal(journalState);
  renderJournal();
});
byId('journal-reset').addEventListener('click', () => {
  journalState = createJournal(journalState.mode);
  renderJournal();
});

let recoveryPhase = 0;
let recoveryDecision;
function renderRecovery() {
  const scenario = byId('recovery-scenario').value;
  const terminal = scenario === 'terminal';
  const tool = scenario === 'tool';
  const restored = recoveryPhase >= 2;
  const finished = recoveryPhase >= 3 || (restored && terminal);
  let copy;
  if (recoveryPhase === 0) {
    const boundaries = {
      partial: 'Its model call is still streaming. Accepted partial chunks end at 128.',
      saved: `Its successful model response is already accepted through 128, before a decision
        checkpoint.`,
      tool: `The tool-ready checkpoint exists. The external operation may have happened; no result
        is saved.`,
      terminal: 'The terminal checkpoint and final result are already saved at 128.',
    };
    copy = `Worker A owns run refund-42. ${boundaries[scenario]}
      Stop the worker to see what survives.`;
  } else if (recoveryPhase === 1) {
    copy = terminal
      ? `Worker A is gone. The terminal outcome is still stored. Worker B can load
      and return that outcome without starting a new attempt.`
      : `Worker A is gone. Its live Go
      stack and provider connection are gone too. The saved run, accepted accounting, logical IDs,
      and checkpoint remain available to a replacement with compatible bindings.`;
  } else if (terminal) {
    copy = `Worker B restores the terminal bundle and returns its saved output or error. There is
      no new model call, tool call, or execution attempt. The accepted cursor stays at 128.`;
  } else if (recoveryPhase === 2) {
    copy = tool
      ? `Worker B restores the same run. Tool-ready state cannot prove the external
      outcome. The default is ErrUncertainTool. The application must reconcile the logical tool
      call and supply a ToolRecovery decision before that work can proceed.`
      : `Worker B imports
      the full bundle through 128 and resumes the supported checkpoint. The next event is 129,
      not 121. Run, context, and logical call IDs survive; physical attempt a becomes attempt b.`;
  } else if (tool) {
    const decisions = {
      completed: `The application verified completion and supplied the complete saved result.
        Gent uses that result without executing the external operation again.`,
      retry: `The application verified that a retry is safe. Gent may attempt the tool again using
        its stable logical identity. External idempotency or reconciliation remains
        the application’s job.`,
      unresolved: `The outcome is still unknown. ErrUncertainTool stops continuation before another
        external call. No automatic retry and no claimed success.`,
    };
    copy = decisions[recoveryDecision];
  } else if (scenario === 'saved') {
    copy = `The completed successful response is reused to continue the decision. No new provider
      call is needed for that logical request. Its known usage remains charged once. Later steps
      can append new evidence after the saved cursor.`;
  } else {
    copy = `The partial response cannot serve as a completed model decision. A new physical model
      attempt runs for the logical call; accepted chunks from the earlier attempt stay visible.
      Any previously known usage remains charged, and new work adds new usage.`;
  }
  const status = recoveryStatus(scenario);
  showStatus('recovery-copy', status.title, copy, status.tone);
  write('recovery-cursor', restored && !terminal ? '129+' : '128');
  write('recovery-checkpoint', terminal ? '128' : '120');
  write('worker-a-state', recoveryPhase === 0 ? (terminal ? 'Finished' : 'Running') : 'Stopped');
  write(
    'worker-b-state',
    !restored
      ? 'Not started'
      : terminal
        ? 'Returned saved result'
        : recoveryDecision === 'unresolved'
          ? 'Blocked: uncertain tool'
          : finished
            ? tool
              ? 'Decision applied'
              : 'Continued'
            : tool
              ? 'Needs reconciliation'
              : 'Restored',
  );
  byId('worker-a').className = `worker ${recoveryPhase === 0 ? 'active' : 'stopped'}`;
  byId('worker-b').className = `worker ${
    !restored ? 'waiting' : recoveryDecision === 'unresolved' ? 'stopped' : 'active'
  }`;
  byId('tool-recovery-actions').hidden = !(tool && recoveryPhase === 2);
  write(
    'recovery-next',
    recoveryPhase === 0
      ? 'Stop worker A →'
      : recoveryPhase === 1
        ? 'Restore worker B →'
        : finished
          ? terminal
            ? 'Saved outcome returned'
            : 'Scenario complete'
          : tool
            ? 'Choose a recovery decision above'
            : 'Continue from saved state →',
  );
  byId('recovery-next').disabled = finished || (tool && recoveryPhase === 2);
  write(
    'recovery-identity',
    `CONTEXT root · ${restored && !terminal ? 'ATTEMPT a → b · LOGICAL IDs KEPT' : 'ATTEMPT a'}`,
  );
}
function recoveryStatus(scenario) {
  if (recoveryPhase === 0) {
    return { title: scenario === 'terminal' ? 'Run already finished' : 'Worker A is running' };
  }
  if (recoveryPhase === 1) return { title: 'The saved evidence remains' };
  if (scenario === 'terminal') return { title: 'Saved outcome returned', tone: 'success' };
  if (scenario === 'tool') {
    const outcomes = {
      completed: { title: 'Completed result reused', tone: 'success' },
      retry: { title: 'Retry allowed by the application', tone: 'success' },
      unresolved: { title: 'Unresolved · execution stopped', tone: 'warning' },
    };
    return outcomes[recoveryDecision] || { title: 'Reconciliation required', tone: 'warning' };
  }
  if (recoveryPhase === 2) return { title: 'Worker B restored', tone: 'success' };
  return {
    title: scenario === 'saved' ? 'Saved response reused' : 'A new model attempt',
    tone: 'success',
  };
}
function resetRecovery() {
  recoveryPhase = 0;
  recoveryDecision = undefined;
  renderRecovery();
}
byId('recovery-scenario').addEventListener('change', resetRecovery);
byId('recovery-next').addEventListener('click', () => {
  recoveryPhase += 1;
  renderRecovery();
});
byId('recovery-reset').addEventListener('click', resetRecovery);

let viewerIndex = 0;
function renderViewer() {
  const state = viewerSteps[viewerIndex];
  const tiles = (viewer) =>
    '<span class="event-ellipsis">…</span>' +
    [128, 129, 130, 131, 132]
      .map((number) => {
        const present = number <= (viewer ? state.applied : state.saved);
        const buffered = viewer && state.buffered.includes(number);
        const kind = present ? (viewer ? 'applied' : 'saved') : buffered ? 'buffered' : 'missing';
        const label = buffered
          ? 'buffered'
          : present
            ? number === 132
              ? 'terminal'
              : kind
            : 'waiting';
        return `<span class="event-tile ${kind}" aria-label="Event ${number}: ${label}">
        ${number}<small>${label}</small></span>`;
      })
      .join('');
  byId('saved-events').innerHTML = tiles(false);
  byId('viewer-events').innerHTML = tiles(true);
  write('viewer-cursor', state.applied || '—');
  write('viewer-copy', prose(state.copy));
  write('viewer-status', state.status);
  byId('viewer-status').dataset.tone = !state.applied
    ? 'neutral'
    : state.saved > state.applied
      ? 'warning'
      : 'success';
  write('viewer-next', state.next);
  byId('viewer-next').disabled = viewerIndex === viewerSteps.length - 1;
}
byId('viewer-next').addEventListener('click', () => {
  viewerIndex += 1;
  renderViewer();
});
byId('viewer-reset').addEventListener('click', () => {
  viewerIndex = 0;
  renderViewer();
});

let statsState = createStats();
function renderStats() {
  const { root, child, scope, errors, rootStopped, childStopped } = statsState;
  const budget = scope === 'self' ? root : root + child;
  write(
    'stats-status',
    rootStopped
      ? 'Root and child canceled'
      : childStopped
        ? 'Child canceled · root can continue'
        : budget === 100
          ? 'At the limit · execution can continue'
          : 'Within the token budget',
  );
  byId('stats-status').dataset.tone = rootStopped || childStopped ? 'warning' : 'success';
  byId('budget-root').parentElement.dataset.exceeded = String(rootStopped);
  write('stats-total', root + child);
  write('stats-self', root);
  write('stats-child', child);
  write('stats-errors', errors);
  write('stats-budget-label', `Root ${scope === 'self' ? '$self: ' : ''}budget: ${budget} / 100`);
  byId('budget-root').style.width = `${Math.min(100, root)}%`;
  byId('budget-child').style.width =
    scope === 'self' ? '0%' : `${Math.min(child, Math.max(0, 100 - root))}%`;
  byId('stats-add-root').disabled = rootStopped;
  ['stats-add-child', 'stats-fail', 'stats-succeed'].forEach((id) => {
    byId(id).disabled = childStopped;
  });
  write(
    'stats-copy',
    rootStopped
      ? `The root’s configured counter exceeded 100. Its context is canceled, canceling the child
      too. Accepted usage stays recorded. Reset to try a different budget.`
      : childStopped
        ? `The child exceeded its own inherited $self: limit. Only the child is canceled.
      The root can continue within its limit; the child’s work still appears in root total.`
        : scope === 'self'
          ? `The root limit checks its own work. Child tokens still propagate into
        the total, but cannot consume the root’s $self: budget. The child inherits its own limit.`
          : `Child work contributes to the root’s total counter. Exactly 100 is still within this
          token limit; an update past 100 cancels the root and its children.`,
  );
}
const statActions = {
  'stats-add-root': 'root',
  'stats-add-child': 'child',
  'stats-fail': 'fail',
  'stats-succeed': 'succeed',
};
Object.entries(statActions).forEach(([id, action]) => {
  byId(id).addEventListener('click', () => {
    statsState = updateStats(statsState, action);
    renderStats();
  });
});
function resetStats() {
  statsState = createStats(byId('stats-scope').value);
  renderStats();
}
byId('stats-reset').addEventListener('click', resetStats);
byId('stats-scope').addEventListener('change', resetStats);

let recipeID = 'start';
function renderRecipe() {
  const recipe = data.recipes.find((item) => item.id === recipeID);
  byId('recipe-tabs').innerHTML = data.recipes
    .map(
      (item) => `
    <button data-recipe="${item.id}" aria-pressed="${item.id === recipeID}">${item.label}</button>
  `,
    )
    .join('');
  write('recipe-description', prose(recipe.text));
  byId('recipe-code').innerHTML = highlightGo(recipe.code);
}
let toastTimer;
function toast(message) {
  write('toast', message);
  byId('toast').classList.add('visible');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => byId('toast').classList.remove('visible'), 4000);
}
byId('copy-recipe').addEventListener('click', async () => {
  const code = data.recipes.find((item) => item.id === recipeID).code;
  try {
    // file:// clipboard access differs by browser. Report failures and select the code to copy.
    if (!navigator.clipboard) throw new Error('Clipboard access is unavailable in this browser.');
    await navigator.clipboard.writeText(code);
    toast('Go example copied.');
  } catch (error) {
    console.warn('Clipboard copy failed; selecting the code for manual copying.', error);
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(byId('recipe-code'));
    selection.removeAllRanges();
    selection.addRange(range);
    const shortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘C' : 'Ctrl+C';
    toast(`Code selected. Press ${shortcut} to copy.`);
  }
});
byId('open-example').addEventListener('click', () => {
  openDialog(
    'A COMPLETE GO EXAMPLE',
    `
    <h2 id="dialog-title">From run to recovery.</h2>
    <p class="dialog-label">The executable example included with this checkout.</p>
    <div class="dialog-prose"><p>This example uses an in-memory teaching store and a local model.
      It shows acceptance, restoration, and the distinction between live display and saved evidence.
      Read the comments for the application’s remaining storage responsibilities.</p></div>
    <pre class="dialog-code"><code>${highlightGo(data.example)}</code></pre>
    <p class="source-path">examples/history/main.go · Run from the repository:
      go run ./examples/history</p>
  `,
  );
});

function filterAtlas() {
  const query = byId('atlas-search').value.trim().toLowerCase();
  const layer = byId('atlas-layer').value;
  let count = 0;
  all('.atlas-row').forEach((row) => {
    const item = componentByID.get(row.dataset.inspect);
    const haystack = prose(
      [item.name, item.label, item.job, item.why, item.implementations, item.layer, item.kind].join(
        ' ',
      ),
    ).toLowerCase();
    const shown =
      (layer === 'all' || layer === item.layer) &&
      query.split(/\s+/).every((word) => haystack.includes(word));
    row.hidden = !shown;
    if (shown) count += 1;
  });
  write('atlas-count', `${count} OF ${data.components.length} ABSTRACTIONS`);
  byId('atlas-empty').hidden = count !== 0;
}
byId('atlas-search').addEventListener('input', filterAtlas);
byId('atlas-layer').addEventListener('change', filterAtlas);
document.addEventListener('keydown', (event) => {
  if (
    event.key === '/' &&
    !dialog.open &&
    !event.ctrlKey &&
    !event.metaKey &&
    !['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement.tagName)
  ) {
    event.preventDefault();
    byId('atlas-search').focus();
    byId('atlas').scrollIntoView({ behavior: motionPaused ? 'instant' : 'smooth' });
  }
});

const quizAnswers = new Map();
function renderQuiz() {
  byId('quiz').innerHTML = data.questions
    .map(
      (question, index) => `
    <div class="quiz-question"><h3><span>${String(index + 1).padStart(2, '0')}</span>
      ${escapeHTML(question.question)}</h3>
      <div class="quiz-options" role="group" aria-label="Question ${index + 1}">
        ${question.options
          .map(
            (option, optionIndex) => `
          <button class="quiz-option" data-question="${index}" data-option="${optionIndex}"
            aria-pressed="false"><span class="option-letter">${'ABC'[optionIndex]}</span>
            <span>${escapeHTML(option)}</span></button>`,
          )
          .join('')}
      </div><p id="quiz-feedback-${index}" class="quiz-feedback" aria-live="polite" hidden></p>
    </div>`,
    )
    .join('');
}
function answerQuiz(index, option) {
  quizAnswers.set(index, option);
  const question = data.questions[index];
  const correct = option === question.answer;
  all(`[data-question="${index}"]`).forEach((button) => {
    const selected = Number(button.dataset.option) === option;
    button.classList.toggle('correct', selected && correct);
    button.classList.toggle('incorrect', selected && !correct);
    button.setAttribute('aria-pressed', String(selected));
  });
  const feedback = byId(`quiz-feedback-${index}`);
  feedback.hidden = false;
  feedback.classList.toggle('incorrect', !correct);
  feedback.textContent = correct
    ? `Connected. ${prose(question.feedback)}`
    : `Try again. ${prose(question.feedback)}`;
  const count = [...quizAnswers].filter(([questionIndex, answer]) => {
    return data.questions[questionIndex].answer === answer;
  }).length;
  write(
    'quiz-progress',
    `${count} of 4 ideas connected.${count === 4 ? ' You have the core model.' : ''}`,
  );
}

byId('open-colophon').addEventListener('click', () => {
  openDialog(
    'ABOUT THIS EDITION',
    `
    <h2 id="dialog-title">A field guide, all in one file.</h2>
    <p class="dialog-label">Built for reading, exploring, and keeping nearby.</p>
    <div class="dialog-prose"><p>Source: docs/code. Output: docs/dist/gent.html. The build embeds
      Go declarations, this guide’s local simulations, the reference example, and all visual assets.
      No model calls, analytics, or network connections are made by this page.</p>
      <p>Run and trace schemas: 2. Build-time source fingerprint:
        <code>${escapeHTML(data.fingerprint)}</code>. Rebuild after changing Gent contracts.</p>
      <p>Typefaces: DM Sans, Instrument Serif, and IBM Plex Mono, via Fontsource 5.3.0.
        Their SIL Open Font License notices are included below.</p>
      ${data.licenses
        .map(
          (license) => `<details class="under-the-hood">
        <summary>${escapeHTML(license.name)} license</summary>
        <pre class="license-copy">${escapeHTML(license.text)}</pre></details>`,
        )
        .join('')}
    </div>`,
  );
});

document.addEventListener('click', (event) => {
  const button = event.target.closest('button');
  if (!button || button.disabled) return;
  if (button.dataset.inspect) inspectComponent(button.dataset.inspect);
  if (button.dataset.map) renderMap(button.dataset.map);
  if (button.dataset.response) {
    responsePreset = button.dataset.response;
    renderParser();
  }
  if (button.dataset.entry) inspectEntry(button.dataset.entry);
  if (button.dataset.journalMode) {
    journalState = createJournal(button.dataset.journalMode);
    renderJournal();
  }
  if (button.dataset.toolRecovery) {
    recoveryDecision = button.dataset.toolRecovery;
    recoveryPhase = 3;
    renderRecovery();
  }
  if (button.dataset.recipe) {
    recipeID = button.dataset.recipe;
    renderRecipe();
    byId('recipe-tabs').querySelector(`[data-recipe="${recipeID}"]`).focus({ preventScroll: true });
  }
  if (button.dataset.question !== undefined) {
    answerQuiz(Number(button.dataset.question), Number(button.dataset.option));
  }
});

const chapters = all('.chapter');
let scrollPending = false;
let activeChapter;
function updateNavigation() {
  const scrollable = document.documentElement.scrollHeight - window.innerHeight;
  byId('reading-progress').style.width =
    `${scrollable > 0 ? (window.scrollY / scrollable) * 100 : 0}%`;
  let current = chapters[0].id;
  for (const chapter of chapters) {
    if (chapter.getBoundingClientRect().top <= 180) current = chapter.id;
  }
  all('#chapter-nav a').forEach((link) => {
    const selected = link.hash === `#${current}`;
    link.classList.toggle('active', selected);
    if (selected) link.setAttribute('aria-current', 'location');
    else link.removeAttribute('aria-current');
    if (selected && current !== activeChapter && matchMedia('(max-width: 850px)').matches) {
      // Move only the chapter strip, preserving the reader's vertical position.
      const nav = byId('chapter-nav');
      nav.scrollTo({ left: link.offsetLeft - nav.offsetLeft - 12, behavior: 'instant' });
    }
  });
  activeChapter = current;
  scrollPending = false;
}
window.addEventListener(
  'scroll',
  () => {
    if (!scrollPending) {
      scrollPending = true;
      requestAnimationFrame(updateNavigation);
    }
  },
  { passive: true },
);
window.addEventListener('resize', updateNavigation);
const reveals = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        reveals.unobserve(entry.target);
      }
    });
  },
  { threshold: 0.04 },
);
all('.reveal').forEach((element) => reveals.observe(element));

renderMap('context');
renderRun();
renderMotion();
renderParser();
renderHistory();
renderJournal();
renderRecovery();
renderViewer();
renderStats();
renderRecipe();
filterAtlas();
renderQuiz();
updateNavigation();

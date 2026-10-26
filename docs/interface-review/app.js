(() => {
  'use strict';
  const data = JSON.parse(document.getElementById('gent-data').textContent);
  const byId = new Map(data.entries.map((entry) => [entry.id, entry]));
  const $ = (id) => document.getElementById(id);
  const escape = (value) =>
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
  const storageKey = `gent-interface-review:${data.fingerprint}`;
  const statusNames = {
    unread: 'Not reviewed',
    reviewed: 'Reviewed',
    question: 'Discuss',
  };
  const state = {
    view: 'learn',
    selected: 'gent.LoopData',
    phase: 3,
    notes: {},
  };
  let storageAvailable = true;
  let feedbackTimer;

  function feedback(message) {
    $('feedback').textContent = message;
    $('feedback').hidden = false;
    clearTimeout(feedbackTimer);
    feedbackTimer = setTimeout(() => {
      $('feedback').hidden = true;
    }, 5000);
  }

  function loadNotes() {
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey) || '{}');
      for (const [id, note] of Object.entries(saved.notes || {})) {
        if (byId.has(id) && note && typeof note.text === 'string' && statusNames[note.status]) {
          state.notes[id] = { text: note.text, status: note.status };
        }
      }
    } catch (error) {
      console.warn('Could not load local review notes.', error);
      storageAvailable = false;
      feedback('Browser storage is unavailable. Export notes to keep them.');
    }
  }

  function saveNotes() {
    try {
      localStorage.setItem(storageKey, JSON.stringify({ version: 1, notes: state.notes }));
      storageAvailable = true;
    } catch (error) {
      console.warn('Could not save local review notes.', error);
      storageAvailable = false;
    }
    const label = $('note-save-status');
    if (label)
      label.textContent = storageAvailable
        ? 'Saved in this browser.'
        : 'Session only: browser storage is unavailable. Export notes to keep them.';
    updateReviewCount();
  }

  function noteFor(id) {
    return state.notes[id] || { status: 'unread', text: '' };
  }

  function markedEntries() {
    return data.entries.filter((entry) => {
      const note = noteFor(entry.id);
      return note.status !== 'unread' || note.text.trim();
    });
  }

  function updateReviewCount() {
    $('note-count').textContent = markedEntries().length;
  }

  function links(ids) {
    return `<div class="related">${ids
      .map(
        (id) =>
          `<button type="button" data-open="${escape(id)}"
          >${escape(byId.get(id)?.name || id)} ↗</button>`,
      )
      .join('')}</div>`;
  }

  function source(file, line) {
    return (
      `<a href="../../${escape(file)}#L${line}" target="_blank" rel="noopener">` +
      `${escape(file)}:${line}</a>`
    );
  }

  function renderArchitecture() {
    const changed = data.entries.filter(
      (item) => item.kind === 'interface' && item.change !== 'same',
    );
    $('interface-count').innerHTML = `${data.interfaceCount}<span>named interfaces</span>`;
    $('scope-detail').textContent =
      `${changed.length} new or changed declarations · ` +
      `${data.supportingCount} supporting types · ` +
      `${data.capabilities.length} structural capabilities`;
    $('snapshot').textContent = `CHECKOUT SNAPSHOT / ${data.fingerprint.slice(0, 8)}`;
    $('footer-source').textContent = `SOURCE ${data.fingerprint} · BASE ${data.head.slice(0, 8)}`;
    $('provenance').textContent =
      `Snapshot of ${data.sourceFiles} non-test Go source files. ` +
      `Source fingerprint: ${data.fingerprint}. Comparison base: ${data.head}.`;
    const labels = {
      Execution: 'The loop, its task, and its lifecycle.',
      'Model & content': 'Providers, streaming, and response structure.',
      'Tools & answers': 'Actions, plans, and answer acceptance.',
      'History & recovery': 'Atomic saving, owned values, and takeover.',
      'Events & observation': 'Mutable hooks and accepted evidence.',
      Compaction: 'When to reduce context, and how.',
      Search: 'Tool discovery and generic retrieval.',
    };
    $('system-map').innerHTML =
      data.groups
        .map((group, index) => {
          const count = data.entries.filter(
            (item) => item.kind === 'interface' && item.group === group,
          ).length;
          return `<button class="map-group" type="button" data-group="${escape(group)}">
        <span class="eyebrow"><span>0${index + 1}</span><span>${count} INTERFACES ↗</span></span>
        <strong>${escape(group)}</strong><p>${escape(labels[group])}</p></button>`;
        })
        .join('') +
      `<div class="map-note"><span class="eyebrow">THE GUIDING DISTINCTION</span>
      <p>What happened.<br>What the model sees.<br>Where control can resume.</p></div>`;
    $('boundaries').innerHTML = data.boundaries
      .map(
        ([title, label, body, why, related]) =>
          `<article class="boundary"><h3>${escape(title)}</h3>
        <p class="boundary-label">${escape(label)}</p><p>${escape(body)}</p>
        <p class="why">${escape(why)}</p>${links(related)}</article>`,
      )
      .join('');
    $('group').insertAdjacentHTML(
      'beforeend',
      data.groups
        .map((group) => `<option value="${escape(group)}">${escape(group)}</option>`)
        .join(''),
    );
  }

  function filteredEntries() {
    const query = $('search').value.trim().toLowerCase();
    return data.entries.filter((item) => {
      if ($('kind').value === 'interface' && item.kind !== 'interface') return false;
      if ($('kind').value === 'support' && item.kind === 'interface') return false;
      if ($('group').value !== 'all' && item.group !== $('group').value) return false;
      const change = $('change').value;
      if (change === 'different' && item.change === 'same') return false;
      if (!['all', 'different'].includes(change) && item.change !== change) return false;
      return `${item.id} ${item.purpose} ${item.why} ${item.code} ${item.rules.join(' ')}`
        .toLowerCase()
        .includes(query);
    });
  }

  function renderList() {
    const entries = filteredEntries();
    $('result-count').textContent = `${entries.length} matching contracts`;
    $('mobile-count').textContent = `(${entries.length})`;
    $('contract-list').innerHTML = entries.length
      ? data.groups
          .map((group) => {
            const items = entries.filter((item) => item.group === group);
            if (!items.length) return '';
            return (
              `<p class="list-group">${escape(group)}</p>` +
              items
                .map((item) => {
                  const note = noteFor(item.id);
                  const label =
                    note.status === 'reviewed'
                      ? '✓'
                      : note.status === 'question'
                        ? '?'
                        : item.change === 'new'
                          ? 'NEW'
                          : item.change === 'changed'
                            ? 'Δ'
                            : '';
                  const status = note.status !== 'unread' ? statusNames[note.status] : item.change;
                  return `<button type="button" class="contract-row" data-open="${escape(item.id)}"
          ${item.id === state.selected ? 'aria-current="true"' : ''}
          aria-label="${escape(item.name)}, ${escape(status)}">
          <span class="contract-name">${escape(item.name)}</span>
          <span class="row-state" aria-hidden="true">${label}</span></button>`;
                })
                .join('')
            );
          })
          .join('')
      : '<p class="empty-state">No matching contracts. Try clearing the filters.</p>';
  }

  function resetFilters() {
    $('search').value = '';
    $('group').value = 'all';
    $('kind').value = 'interface';
    $('change').value = 'all';
  }

  function embeddedContracts(item) {
    if (!item.embeds.length) return '';
    return (
      `<div class="detail-section"><h3>Embedded contracts</h3>` +
      item.embeds
        .map((name) => {
          const id = `${item.id.slice(0, item.id.lastIndexOf('.') + 1)}${name}`;
          if (byId.has(id))
            return `<p class="small">Inherits the methods of ${escape(name)}.</p>` + links([id]);
          const explanation =
            name === 'error'
              ? 'Built-in error: Error() string.'
              : 'This contract is supplied by an external package.';
          return `<p><code>${escape(name)}</code> — ${escape(explanation)}</p>`;
        })
        .join('') +
      '</div>'
    );
  }

  function methods(item) {
    if (!item.methods.length) return '';
    const title = item.kind === 'interface' ? 'Declared methods' : 'Public methods';
    return (
      `<details class="detail-section"><summary>${title} (${item.methods.length})</summary>` +
      item.methods
        .map(
          (method) => `<details class="method-row">
        <summary>${escape(method.code)}</summary>
        <p>${escape(method.doc || 'No method-specific source comment. See the contract above.')}</p>
        <div class="source-line">${source(method.file, method.line)}</div></details>`,
        )
        .join('') +
      '</details>'
    );
  }

  function renderDetail() {
    const item = byId.get(state.selected);
    const note = noteFor(item.id);
    const changedLabel =
      item.change === 'new'
        ? 'NEW DECLARATION'
        : item.change === 'changed'
          ? 'CHANGED DECLARATION'
          : 'UNCHANGED DECLARATION';
    const kindLabel = item.kind === 'interface' ? 'INTERFACE' : 'SUPPORTING TYPE';
    const comparison = item.previous
      ? `<details class="detail-section">
      <summary>Previous declaration at ${data.head.slice(0, 8)}</summary>
      <pre class="previous-code"><code>${escape(item.previous)}</code></pre>
      <p class="small">Compared in the same source file; bodies and comments are excluded.</p>
      </details>`
      : '';
    $('contract-detail').innerHTML = `<div class="detail-topline">
      <span>${escape(item.group)}</span><span class="badge">${kindLabel}</span>
      <span class="badge ${item.change}">${changedLabel}</span></div>
      <h2 class="detail-title" id="selected-contract" tabindex="-1">${escape(item.id)}</h2>
      <p class="detail-purpose">${escape(item.purpose)}</p>
      <div class="source-line">${source(item.file, item.line)}
        <button type="button" class="text-button" id="copy-reference"
        >Copy source reference</button></div>
      <div class="contract-code"><div class="code-label"><span>GO DECLARATION</span>
        <button type="button" class="text-button" id="copy-signature">Copy</button></div>
        <pre><code id="selected-code">${escape(item.code)}</code></pre></div>
      ${
        item.change === 'same'
          ? '<p class="small">The declaration is unchanged; its behavior may have changed.</p>'
          : ''
      }
      <p class="rationale">${escape(item.why)}</p>
      ${
        item.rules.length
          ? `<section class="detail-section"><h3>Contract to preserve</h3><ul>` +
            item.rules.map((rule) => `<li>${escape(rule)}</li>`).join('') +
            '</ul></section>'
          : ''
      }
      ${
        item.implementations.length
          ? `<section class="detail-section"><h3>Implementations & usage</h3>
        <p class="implementations">${item.implementations.map(escape).join('<br>')}</p></section>`
          : ''
      }
      ${embeddedContracts(item)}${methods(item)}${comparison}
      <details class="detail-section"><summary>Original declaration & source comments</summary>
        <p class="source-comment">${escape(item.doc || 'No type-level source comment.')}</p>
        <pre><code>${escape(item.fullCode)}</code></pre></details>
      ${
        item.related.length
          ? `<section class="detail-section"><h3>Connected contracts</h3>
        ${links(item.related)}</section>`
          : ''
      }
      <section class="review-box"><p class="eyebrow">REVIEW THIS CONTRACT</p>
        <h3>A question to start with</h3><p class="review-question">${escape(item.question)}</p>
        <div class="review-status" aria-label="Review status">
          ${Object.entries(statusNames)
            .map(
              ([status, label]) => `<button type="button"
            data-status="${status}" aria-pressed="${status === note.status}">${label}</button>`,
            )
            .join('')}
        </div><label class="field-label" for="review-note">Your notes or questions</label>
        <textarea id="review-note"
          placeholder="What should we discuss about ${escape(item.name)}?"
        >${escape(note.text)}</textarea>
        <p id="note-save-status" class="small">${
          storageAvailable
            ? 'Saved in this browser.'
            : 'Session only: browser storage is unavailable. Export notes to keep them.'
        }</p></section>
      <div class="detail-paging">
        <button type="button" class="text-button" data-page="-1">← Previous match</button>
        <button type="button" class="text-button" data-page="1">Next match →</button></div>`;
    updatePaging();
    $('review-note').addEventListener('input', (event) => {
      state.notes[item.id] = { ...noteFor(item.id), text: event.target.value };
      saveNotes();
    });
    $('copy-signature').addEventListener('click', () => copy(item.code));
    $('copy-reference').addEventListener('click', () =>
      copy(`${item.id} — ${item.file}:${item.line} (source ${data.fingerprint})`),
    );
  }

  function updatePaging() {
    const entries = filteredEntries();
    const index = entries.findIndex((item) => item.id === state.selected);
    document.querySelectorAll('[data-page]').forEach((button) => {
      const next = index + Number(button.dataset.page);
      button.disabled = index < 0 || next < 0 || next >= entries.length;
    });
  }

  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
      feedback('Copied.');
    } catch (error) {
      console.warn('Clipboard unavailable; offering the exact text for manual copying.', error);
      $('manual-copy').value = text;
      $('copy-help').showModal();
      $('manual-copy').focus();
      $('manual-copy').select();
    }
  }

  function renderRecovery() {
    $('phase-list').innerHTML = data.phases
      .map(
        ([id], index) =>
          `<button type="button" data-phase="${index}" aria-pressed="${index === state.phase}"
          >${escape(id)}</button>`,
      )
      .join('');
    const [id, title, saved, takeover, related] = data.phases[state.phase];
    $('phase-detail').innerHTML = `<p class="phase-label">${escape(id)}</p><h2>${escape(title)}</h2>
      <h3>What this boundary saves</h3><p>${escape(saved)}</p>
      <h3>Continuation contract</h3><p>${escape(takeover)}</p>
      <h3>Inspect the contracts</h3>${links(related)}`;
    $('capabilities').innerHTML = data.capabilities
      .map(
        (item) =>
          `<article class="capability"><h3>${escape(item.name)}</h3><p>${escape(item.purpose)}</p>
        <pre><code>${escape(item.code)}</code></pre><details>
        <summary>Source checks (${item.sites.length})</summary><div class="source-line">${item.sites
          .map((site) => source(site.file, site.line))
          .join('<br>')}</div></details></article>`,
      )
      .join('');
    renderSave();
  }

  function renderSave() {
    const outcomes = {
      success: {
        values: ['Batch committed', 'Cursor advances', 'Delivery may proceed'],
        explanation:
          'The recorder commits and returns success. Gent promotes the accepted view, then ' +
          'notifies accepted observers. Display delivery failure does not undo the transaction.',
      },
      lost: {
        values: ['Batch committed', 'Cursor unchanged', 'Execution stopped'],
        explanation:
          'In this scenario storage committed but the acknowledgment was lost. RetrySave sends ' +
          'the same batch. An idempotent recorder reconciles it; resume from accepted state on a ' +
          'fresh context. The failed context remains canceled.',
      },
      rejected: {
        values: ['Batch rejected', 'Cursor unchanged', 'Execution stopped'],
        explanation:
          'Storage rejects this writer under its lease or fencing check. No candidate evidence ' +
          'is promoted locally. Establish valid writer authority and load the ' +
          'accepted run before replacement work; cancellation alone cannot fence a stale worker.',
      },
    };
    const mode = $('save-outcome').value;
    const result = outcomes[mode];
    $('save-lanes').innerHTML = ['Application storage', 'Gent accepted view', 'Next external work']
      .map(
        (label, index) => `<div class="save-lane ${mode !== 'success' ? 'warning' : ''}">
        <span>${label}</span><strong>${result.values[index]}</strong></div>`,
      )
      .join('');
    $('save-explanation').textContent = result.explanation;
  }

  function renderReview() {
    const entries = markedEntries();
    const reviewed = entries.filter(
      (entry) => entry.kind === 'interface' && noteFor(entry.id).status === 'reviewed',
    ).length;
    const questions = entries.filter((entry) => noteFor(entry.id).status === 'question').length;
    $('review-summary').innerHTML =
      `<span><strong>${reviewed}/${data.interfaceCount}</strong> interfaces reviewed</span>
      <span><strong>${questions}</strong> marked for discussion</span>
      <button type="button" class="primary-button" data-export>Export review notes ↗</button>`;
    $('review-list').innerHTML = entries.length
      ? entries
          .map((entry) => {
            const note = noteFor(entry.id);
            return `<article class="review-item"><h3>${escape(entry.id)}
              <span class="badge">${statusNames[note.status]}</span></h3>
        <p>${escape(note.text || 'No written note.')}</p><button type="button" class="text-button"
          data-open="${escape(entry.id)}">Return to contract →</button></article>`;
          })
          .join('')
      : `<p class="empty-state">No notes yet. Mark an interface reviewed or leave a question.</p>
      <button type="button" class="primary-button" data-open="gent.Recorder"
      >Review Recorder →</button>`;
  }

  function exportNotes() {
    const lines = [
      '# Gent interface review',
      '',
      `Source fingerprint: ${data.fingerprint}`,
      `Comparison HEAD: ${data.head}`,
      '',
      'This review refers to the source snapshot embedded in gent-interfaces.html.',
      '',
    ];
    const marked = markedEntries();
    if (!marked.length) lines.push('No review notes yet.', '');
    for (const entry of marked) {
      const note = noteFor(entry.id);
      lines.push(
        `## ${entry.id}`,
        '',
        `Status: ${statusNames[note.status]}`,
        `Source: ${entry.file}:${entry.line}`,
        '',
        note.text.trim() || 'No written note.',
        '',
      );
    }
    const url = URL.createObjectURL(
      new Blob([lines.join('\n')], { type: 'text/markdown;charset=utf-8' }),
    );
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = 'gent-interface-review.md';
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    feedback('Review notes exported.');
  }

  function applyRoute(shouldScroll = false) {
    const params = new URLSearchParams(location.hash.slice(1));
    const requestedView = params.get('view');
    state.view = ['learn', 'architecture', 'atlas', 'recovery', 'review'].includes(requestedView)
      ? requestedView
      : 'learn';
    const id = params.get('item');
    if (byId.has(id)) state.selected = id;
    document.querySelectorAll('.view').forEach((view) => {
      view.hidden = view.id !== state.view;
    });
    document.querySelectorAll('[data-view]').forEach((button) => {
      if (button.dataset.view === state.view) button.setAttribute('aria-current', 'page');
      else button.removeAttribute('aria-current');
    });
    if (state.view === 'atlas') {
      if (!filteredEntries().some((entry) => entry.id === state.selected)) {
        resetFilters();
        $('kind').value = byId.get(state.selected).kind === 'interface' ? 'interface' : 'support';
      }
      renderList();
      renderDetail();
    }
    if (state.view === 'recovery') renderRecovery();
    if (state.view === 'review') renderReview();
    if (shouldScroll) window.scrollTo({ top: 0, behavior: 'instant' });
  }

  function navigate(view, id = state.selected) {
    const params = new URLSearchParams({ view });
    if (view === 'atlas') params.set('item', id);
    history.pushState(null, '', `#${params}`);
    applyRoute(true);
  }

  function openContract(id) {
    if (!byId.has(id)) return;
    const alreadyInAtlas = state.view === 'atlas';
    navigate('atlas', id);
    if (window.matchMedia('(max-width: 760px)').matches) $('catalog-panel').open = false;
    if (alreadyInAtlas) $('selected-contract').focus({ preventScroll: true });
  }

  document.addEventListener('click', (event) => {
    const button = event.target.closest('button');
    if (!button) return;
    if (button.dataset.open) openContract(button.dataset.open);
    if (button.dataset.view) navigate(button.dataset.view);
    if (button.dataset.group) {
      resetFilters();
      $('group').value = button.dataset.group;
      openContract(filteredEntries()[0].id);
    }
    if (button.dataset.phase !== undefined) {
      state.phase = Number(button.dataset.phase);
      renderRecovery();
      $('phase-list').querySelector(`[data-phase="${state.phase}"]`).focus();
    }
    if (button.dataset.status) {
      state.notes[state.selected] = {
        ...noteFor(state.selected),
        status: button.dataset.status,
      };
      saveNotes();
      document
        .querySelectorAll('[data-status]')
        .forEach((item) =>
          item.setAttribute('aria-pressed', String(item.dataset.status === button.dataset.status)),
        );
      renderList();
    }
    if (button.dataset.page) {
      const entries = filteredEntries();
      const index = entries.findIndex((item) => item.id === state.selected);
      const next = entries[index + Number(button.dataset.page)];
      if (next) openContract(next.id);
    }
    if (button.hasAttribute('data-export')) exportNotes();
  });

  for (const id of ['search', 'group', 'kind', 'change']) {
    $(id).addEventListener(id === 'search' ? 'input' : 'change', () => {
      renderList();
      updatePaging();
    });
  }
  $('clear-filters').addEventListener('click', () => {
    resetFilters();
    renderList();
    updatePaging();
  });
  $('show-changes').addEventListener('click', () => {
    resetFilters();
    $('change').value = 'different';
    openContract(filteredEntries()[0].id);
  });
  $('save-outcome').addEventListener('change', renderSave);
  $('export').addEventListener('click', exportNotes);
  $('show-licenses').addEventListener('click', () => {
    $('licenses').showModal();
  });
  $('close-licenses').addEventListener('click', () => {
    $('licenses').close();
  });
  $('close-copy').addEventListener('click', () => {
    $('copy-help').close();
  });
  $('license-content').innerHTML = data.licenses
    .map(
      (license) =>
        `<details><summary>${escape(license.name)}</summary>
        <pre>${escape(license.text)}</pre></details>`,
    )
    .join('');
  document.addEventListener('keydown', (event) => {
    if (
      event.key !== '/' ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey ||
      ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName) ||
      document.querySelector('dialog[open]')
    )
      return;
    event.preventDefault();
    navigate('atlas');
    $('catalog-panel').open = true;
    $('search').focus();
  });
  window.addEventListener('hashchange', () => applyRoute(true));
  window.addEventListener('popstate', () => applyRoute(true));
  loadNotes();
  renderArchitecture();
  updateReviewCount();
  applyRoute();
})();

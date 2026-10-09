// These small models teach contracts; they do not execute Gent or make external calls.
// Pure transitions let tests check the failure paths independently of presentation.
export function createHistory() {
  return {
    revision: 1,
    entries: [
      {
        id: 'A',
        kind: 'Original',
        title: 'A request from the user',
        text: 'Can I get a refund for an opened product bought 10 days ago?',
        source: 'User input captured by the application.',
      },
      {
        id: 'B',
        kind: 'Original',
        title: 'The full policy observation',
        text: `Policy P10, version 4. Refunds are allowed within 14 days of purchase. Opened
          products are excluded unless defective. The receipt and original order ID are
          required.`,
        source: 'The actual text returned by the policy tool, with its ID and version.',
      },
    ],
    selections: [['A', 'B']],
  };
}

export function compactHistory(state) {
  if (state.selections.length >= 3) return state;
  const first = state.selections.length === 1;
  const id = first ? 'S1' : 'S2';
  const previous = state.selections.at(-1);
  const summary = {
    id,
    kind: 'Summary',
    title: first ? 'First summary · exception kept' : 'Second summary · less detail',
    text: first
      ? `The user bought an opened product 10 days ago. Policy P10 allows refunds within 14 days,
          but excludes opened products unless defective. A receipt and order ID are required.`
      : 'The user requests a refund after 10 days. Policy P10 has a 14-day refund window.',
    source: `Derived from ${previous.join(' + ')}. The input entries remain unchanged in history.`,
  };
  const marker = {
    id: first ? 'C1' : 'C2',
    kind: 'Compaction',
    title: `${previous.join(' + ')} → ${id}`,
    text: `Accepted compaction: input selection [${previous.join(', ')}], output selection [${id}].
      This marker records the decision. It is evidence, not a message added to the next prompt.`,
    source: `Revision ${state.selections.length} → ${state.selections.length + 1}.`,
  };
  return {
    revision: state.selections.length + 1,
    entries: [...state.entries, summary, marker],
    selections: [...state.selections, [id]],
  };
}

export function contextAt(state, revision) {
  const ids = state.selections[revision - 1];
  if (!ids) throw new RangeError(`Unknown history revision ${revision}`);
  return ids.map((id) => state.entries.find((entry) => entry.id === id));
}

export function createJournal(mode = 'success') {
  return { mode, phase: 0, accepted: 120, stored: 120, saveCalls: 0, batchID: 'refund-42/b17' };
}

export function advanceJournal(state) {
  if (state.phase < 2) return { ...state, phase: state.phase + 1 };
  if (state.phase === 2) {
    return {
      ...state,
      phase: 3,
      saveCalls: 1,
      stored: state.mode === 'rejected' ? 120 : 121,
      accepted: state.mode === 'success' ? 121 : 120,
    };
  }
  if (state.phase === 3 && state.mode === 'lost') {
    // The exact batch is acknowledged, without appending a second copy or reviving the old context.
    return { ...state, phase: 4, accepted: 121, saveCalls: 2 };
  }
  return state;
}

export function journalView(state) {
  const { mode, phase } = state;
  if (phase === 0)
    return {
      next: 'Queue a batch →',
      rail: 0,
      provider: 'Waiting',
      copy: `The accepted run ends at event 120. A new model request is waiting to be recorded.
      Its checkpoint is still at 120. Nothing has been sent to the provider.`,
    };
  if (phase === 1)
    return {
      next: 'Send to Recorder.Save →',
      rail: 1,
      provider: 'Waiting',
      copy: `ExecutionContext prepares batch refund-42/b17 with event 121, cursor 120, and a digest.
        Assigning a number is preparation. Export and accepted observers still stop at
        120.`,
    };
  if (phase === 2)
    return {
      next: 'Resolve the save →',
      rail: 2,
      provider: 'Waiting',
      copy: `Recorder.Save is in progress. The application must check writer ownership and the
        expected cursor, deduplicate the batch, and commit it atomically. Gent waits for
        acknowledgment.`,
    };
  if (phase === 4)
    return {
      next: 'Batch reconciled',
      rail: 3,
      provider: 'Stopped',
      done: true,
      copy: `The application sends the same batch and storage recognizes its digest. No duplicate
        event is added. The stored view stays at 121. The stopped ExecutionContext stays
        stopped: restore a fresh context from the reconciled bundle before continuing.`,
    };
  if (mode === 'lost')
    return {
      next: 'Retry the identical batch →',
      rail: 2,
      provider: 'Stopped',
      copy: `Storage committed 121, but its acknowledgment was lost. Gent receives a save error and
      keeps its accepted cursor at 120. PersistenceError retains the exact uncertain batch.
      The next provider call cannot start.`,
    };
  if (mode === 'rejected')
    return {
      next: 'Writer rejected',
      rail: 2,
      provider: 'Stopped',
      done: true,
      copy: `The storage adapter rejects an expired writer lease before committing. Both views stay
      at 120. This worker cannot continue. The application gives the legitimate replacement writer
      the accepted bundle and fresh runtime bindings.`,
    };
  return {
    next: 'Batch accepted',
    rail: 3,
    provider: 'Allowed',
    done: true,
    copy: `Save returns success. ExecutionContext advances its accepted view to 121.
      Accepted observers can now see it and the provider call may begin. This progress-only batch
      leaves the control checkpoint at 120.`,
  };
}

export const viewerSteps = [
  {
    saved: 128,
    applied: 0,
    buffered: [],
    status: 'Not connected',
    next: 'Join at event 128 →',
    copy: `The shared store contains activity through 128, even though the latest control checkpoint
      is at 120. A new viewer has no local history yet.`,
  },
  {
    saved: 128,
    applied: 128,
    buffered: [],
    status: 'In sync',
    next: 'Miss 129; receive 130 →',
    copy: `Subscribe first and buffer delivery while loading the full accepted run. This read ends
      at 128. Discard buffered duplicates at or below 128, then apply later events in order.`,
  },
  {
    saved: 130,
    applied: 128,
    buffered: [130],
    status: 'Gap detected',
    next: 'Reload the full view →',
    copy: `Events 129 and 130 are committed, but only 130 arrives. The viewer buffers 130 and keeps
      its applied cursor at 128. It needs the missing evidence before advancing.`,
  },
  {
    saved: 130,
    applied: 130,
    buffered: [],
    status: 'Gap repaired',
    next: 'Receive event 131 →',
    copy: `A consistent full read contains 129 and 130. Replace the local view through 130, discard
      the buffered duplicate, and continue consuming later events in order.`,
  },
  {
    saved: 131,
    applied: 131,
    buffered: [],
    status: 'In sync',
    next: 'Lose the final update →',
    copy: 'Event 131 arrives in order. The viewer applies it and advances its cursor to 131.',
  },
  {
    saved: 132,
    applied: 131,
    buffered: [],
    status: 'Quietly stale',
    next: 'Run a periodic full read →',
    copy: `The terminal event 132 is saved, but delivery is lost. No later event arrives to
      expose a gap. The viewer still appears to be running. Reconnecting only when a gap appears
      is not enough.`,
  },
  {
    saved: 132,
    applied: 132,
    buffered: [],
    status: 'Terminal result recovered',
    next: 'Viewer is current',
    copy: `A periodic full read discovers event 132 and the saved final outcome. The viewer is
      current again. Delivery may drop an update; committed evidence survives that delivery
      failure.`,
  },
];

export function createStats(scope = 'total') {
  return { scope, root: 60, child: 20, errors: 0, rootStopped: false, childStopped: false };
}

export function updateStats(state, action) {
  if (action === 'root' && state.rootStopped) return state;
  if (action !== 'root' && state.childStopped) return state;
  const next = { ...state };
  if (action === 'root') next.root += 10;
  if (action === 'child') next.child += 15;
  if (action === 'fail') next.errors += 1;
  if (action === 'succeed') next.errors = 0;
  const rootBudget = next.scope === 'self' ? next.root : next.root + next.child;
  next.rootStopped = rootBudget > 100;
  next.childStopped = next.rootStopped || next.child > 100;
  return next;
}

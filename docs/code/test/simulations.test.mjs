import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createHistory,
  compactHistory,
  contextAt,
  createJournal,
  advanceJournal,
  journalView,
  viewerSteps,
  createStats,
  updateStats,
} from '../src/simulations.mjs';

test('two compactions retain the exact original evidence and reconstruct earlier context', () => {
  const original = createHistory();
  const before = structuredClone(original);
  const first = compactHistory(original);
  const second = compactHistory(first);
  assert.deepEqual(original, before);
  assert.deepEqual(second.entries.slice(0, 2), before.entries);
  assert.deepEqual(contextAt(second, 1), before.entries);
  assert.deepEqual(contextAt(second, 2), contextAt(first, 2));
  assert.deepEqual(second.selections, [['A', 'B'], ['S1'], ['S2']]);
  assert.deepEqual(contextAt(second, 3), [
    {
      id: 'S2',
      kind: 'Summary',
      title: 'Second summary · less detail',
      text: 'The user requests a refund after 10 days. Policy P10 has a 14-day refund window.',
      source: 'Derived from S1. The input entries remain unchanged in history.',
    },
  ]);
  assert.deepEqual(
    second.entries
      .filter((entry) => entry.kind === 'Compaction')
      .map((entry) => ({
        id: entry.id,
        title: entry.title,
        source: entry.source,
      })),
    [
      { id: 'C1', title: 'A + B → S1', source: 'Revision 1 → 2.' },
      { id: 'C2', title: 'S1 → S2', source: 'Revision 2 → 3.' },
    ],
  );
  assert.throws(() => contextAt(second, 4), {
    name: 'RangeError',
    message: 'Unknown history revision 4',
  });
});

const journalCases = [
  {
    name: 'acknowledged save permits the provider only after acceptance',
    mode: 'success',
    expected: { accepted: 121, stored: 121, saveCalls: 1, provider: 'Allowed' },
  },
  {
    name: 'lost acknowledgment leaves the committed batch ahead of Gent',
    mode: 'lost',
    expected: { accepted: 120, stored: 121, saveCalls: 1, provider: 'Stopped' },
  },
  {
    name: 'stale writer cannot change storage or start an effect',
    mode: 'rejected',
    expected: { accepted: 120, stored: 120, saveCalls: 1, provider: 'Stopped' },
  },
];
for (const scenario of journalCases) {
  test(scenario.name, () => {
    let state = createJournal(scenario.mode);
    for (let step = 0; step < 3; step += 1) {
      assert.equal(journalView(state).provider, 'Waiting');
      assert.equal(state.accepted, 120);
      state = advanceJournal(state);
    }
    assert.deepEqual(
      {
        accepted: state.accepted,
        stored: state.stored,
        saveCalls: state.saveCalls,
        provider: journalView(state).provider,
      },
      scenario.expected,
    );
  });
}

test('retry reconciles the same batch without another event or reviving execution', () => {
  let state = createJournal('lost');
  const batchID = state.batchID;
  for (let step = 0; step < 4; step += 1) state = advanceJournal(state);
  assert.deepEqual(state, {
    mode: 'lost',
    phase: 4,
    accepted: 121,
    stored: 121,
    saveCalls: 2,
    batchID,
  });
  assert.equal(journalView(state).provider, 'Stopped');
  assert.deepEqual(advanceJournal(state), state);
});

test('viewer buffers a gap and discovers a lost terminal event by a full read', () => {
  assert.deepEqual(
    viewerSteps.map(({ saved, applied, buffered }) => ({ saved, applied, buffered })),
    [
      { saved: 128, applied: 0, buffered: [] },
      { saved: 128, applied: 128, buffered: [] },
      { saved: 130, applied: 128, buffered: [130] },
      { saved: 130, applied: 130, buffered: [] },
      { saved: 131, applied: 131, buffered: [] },
      { saved: 132, applied: 131, buffered: [] },
      { saved: 132, applied: 132, buffered: [] },
    ],
  );
});

const statsCases = [
  {
    name: 'exactly the token budget remains allowed',
    scope: 'total',
    actions: ['root', 'root'],
    expected: { root: 80, child: 20, errors: 0, rootStopped: false, childStopped: false },
  },
  {
    name: 'child usage cancels root when the propagated budget is exceeded',
    scope: 'total',
    actions: ['child', 'child', 'root', 'child'],
    expected: { root: 60, child: 50, errors: 0, rootStopped: true, childStopped: true },
  },
  {
    name: 'a child self limit stops only that child',
    scope: 'self',
    actions: ['child', 'child', 'child', 'child', 'child', 'child', 'root'],
    expected: { root: 70, child: 110, errors: 0, rootStopped: false, childStopped: true },
  },
  {
    name: 'successful tool call clears the local streak',
    scope: 'total',
    actions: ['fail', 'fail', 'succeed'],
    expected: { root: 60, child: 20, errors: 0, rootStopped: false, childStopped: false },
  },
  {
    name: 'failed tool calls increase only the local gauge in this demo',
    scope: 'total',
    actions: ['fail', 'fail'],
    expected: { root: 60, child: 20, errors: 2, rootStopped: false, childStopped: false },
  },
];
for (const scenario of statsCases) {
  test(scenario.name, () => {
    const result = scenario.actions.reduce(updateStats, createStats(scenario.scope));
    assert.deepEqual(result, { scope: scenario.scope, ...scenario.expected });
  });
}

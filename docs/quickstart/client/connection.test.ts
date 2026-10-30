import assert from "node:assert/strict";
import test from "node:test";
import { connectRun, type Transport } from "./src/connection";
import type { Frame, State } from "./src/state";

const initial: State = {
  runId: "run",
  lastEventNumber: "0",
  status: "running",
  previews: [],
  answers: [],
};

class FakeSocket {
  onmessage: WebSocket["onmessage"] = null;
  onclose: WebSocket["onclose"] = null;
  onerror: WebSocket["onerror"] = null;
  closed = false;
  close() {
    this.closed = true;
  }
  send(frame: Frame) {
    this.onmessage?.call(
      this as unknown as WebSocket,
      new MessageEvent("message", { data: JSON.stringify(frame) }),
    );
  }
}

function harness(pollMs = 100000) {
  const sockets: FakeSocket[] = [];
  const loads: { resolve: (state: State) => void; signal: AbortSignal }[] = [];
  const states: State[] = [];
  const statuses: string[] = [];
  const transport: Transport = {
    open() {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    load(_run, signal) {
      return new Promise((resolve) => loads.push({ resolve, signal }));
    },
    pollMs,
    retryMs: 1,
  };
  const stop = connectRun(
    "run",
    (state) => states.push(state),
    (status) => statuses.push(status),
    transport,
  );
  return { sockets, loads, states, statuses, stop };
}
const ready: Frame = { kind: "ready", runId: "run" };
const change = (number: string): Frame => ({
  kind: "event",
  runId: "run",
  eventNumber: number,
  event: { type: "checkpoint", runId: "run" },
});
const turn = () => new Promise<void>((resolve) => setImmediate(resolve));
async function until(check: () => boolean) {
  for (let tries = 0; tries < 100 && !check(); tries++) {
    await new Promise((resolve) => setTimeout(resolve, 2));
  }
  assert.equal(check(), true);
}

test("subscribe, buffer during fetch, discard covered events, then apply live events", async () => {
  const h = harness();
  try {
    assert.equal(h.loads.length, 0, "socket open is not proof of a server subscription");
    h.sockets[0].send(ready);
    h.sockets[0].send(change("1"));
    h.sockets[0].send(change("2"));
    h.loads[0].resolve({ ...initial, lastEventNumber: "1" });
    await turn();
    assert.deepEqual(h.states, [{ ...initial, lastEventNumber: "2" }]);
    h.sockets[0].send(change("3"));
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "3" });
  } finally {
    h.stop();
  }
});

test("a gap closes the old socket and reconnects from the full saved state", async () => {
  const h = harness();
  try {
    h.sockets[0].send(ready);
    h.loads[0].resolve(initial);
    await turn();
    h.sockets[0].send(change("2"));
    await until(() => h.sockets.length === 2);
    assert.equal(h.sockets[0].closed, true);
    h.sockets[1].send(ready);
    h.loads[1].resolve({ ...initial, lastEventNumber: "2" });
    await turn();
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "2" });
  } finally {
    h.stop();
  }
});

test("periodic state loads find a lost terminal update and stop reconnecting", async () => {
  const h = harness(5);
  try {
    h.sockets[0].send(ready);
    h.loads[0].resolve(initial);
    await until(() => h.loads.length === 2);
    const final = { ...initial, lastEventNumber: "1", status: "succeeded" };
    h.loads[1].resolve(final);
    await turn();
    assert.deepEqual(h.states.at(-1), final);
    assert.equal(h.sockets[0].closed, true);
    assert.equal(h.statuses.at(-1), "Complete");
  } finally {
    h.stop();
  }
});

test("a full buffer reconnects, and cleanup aborts pending fetches", async () => {
  const h = harness();
  h.sockets[0].send(ready);
  for (let number = 1; number <= 1025; number++) h.sockets[0].send(change(String(number)));
  await until(() => h.sockets.length === 2);
  assert.equal(h.loads[0].signal.aborted, true);
  h.sockets[1].send(ready);
  h.stop();
  assert.equal(h.loads[1].signal.aborted, true);
  h.loads[0].resolve(initial);
  h.loads[1].resolve(initial);
  await turn();
  assert.deepEqual(h.states, []);
});

test("a saved refresh behind live progress keeps the current unsaved suffix", async () => {
  const h = harness(5);
  try {
    h.sockets[0].send(ready);
    h.loads[0].resolve(initial);
    await turn();
    h.sockets[0].send(change("1"));
    h.sockets[0].send(change("2"));
    await until(() => h.loads.length === 2);
    h.sockets[0].send(change("3"));
    h.loads[1].resolve({ ...initial, lastEventNumber: "1" });
    await turn();
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "3" });
    assert.equal(h.sockets.length, 1);
    assert.equal(h.statuses.at(-1), "Live");
  } finally {
    h.stop();
  }
});

test("recovery replaces a higher preview and ignores old frames and fetch replies", async () => {
  const h = harness(5);
  try {
    h.sockets[0].send(ready);
    h.loads[0].resolve(initial);
    await turn();
    h.sockets[0].send(change("1"));
    h.sockets[0].send(change("2"));
    await until(() => h.loads.length === 2);
    const staleMessage = h.sockets[0].onmessage;
    h.sockets[0].send({ kind: "resync", runId: "run" });
    await until(() => h.sockets.length === 2);
    h.sockets[1].send(ready);
    h.loads[2].resolve({ ...initial, lastEventNumber: "1" });
    await turn();
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "1" });
    staleMessage?.call(h.sockets[0] as unknown as WebSocket,
      new MessageEvent("message", { data: JSON.stringify(change("99")) }));
    h.loads[1].resolve({ ...initial, lastEventNumber: "99" });
    await turn();
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "1" });
    h.sockets[1].send(change("2"));
    assert.deepEqual(h.states.at(-1), { ...initial, lastEventNumber: "2" });
  } finally {
    h.stop();
  }
});

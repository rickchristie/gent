import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { applyEvent, catchUp, Gap, terminal, type ContentPart, type Frame, type State } from "./src/state";

const initial: State = {
  runId: "run",
  lastEventNumber: "0",
  status: "pending",
  previews: [],
  answers: [],
};
const event = (number: string, type = "checkpoint"): Frame => ({
  kind: "event",
  runId: "run",
  eventNumber: number,
  event: { type, runId: "run" },
});

test("cursor handles duplicates, gaps, and numbers above JavaScript integer precision", () => {
  type Input = { state: State; frames: Frame[] };
  type Expected = { state?: State; error?: typeof Error };
  type Case = { name: string; input: Input; expected: Expected };
  const cases: Case[] = [
    {
      name: "duplicate at the snapshot cursor",
      input: {
        state: { ...initial, lastEventNumber: "2" },
        frames: [event("1"), event("2"), event("3")],
      },
      expected: { state: { ...initial, lastEventNumber: "3" } },
    },
    { name: "gap", input: { state: initial, frames: [event("2")] }, expected: { error: Gap } },
    {
      name: "wrong run",
      input: { state: initial, frames: [{ ...event("1"), runId: "wrong" }] },
      expected: { error: Error },
    },
    {
      name: "large cursor",
      input: {
        state: { ...initial, lastEventNumber: "9007199254740992" },
        frames: [event("9007199254740993")],
      },
      expected: { state: { ...initial, lastEventNumber: "9007199254740993" } },
    },
  ];
  for (const item of cases) {
    if (item.expected.error) {
      assert.throws(() => catchUp(item.input.state, item.input.frames), item.expected.error);
    } else
      assert.deepEqual(
        catchUp(item.input.state, item.input.frames),
        item.expected.state,
        item.name,
      );
  }
});

test("the reducer matches the server snapshot for a real recorded Gent run", () => {
  const fixture = JSON.parse(
    readFileSync(new URL("./trace-fixture.json", import.meta.url), "utf8"),
  );
  assert.deepEqual(catchUp(fixture.initial, fixture.frames), fixture.final);
  // A snapshot in the middle and an overlapping socket buffer must produce the same final view.
  const cut = Math.floor(fixture.frames.length / 2);
  const snapshot = catchUp(fixture.initial, fixture.frames.slice(0, cut));
  assert.deepEqual(catchUp(snapshot, fixture.frames.slice(cut - 2)), fixture.final);
});

test("an accepted empty answer replaces the preview instead of appending text", () => {
  const state: State = {
    ...initial,
    answers: [
      {
        contextId: "ctx",
        messageId: "old",
        sourceMessageIds: [],
        content: [{ Text: "old answer" }],
      },
    ],
  };
  const final: Frame = {
    ...event("1", "execution_content"),
    event: {
      type: "execution_content",
      runId: "run",
      contextId: "ctx",
      payload: {
        content: [],
        messageId: "final",
        sourceMessageIds: ["preview"],
      },
    },
  };
  assert.deepEqual(applyEvent(state, final), {
    ...state,
    lastEventNumber: "1",
    answers: [
      {
        contextId: "ctx",
        messageId: "final",
        sourceMessageIds: ["preview"],
        content: [],
      },
    ],
  });
});


test("binary parts keep their order through previews, acceptance, and snapshot catch-up", () => {
  const image = { MIMEType: "image/png", Data: "AQID" };
  const start: State = { ...initial, previews: [{
    id: "call", attemptId: "attempt", contextId: "ctx", status: "running", parts: [],
  }] };
  const chunk = (number: string, content: ContentPart[]): Frame => ({
    ...event(number), event: {
      type: "model_stream_chunk", runId: "run", modelCallId: "call", attemptId: "attempt",
      payload: { metaContent: [{ type: "intermediary", messageId: "preview", content }] },
    },
  });
  const frames = [chunk("1", [{ Text: "Before " }]), chunk("2", [{ Text: "image" }]),
    chunk("3", [image]), chunk("4", [{ Text: "After" }])];
  const state = catchUp(start, frames);
  assert.deepEqual(state, { ...start, lastEventNumber: "4", previews: [{
    ...start.previews[0], parts: [
      { type: "intermediary", messageId: "preview", content: [
        { Text: "Before " }, { Text: "image" }, image, { Text: "After" },
      ] },
    ],
  }] });
  assert.deepEqual(catchUp(catchUp(start, frames.slice(0, 2)), frames.slice(1)), state);
  const accepted = { ...event("5"), event: { type: "execution_content", runId: "run",
    contextId: "ctx", payload: { content: [image], messageId: "final",
      sourceMessageIds: ["preview"] } } };
  const final = applyEvent(state, accepted);
  image.Data = "changed";
  assert.deepEqual(final.answers, [{ contextId: "ctx", messageId: "final",
    sourceMessageIds: ["preview"], content: [{ MIMEType: "image/png", Data: "AQID" }] }]);
});

test("refusal ends the connection without an accepted answer", () => {
  const final = applyEvent(initial, { ...event("1"), event: {
    type: "execution_finished", runId: "run", payload: { status: "refused" },
  } });
  assert.deepEqual(final, { ...initial, status: "refused", lastEventNumber: "1" });
  assert.equal(terminal(final), true);
});

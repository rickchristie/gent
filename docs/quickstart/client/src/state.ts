// These are the JSON shapes of Gent's text, binary, and URL content values.
// Data is base64 on the JSON wire. Unknown native parts stay visible as an unsupported part.
export type ContentPart =
  | { Text: string }
  | { MIMEType: string; Data: string }
  | { URL: string; Detail?: string };
export type Part = { type: string; content: ContentPart[]; messageId: string };
export type Preview = {
  id: string;
  attemptId: string;
  contextId: string;
  status: string;
  parts: Part[];
};
export type Answer = {
  contextId: string;
  messageId: string;
  sourceMessageIds: string[] | null;
  content: ContentPart[];
};
export type State = {
  runId: string;
  lastEventNumber: string;
  status: string;
  previews: Preview[];
  answers: Answer[];
};
export type TraceEvent = {
  type: string;
  runId: string;
  modelCallId?: string;
  attemptId?: string;
  contextId?: string;
  error?: unknown;
  payload?: {
    status?: string;
    metaContent?: Part[];
    content?: ContentPart[];
    messageId?: string;
    sourceMessageIds?: string[];
  };
};
export type Frame =
  | { kind: "event"; runId: string; eventNumber: string; event: TraceEvent }
  | { kind: "ready" | "resync" | "heartbeat"; runId: string };

export class Gap extends Error {}

// Gent uses uint64. Decimal strings on the wire and BigInt here keep every cursor exact.
export function cursor(value: string): bigint {
  if (typeof value !== "string" || !/^(0|[1-9]\d*)$/.test(value)) {
    throw new Error("Invalid event cursor");
  }
  return BigInt(value);
}

function appendParts(parts: Part[], added: Part[]): Part[] {
  const next = structuredClone(parts);
  for (const part of added) {
    if (!part.content.length) continue;
    const last = next.at(-1);
    if (last?.type === part.type && last.messageId === part.messageId) {
      last.content.push(...structuredClone(part.content));
    } else next.push(structuredClone(part));
  }
  return next;
}

// This reducer matches the small BrowserState returned by this example's API.
// It does not claim to implement all fields of trace.Snapshot or restore a Gent RunView.
export function applyEvent(state: State, frame: Frame): State {
  if (frame.kind !== "event") throw new Error("Expected an event frame");
  if (frame.runId !== state.runId || frame.event.runId !== state.runId) {
    throw new Error("Wrong run");
  }
  const number = cursor(frame.eventNumber);
  const previous = cursor(state.lastEventNumber);
  if (number <= previous) return state;
  if (number !== previous + 1n) throw new Gap("Missing event: reload the saved state");
  const event = frame.event;
  const payload = event.payload ?? {};
  const next = { ...state, lastEventNumber: frame.eventNumber };
  if (event.type === "execution_started") next.status = "running";
  if (event.type === "execution_finished") next.status = payload.status!;
  if (event.type === "limit_exceeded") next.status = "limit_exceeded";
  if (event.type === "model_call_started") {
    next.previews = [
      ...state.previews,
      {
        id: event.modelCallId!,
        attemptId: event.attemptId!,
        contextId: event.contextId!,
        status: "running",
        parts: [],
      },
    ];
  }
  if (event.type === "model_stream_chunk" || event.type === "model_call_finished") {
    next.previews = state.previews.map((preview) => {
      if (preview.id !== event.modelCallId || preview.attemptId !== event.attemptId) return preview;
      if (event.type === "model_call_finished") {
        return { ...preview, status: event.error ? "failed" : "succeeded" };
      }
      return { ...preview, parts: appendParts(preview.parts, payload.metaContent ?? []) };
    });
  }
  if (event.type === "attempt_started" || event.type === "attempt_interrupted") {
    next.previews = state.previews.map((preview) => {
      const old =
        event.type === "attempt_started"
          ? preview.attemptId !== event.attemptId
          : preview.attemptId === event.attemptId;
      const sameContext = preview.contextId === event.contextId;
      return old && sameContext && preview.status === "running"
        ? { ...preview, status: "interrupted" }
        : preview;
    });
  }
  if (event.type === "execution_content") {
    const answer: Answer = {
      contextId: event.contextId!,
      messageId: payload.messageId!,
      sourceMessageIds: payload.sourceMessageIds ?? [],
      content: structuredClone(payload.content ?? []),
    };
    const exists = state.answers.some((item) => item.contextId === answer.contextId);
    next.answers = exists
      ? state.answers.map((item) => (item.contextId === answer.contextId ? answer : item))
      : [...state.answers, answer];
  }
  // Even events without a visible change must advance the canonical cursor.
  return next;
}

export function catchUp(snapshot: State, buffered: Frame[]): State {
  let next = snapshot;
  for (const frame of buffered) next = applyEvent(next, frame);
  return next;
}

export function terminal(state: State): boolean {
  return ["succeeded", "refused", "failed", "canceled", "limit_exceeded"].includes(state.status);
}

import { applyEvent, catchUp, cursor, terminal, type Frame, type State } from "./state";

type Socket = Pick<WebSocket, "onmessage" | "onclose" | "onerror" | "close">;
export type Transport = {
  open: (runId: string) => Socket;
  load: (runId: string, signal: AbortSignal) => Promise<State>;
  pollMs: number;
  retryMs: number;
};

export const browserTransport: Transport = {
  open(runId) {
    const url = new URL(`/api/runs/${encodeURIComponent(runId)}/events`, location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    return new WebSocket(url);
  },
  async load(runId, signal) {
    const response = await fetch(`/api/runs/${encodeURIComponent(runId)}/state`, {
      signal,
      cache: "no-store",
      credentials: "same-origin",
    });
    if (!response.ok) throw new Error(`State request failed: ${response.status}`);
    return response.json();
  },
  pollMs: 5000,
  retryMs: 500,
};

export function connectRun(
  runId: string,
  publish: (state: State) => void,
  status: (text: string) => void,
  transport: Transport = browserTransport,
): () => void {
  let stopped = false;
  let generation = 0;
  let attempt = 0;
  let closeCurrent = () => {};
  let retry: ReturnType<typeof setTimeout>;

  function connect() {
    if (stopped) return;
    const current = ++generation;
    const socket = transport.open(runId);
    let state: State | null = null;
    let buffer: Frame[] = [];
    let bufferNumbers = new Set<string>();
    let bufferBytes = 0;
    let savedCursor = 0n;
    let loading = false;
    let ready = false;
    let controller: AbortController | null = null;
    let fetchTimer: ReturnType<typeof setTimeout>;
    let poll: ReturnType<typeof setTimeout>;
    let watch: ReturnType<typeof setTimeout>;
    const valid = () => !stopped && generation === current;
    status("Connecting");

    closeCurrent = () => {
      clearTimeout(poll);
      clearTimeout(watch);
      clearTimeout(fetchTimer);
      controller?.abort();
      socket.onmessage = socket.onclose = socket.onerror = null;
      socket.close();
      buffer = [];
      bufferNumbers.clear();
    };

    function reconnect(reason: string) {
      if (!valid()) return;
      generation++;
      closeCurrent();
      status(`Reconnecting: ${reason}`);
      const delay = Math.min(10000, transport.retryMs * 2 ** Math.min(attempt++, 5));
      retry = setTimeout(connect, delay);
    }

    function watchConnection() {
      clearTimeout(watch);
      watch = setTimeout(() => reconnect("no server heartbeat"), 20000);
    }

    async function refresh() {
      if (!valid() || !ready || loading) return;
      loading = true;
      controller = new AbortController();
      fetchTimer = setTimeout(() => controller?.abort(), 10000);
      status("Loading saved state");
      try {
        const snapshot = await transport.load(runId, controller.signal);
        if (!valid()) return;
        if (snapshot.runId !== runId) throw new Error("Wrong snapshot run");
        if (cursor(snapshot.lastEventNumber) < savedCursor) {
          throw new Error("Saved cursor moved backwards");
        }
        savedCursor = cursor(snapshot.lastEventNumber);
        // Keep this connection's unsaved suffix across refreshes. On reconnect, both the
        // preview and this buffer start again from the saved state, even at a lower cursor.
        state = catchUp(snapshot, buffer);
        buffer = buffer.filter((frame) => frame.kind === "event" &&
          cursor(frame.eventNumber) > savedCursor);
        bufferNumbers = new Set(buffer.flatMap((frame) =>
          frame.kind === "event" ? [frame.eventNumber] : []));
        bufferBytes = buffer.reduce((size, frame) => size + JSON.stringify(frame).length * 2, 0);
        loading = false;
        attempt = 0;
        publish(state);
        // Only a full saved snapshot proves that the entire terminal batch is present.
        if (terminal(snapshot)) {
          stopped = true;
          closeCurrent();
          status("Complete");
          return;
        }
        status("Live");
        clearTimeout(poll);
        poll = setTimeout(() => void refresh(), transport.pollMs);
      } catch (error) {
        reconnect(error instanceof Error ? error.message : String(error));
      } finally {
        clearTimeout(fetchTimer);
      }
    }

    socket.onmessage = (message) => {
      if (!valid()) return;
      try {
        const frame: Frame = JSON.parse(String(message.data));
        if (frame.runId !== runId) throw new Error("Wrong socket run");
        watchConnection();
        if (frame.kind === "ready") {
          ready = true;
          void refresh();
          return;
        }
        if (frame.kind === "resync") {
          reconnect("server requested a fresh subscription");
          return;
        }
        if (frame.kind === "heartbeat") return;
        if (frame.kind !== "event") throw new Error("Unknown frame kind");
        if (cursor(frame.eventNumber) > savedCursor && !bufferNumbers.has(frame.eventNumber)) {
          buffer.push(frame);
          bufferNumbers.add(frame.eventNumber);
          bufferBytes += String(message.data).length * 2;
        }
        if (buffer.length > 1024 || bufferBytes > 2 * 1024 * 1024) {
          reconnect("snapshot buffer is full");
          return;
        }
        if (!state || loading) return;
        state = applyEvent(state, frame);
        publish(state);
        if (terminal(state)) void refresh();
      } catch (error) {
        reconnect(error instanceof Error ? error.message : String(error));
      }
    };
    socket.onclose = () => reconnect("socket closed");
    socket.onerror = () => reconnect("socket error");
    watchConnection();
  }

  connect();
  return () => {
    stopped = true;
    generation++;
    clearTimeout(retry);
    closeCurrent();
  };
}

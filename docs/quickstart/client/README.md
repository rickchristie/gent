# React client for the saved Gent API

This is the complete React project for scenario 3. It reconnects to a run by its URL ID and
rebuilds the same display state after a browser refresh. Use Node.js 22.12 or later.

Inside this folder:

```bash
npm ci
npm test
npm run build
```

Start the Go project from scenario 2. With this folder at `../client` relative to that server,
open `http://127.0.0.1:8080`. Select **Start order lookup**, then refresh while text arrives.
The local model needs no credentials. The accepted answer must appear once without a repeated
preview prefix. For development, start the Go server and run `npm run dev` here. Vite proxies
both HTTP and WebSocket API calls to port 8080.

## Data flow

1. Open the WebSocket and install its message handler.
2. Buffer events. Wait for `ready`, which confirms the server subscription exists.
3. Load the latest full display state with its `lastEventNumber` cursor.
4. Skip buffered events at or below that cursor. Apply the next contiguous event numbers.
5. Drain the buffer in the same JavaScript task, then apply new events directly.
6. On a gap, overflow, or disconnect, reconnect and repeat. Periodic snapshots also catch a lost
   final event that has no later event to expose the gap.

`state.ts` is the reducer for the server's small `BrowserState` format. It does not implement
every field of Gent's trace snapshot. It uses `BigInt` with decimal strings, so cursor comparison
stays exact above JavaScript's integer precision limit. Unknown display event types still advance
the cursor. Run IDs are checked before any state is applied.

`connection.ts` owns one socket and one state request at a time. It bounds the buffer to 1,024
events or 2 MiB, aborts old requests, and rejects stale responses by connection generation.
It stops only after a complete terminal snapshot. A terminal event can precede other events in
the same accepted batch, so that event alone is not enough to close the feed.

`useRun.ts` closes sockets and timers when the run changes or the component unmounts. Every event
is reduced, but rendering is limited to once per animation frame. `main.tsx` stores the run ID in
the URL and replaces only previews named by the accepted answer's `SourceMessageIds`.

## Verify

`npm test` covers overlapping snapshots, duplicate and missing events, large cursors, wrong run
IDs, buffer overflow, reconnect, cleanup, and a lost terminal event. The reducer's complete result
is also compared with a real Gent snapshot in `trace-fixture.json`. The Go HTTP test generates
that fixture; do not edit its event fields by hand.

The repository browser check at `docs/quickstart/tests/gent-streaming.mjs` starts the real Go server and
checks refresh, process restart, and recovery. It uses the production React build.

The browser preserves each ordered content part. Text renders as escaped text; common image bytes
render from their base64 JSON values; PDF bytes have a download link. Remote references remain text
and do not cause an automatic fetch. Refusal is a terminal state with no accepted answer. The saved
snapshot and live event reducer use the same content grouping, including empty accepted output.

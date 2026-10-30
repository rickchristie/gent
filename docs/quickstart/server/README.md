# Saved Gent streaming API

This is the complete Go project for scenarios 2 and 4. It saves accepted Gent batches in bbolt,
provides a full saved display state, and sends live events over WebSocket. Live previews can precede
persistence; the final accepted answer waits for the terminal save. The database is the source of
saved state. A disconnected browser does not stop the agent.

## Run

Use Go 1.24.10 or later. Inside this folder:

```bash
CGO_ENABLED=0 go run .
```

The module replacement points to the Gent checkout three levels above this folder. If you
downloaded the ZIP, set the path to a checkout with the API revision shown in the HTML guide:

```bash
go mod edit '-replace=github.com/rickchristie/gent=/absolute/path/to/gent'
go mod tidy
CGO_ENABLED=0 go run .
```

Without `OPENAI_API_KEY`, a local model streams a fixed order answer. No model API call is made.
To use OpenAI, set both `OPENAI_API_KEY` and `GENT_MODEL`. The same agent, recorder, and API apply.

Build the React project from scenario 3 next to this folder as `../client`. The server serves
`../client/dist` at `http://127.0.0.1:8080`. Set `UI_DIR` if the build is elsewhere. Use `API_ADDR`
to change the listening address and `RUN_DB` to change the database path (default `runs.db`).

## API and cursor

| Route | Result |
| --- | --- |
| `POST /api/runs` | JSON question in; HTTP 202 with `runId` out. |
| `GET /api/runs/{id}/state` | A complete `BrowserState` and `lastEventNumber`. |
| `GET /api/runs/{id}/events` | WebSocket frames: `ready`, `event`, `heartbeat`, `resync`. |
| `POST /api/runs/{id}/resume` | HTTP 202; HTTP 409 if that run has an active worker. |

Example body for a new run: `{"question":"What is the status of order-1001?"}`.

The subscription exists before `ready` is sent. The browser then loads a snapshot while it
buffers events. Each frame has the run ID. Event frames have a decimal-string `eventNumber`.
Snapshot `lastEventNumber` is the saved position in that sequence. It can lag the live preview.
The server also sends the unsaved prefix already projected before the subscription was installed.
The nested trace event also has Gent's numeric event number; the browser uses the string envelope
to avoid loss of precision in JavaScript. Every cursor position is sent, even for invisible events.

`BrowserState` is a small display view: status, model previews, and accepted answers. It is not a
serialized recovery object. The database holds the full `gent.RunView` through `CodecRegistry`.
`RunView.LastAppliedEventNumber` covers saved progress beyond `Checkpoint.EventNumber`.

## Restart and resume

Start an order lookup in React. Stop the service with Ctrl+C while its preview arrives. Restart
with the same database, keep the browser's `?run=...` URL, and select **Resume saved run**.
Do this within the original two-minute deadline. Recovery keeps the original deadline and limits.

`recover.go` binds a fresh recorder to `Runner.Resume` and attaches `seq.Follow` through Configure.
The codec registry and agent are also fresh. The browser discards the previous connection and
preview, resets to the saved cursor even if it is lower, and rejects old callbacks and fetches. Keep
`WithRecoveryVersion` on the original agent so the new worker can check its
behavior. A terminal run returns the saved outcome without another model call.

The writer's owner number is checked in the same database transaction as the state write. This
prevents an old owner from accepting new batches. The API allows only one active worker per run.
bbolt also locks its file to one process; this is not a shared database for multiple servers.

The sample tool only reads data, so its recovery policy allows retry. For external writes, use
receipts or a stable `ToolCallId` as an idempotency key. A tool-ready checkpoint does not prove
whether an external operation completed. Do not turn every unknown outcome into a retry.

## Scope

The example keeps one root context and at most 16 active workers. Each run has six iterations,
1,024 output tokens per model call, and a two-minute worker deadline. It keeps text previews so
the snapshot and event reducer can be compared without truncation differences.

The default pending-save policy groups up to 256 events or 1 MiB before an early progress save.
It also saves at the existing model, tool, and terminal boundaries. Early progress advances the
saved cursor without replacing the control checkpoint. Configure these with `CheckpointPolicy`.
The live socket queue is limited to 256 events, 2 MiB, and a 5-second delivery wait. Socket writes
have a separate 5-second deadline. Delivery failures do not change the result of a database save.

For clarity, each grouped save rewrites a full `RunView`. A larger store should append batches and
update its materialized view atomically. Keep Gent's digest, duplicate, ordering, and fencing
checks. A socket failure must not change a successful database save into a failed save.

The service binds to localhost. Before public use, add authentication and authorization on every
run route. Apply the same display redaction policy to snapshots and events. The WebSocket uses
Gorilla's default same-host Origin check, one reader, one writer, bounded queues, and deadlines.

## Verify

```bash
go test ./... -count=1 -timeout 300s
CGO_ENABLED=0 go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 300s
```

Tests use the real Gent loop, bbolt transactions, reopened databases, HTTP, and WebSocket with
the local model. They test batch rejection, duplicate acceptance, owner fencing, recovery from
tool-ready and tool-completed checkpoints, and terminal reuse. No provider credentials are used.

To regenerate the React parity fixture from a real recorded run:

```bash
GENT_TRACE_FIXTURE=../client/trace-fixture.json go test ./... -count=1 -timeout 300s
```

Set `GOCACHE` to a writable folder when needed. HTTP tests require local listening sockets.

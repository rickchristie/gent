"""Coding steps for the saved stream, React client, and recovery examples."""

from pathlib import Path

HERE = Path(__file__).resolve().parent


def snippet(path, start=None, end=None):
    text = (HERE / path).read_text()
    begin = text.index(start) if start else 0
    finish = text.index(end, begin) if end else len(text)
    return text[begin:finish].rstrip()


def step(key, short, title, intro, action, path, start, end, notes, result, extra=""):
    return dict(id=key, short=short, title=title, intro=intro, action=action,
                location=path, code=snippet(path, start, end), notes=notes,
                done=result, extra=extra)


SERVER = {
    "id": "server", "name": "Saved streaming API", "tag": "02 / GO + DATABASE",
    "summary": "Save accepted run state, then send numbered events to the browser.",
    "goal": "a Go API with a durable run store, full-state reads, and a WebSocket feed.",
    "project": "server",
    "steps": [
        step("model", "Configure the agent", "Keep the normal agent. Add recovery identity.",
             "This is the next version of the simple order agent. A real database stores its "
             "accepted state. A local demo model streams without API credentials.",
             "Create the server folder from Complete files. Add agent.go and keep "
             "WithRecoveryVersion on the original agent, before the first run is saved.",
             "server/agent.go", "type OrderInput", "// The demo provider", [
                 ("WithRecoveryVersion", "Name the application behavior that a fresh worker "
                  "must match. Change the version when tool or prompt behavior changes."),
                 ("Model choice", "main.go uses DemoModel without a key. Set OPENAI_API_KEY "
                  "and GENT_MODEL to use models.NewOpenAI instead."),
             ], "The same agent can run now and continue on a fresh worker later."),
        step("save", "Save to the database", "Implement gent.Recorder.Save.",
             "Gent passes a SaveBatch at each acceptance boundary, including streamed progress. "
             "A batch can contain events, history changes, runtime counters, and a checkpoint.",
             "Add store.go. Put this method on Writer. Commit all state in one database "
             "transaction and return nil only after the transaction succeeds.",
             "server/store.go", "// Save is the Gent", None, [
                 ("gent.ApplyBatch", "Checks the previous cursor, consecutive event numbers, "
                  "batch digest, and duplicate identity. An identical retry is safe."),
                 ("CodecRegistry.Marshal / Unmarshal", "Keep concrete Go content and metadata "
                  "types. Ordinary JSON decoding of a RunView is not a restoration format."),
                 ("Writer fence", "The transaction rejects a worker whose owner number is old. "
                  "Canceling a context alone cannot prevent an old worker from saving."),
                 ("Database choice", "bbolt is a real file database with atomic transactions. "
                  "This short-run example rewrites one full RunView; a larger store should keep "
                  "append-only batches and maintain its view in the same transaction."),
             ], "A saved view and its cursor always describe the same committed data."),
        step("observe", "Attach the recorder", "Save first. Observe accepted events second.",
             "trace.Sequencer receives accepted events through WithObserver. Gent calls the "
             "recorder before it publishes those events. Raw provider callbacks are too early.",
             "In API.start in runtime.go, create one sequencer per active run, then use "
             "WithRecorder, WithObserver, and WithCodecs when you create the execution context.",
             "server/runtime.go", "\t\t\tdata := gent.NewBasicLoopData", "\t\tif runErr != nil", [
                 ("trace.NewSequencer(runID, displayConfig(codecs))", "Create seq before this "
                  "block. displayConfig enables preview chunks and accepted answer content."),
                 ("Worker context", "The worker belongs to the service, not to the WebSocket "
                  "request. Refreshing the browser must not cancel the agent."),
                 ("PersistenceError", "A failed or uncertain save stops new external work. "
                  "Reconcile the same batch with RetrySave, or load the accepted database view."),
             ], "Database acceptance controls what a connected browser is allowed to see."),
        step("state", "Provide the state API", "Return a complete display state and its cursor.",
             "GET /api/runs/{id}/state loads one committed RunView, rebuilds the trace, and "
             "returns the subset needed by this React client. It also works after a restart.",
             "Add the snapshot method in runtime.go. Add BrowserState and browserState from "
             "view.go. Keep lastEventNumber in the same response as the content it covers.",
             "server/runtime.go", "func (a *API) snapshot", None, [
                 ("trace.RestoreSequencer", "Rebuilds display state from saved activity. It "
                  "does not resume the agent or call a model."),
                 ("Snapshot.LastEventNumber", "The cursor used for this browser view. view.go "
                  "sends it as a decimal string so JavaScript can compare it with BigInt."),
                 ("Display versus recovery", "BrowserState holds previews, accepted answers, "
                  "and status. The database keeps the full RunView, including recovery data."),
             ], "The frontend can replace its display state with one consistent response."),
        step("subscribe", "Open the WebSocket", "Subscribe before sending the ready frame.",
             "The browser's open event only proves the WebSocket handshake finished. Send a "
             "ready frame after the server subscription exists, then allow the state request.",
             "Add this subscription block inside streamEvents in http.go. The full file "
             "selects the active sequencer or restores an idle run from the database first.",
             "server/http.go", "\t// Install the bounded subscription", "\ttick :=", [
                 ("SubscribeWithConfig", "Use a bounded queue. OverflowCloseSubscriber closes "
                  "a slow subscription instead of blocking the agent or silently losing events."),
                 ("ready", "The client starts buffering as soon as it receives events. It "
                  "fetches the snapshot only after it receives ready for the correct run ID."),
                 ("Connection ownership", "One goroutine reads control messages and one writes "
                  "data. Deadlines, ping/pong, unsubscribe, and Close release socket resources."),
             ], "Every event after ready is covered by the subscription or a later full reload."),
        step("events", "Send accepted changes", "Forward every numbered event.",
             "A client must see every cursor position, even when an event has no visible text. "
             "Do not filter the stream down to token events and then require contiguous numbers.",
             "Continue streamEvents with this loop. Send trace.Event inside an event frame. "
             "On a closed subscription, request a fresh subscription and snapshot.",
             "server/http.go", "\ttick :=", None, [
                 ("eventFrame", "Copies Event.EventNumber into a decimal string envelope. "
                  "The payload still contains the display event and its call identities."),
                 ("Socket failure", "Close the connection and let the client catch up. A "
                  "failed socket write must not turn a committed database save into a failure."),
                 ("Worker changes", "An idle subscriber reconnects when a new worker starts. "
                  "A finished worker releases its sequencer and leaves the database view intact."),
             ], "The socket sends accepted changes. The database remains the source of truth."),
        step("run", "Run the API", "Expose the routes and start the service.",
             "The complete server includes HTTP handlers, the database adapter, the model factory, "
             "and service shutdown. It listens on 127.0.0.1:8080 by default.",
             "Add these routes to http.go and main.go from Complete files. From the repository "
             "root, run: cd docs/quickstart/server, then CGO_ENABLED=0 go run .",
             "server/http.go", "func (a *API) routes", "func writeJSON", [
                 ("POST /api/runs", "Send {\"question\":\"Check order-1001\"}. The response "
                  "is HTTP 202 with runId. The service runs the agent in its own worker context."),
                 ("GET /api/runs/{id}/state", "Returns BrowserState with Cache-Control: no-store."),
                 ("GET /api/runs/{id}/events", "Upgrade to WebSocket. Frames are ready, event, "
                  "heartbeat, or resync."),
                 ("POST /api/runs/{id}/resume", "Starts a fresh worker for the saved run. "
                  "An already active worker returns HTTP 409."),
             ], "Use scenario 3 for the React UI. Use scenario 4 to restart an interrupted run.",
             '<p class="scope-note">This local example uses one database process and at most 16 '
             'active workers. Before exposing it to users, authenticate every route and authorize '
             'each run. Use the same display redaction rules for snapshots and events.</p>'),
    ],
}

CLIENT = {
    "id": "react", "name": "React streaming client", "tag": "03 / REACT + TYPESCRIPT",
    "summary": "Connect, buffer, load state, catch up, and keep listening across refreshes.",
    "goal": "a React page that can refresh during an agent run and keep the same accepted state.",
    "project": "client",
    "steps": [
        step("cursor", "Define the cursor", "Use the event number, not the iteration count.",
             "trace.Event.EventNumber and trace.Snapshot.LastEventNumber share one run-wide "
             "sequence. RunView.LastAppliedEventNumber is the equivalent cursor in saved state.",
             "Create src/state.ts. Add the types from Complete files and this cursor parser. "
             "Our API sends cursor values as decimal strings, so use BigInt for comparisons.",
             "client/src/state.ts", "export class Gap", "function appendParts", [
                 ("Checkpoint.EventNumber", "This can be older than the saved progress cursor. "
                  "It tells the worker where to continue, not the browser where to deduplicate."),
                 ("HistoryRevision / draft Revision", "These identify history changes or local "
                  "edits. They are not the cursor for the complete event feed."),
                 ("Run ID + cursor", "Reject another run's snapshot or event. Compare cursors "
                  "only within the same run."),
             ], "The browser can compare exact event numbers, including values above 2^53.",
             '<div class="flow-strip">Subscribe → ready → buffer → snapshot C → discard ≤ C '
             '→ apply C+1 → listen</div>'),
        step("reduce", "Apply one change", "Make the reducer strict about order.",
             "Drop events already covered by the current state. Apply only the next number. "
             "If a number is missing, request a fresh subscription and complete snapshot.",
             "Add applyEvent and catchUp to state.ts. This reducer updates the BrowserState "
             "subset from scenario 2; it is not a general reducer for every trace.Snapshot field.",
             "client/src/state.ts", "export function applyEvent", "export function terminal", [
                 ("number ≤ previous", "Duplicate delivery is normal after a snapshot overlaps "
                  "with a socket buffer. Ignore it without changing text or counters."),
                 ("number = previous + 1", "Apply the change and advance the cursor together. "
                  "Unknown display event types still advance the cursor."),
                 ("execution_content", "Replace the accepted answer with Content. Do not append "
                  "it to the preview. SourceMessageIds identify the previews it replaces."),
                 ("Attempt identity", "Keep retries separate with ModelCallId plus AttemptId. "
                  "This uses one root context; child context trees need a matching reducer."),
             ], "Duplicate events cannot duplicate text. A gap triggers a fresh state read."),
        step("buffer", "Connect and buffer", "Keep buffering while the snapshot is loading.",
             "A token can arrive while the HTTP state request is in flight. Buffer it instead "
             "of applying it to incomplete state. Limit the queue to keep memory use bounded.",
             "Create src/connection.ts from Complete files. Add this message handler inside "
             "connect(). It waits for ready and buffers events until the snapshot is installed.",
             "client/src/connection.ts", "    socket.onmessage = (message)",
             "    socket.onclose = ()", [
                 ("ready before fetch", "WebSocket open alone is not sufficient. The ready "
                  "message confirms the server has installed the accepted-event subscription."),
                 ("While loading", "Continue adding events to the queue. Never pause the socket "
                  "listener while awaiting fetch."),
                 ("Bounded buffering", "At 1,024 events or 2 MiB, reconnect and load a fresh "
                  "snapshot. The new snapshot covers the discarded accepted events."),
             ], "Events have one ordered path before, during, and after the snapshot request."),
        step("catchup", "Install and catch up", "Replace state, skip duplicates, drain the buffer.",
             "Suppose the buffer contains events 80, 81, and 82. A complete snapshot through 81 "
             "already includes 80 and 81. Apply only 82, then accept 83 from the live socket.",
             "Add refresh() inside connect(). After fetch returns, catchUp installs the snapshot "
             "and applies all later buffered events in the same JavaScript task.",
             "client/src/connection.ts", "    async function refresh",
             "    socket.onmessage = (message)", [
                 ("One in-flight state read", "The loading flag prevents overlapping fetches. "
                  "Generation checks reject responses from a replaced connection."),
                 ("Periodic refresh", "Reload every five seconds, even when no gap is visible. "
                  "This finds a lost final update that has no later event to reveal the gap."),
                 ("Full terminal snapshot", "A terminal display event can precede other events "
                  "in its batch. Stop only after a full saved snapshot confirms completion."),
             ], "The page reaches the same view as the server without applying any event twice."),
        step("hook", "Connect React", "Own the connection in an Effect.",
             "The connection belongs to the selected run. Changing the run or unmounting the "
             "component must close the socket, abort fetches, and clear retry and poll timers.",
             "Create src/useRun.ts. Keep event processing in connectRun. Use requestAnimationFrame "
             "to render the latest applied state at most once per browser frame.",
             "client/src/useRun.ts", None, None, [
                 ("[runId]", "Create a new connection when the selected run changes. React "
                  "StrictMode can run setup and cleanup twice during development."),
                 ("Cleanup", "connectRun returns a stop function. It closes the socket, "
                  "aborts pending reads, clears timers, and rejects stale callbacks."),
             ], "The UI can switch runs, unmount, and refresh without leaving old listeners."),
        step("render", "Render the answer", "Separate previews from accepted answers.",
             "The API includes provisional metadata in previews and accepted execution text in "
             "answers. A model's completed candidate is not necessarily an accepted answer.",
             "Create src/main.tsx. Render the parts that have not been replaced by an answer's "
             "SourceMessageIds, then render each accepted answer by ContextId.",
             "client/src/main.tsx", "          {state.previews.map", "        </>", [
                 ("Accepted text", "The final answer can differ from the preview, including "
                  "formatting or an empty accepted answer. Replace it as a complete value."),
                 ("Custom model adapters", "Set CandidateSource.MessageIds to the same local "
                  "IDs used by the candidate's preview chunks. Gent scopes those IDs per call."),
                 ("Browser refresh", "main.tsx keeps runId in the URL query string. A refresh "
                  "rejoins that run and uses the same subscribe-buffer-snapshot sequence."),
             ], "The accepted answer replaces its preview. The answer appears only once."),
        step("run", "Build and try a refresh", "Run the React client against scenario 2.",
             "The Go server can serve the built React app on the same origin as its API. "
             "The Vite development proxy also forwards HTTP and WebSocket requests to that API.",
             "Create the client files from Complete files. Run npm ci and npm run build in "
             "docs/quickstart/client. Start the Go server, then open http://127.0.0.1:8080.",
             "client/vite.config.ts", None, None, [
                 ("Try it", "Select Start order lookup. Refresh while the preview is arriving. "
                  "The accepted answer appears once, with no repeated prefix."),
                 ("Development", "Use npm run dev for hot reload while the Go server runs. "
                  "The proxy keeps HTTP and WebSocket calls on the browser's current origin."),
                 ("Repeatable checks", "npm test checks duplicates, gaps, queue overflow, cleanup, "
                  "a lost finish, and parity with events from a real recorded Gent run."),
             ], "The same page works for a new run, a refresh, or a resumed run."),
    ],
}

RECOVERY = {
    "id": "recovery", "name": "Resume a saved run", "tag": "04 / FRESH WORKER",
    "summary": "Load the durable run, bind fresh objects, and continue from its saved checkpoint.",
    "goal": "a replacement worker that continues the same run without starting the task over.",
    "project": "server",
    "steps": [
        step("load", "Load accepted state", "Read the saved RunView from scenario 2.",
             "A browser refresh and a worker restart are different operations. The browser "
             "rebuilds display state. A replacement worker needs the full saved RunView.",
             "Create fresh model and agent objects. Register the agent's codecs before loading "
             "the database bytes. Call Store.load with the existing run ID.",
             "server/store.go", "func (s *Store) read", "type Writer", [
                 ("RegisterCodecs before decode", "newAgent(model).RegisterCodecs(codecs) "
                  "registers the tool and agent state types. Use that registry in openStore."),
                 ("Keep progress beyond the checkpoint", "Do not trim Activity to "
                  "Checkpoint.EventNumber. Saved model output and accounting may be newer."),
             ], "You have the complete accepted run, including its continuation checkpoint."),
        step("claim", "Replace the writer", "Fence the old worker before continuing.",
             "The previous worker can still be saving after its context is canceled. A new owner "
             "number makes its next database write fail inside the save transaction.",
             "Call Store.claim for the existing run ID. Pass the returned Writer to the restored "
             "context. The Save method from scenario 2 checks this number on every transaction.",
             "server/store.go", "// A new owner fences", "func (s *Store) read", [
                 ("Same transaction", "Checking the fence separately from the data write leaves "
                  "a race. bbolt checks the owner and writes the complete view in one update."),
                 ("Multiple processes", "bbolt locks its database file to one process. A shared "
                  "SQL store needs an equivalent transactional lease or fencing check."),
             ], "The replacement worker is the only writer allowed to advance this run."),
        step("tools", "Handle uncertain tools", "Do not blindly repeat an external write.",
             "A process can stop after a tool changes an external system but before its result "
             "is saved. A tool-ready checkpoint alone cannot prove whether the tool completed.",
             "Set Recovery(recoverOrder) on the lookup tool builder in agent.go. This allows retry for "
             "lookup_order because it is a read. Return unresolved for an unknown write outcome.",
             "server/agent.go", "// The order lookup reads", None, [
                 ("ToolRecoveryCompleted", "Return the complete saved result when an external "
                  "receipt proves the operation finished."),
                 ("ToolRecoveryRetryAllowed", "Use this only when retry is safe, such as this "
                  "read-only tool or an operation protected by a stable idempotency key."),
                 ("ToolRecoveryUnresolved", "Keep the unknown outcome visible to the model. "
                  "Gent does not promise exactly-once external side effects."),
                 ("ToolIdentityFromContext", "Pass its stable ToolCallId to your external "
                  "idempotency mechanism. A new attempt ID does not mean a new logical operation."),
             ], "Completed steps are reused. Each unknown outcome has an explicit policy."),
        step("resume", "Resume the runner", "Call Resume with fresh runtime bindings.",
             "Rebuild the trace with trace.RestoreSequencer(saved, displayConfig(codecs)), then "
             "pass the new recorder, codecs, and observer to Runner.Resume. Tools own recovery.",
             "Add recover.go and call resumeSaved from the resume branch in API.start. Reuse "
             "the stored run ID. Do not create a new Task or call Execute on a new context.",
             "server/recover.go", "func resumeSaved", None, [
                 ("Runner.Resume", "Configures the fresh agent, checks compatibility, restores "
                  "state, and continues from the saved control boundary."),
                 ("Cursor after recovery", "The next event follows RunView.LastAppliedEventNumber, "
                  "even if the checkpoint cursor is older. The frontend protocol stays the same."),
                 ("Preserved limits", "Counters, original limits, and absolute deadlines survive "
                  "restoration. Restarting the worker does not extend the original deadline."),
                 ("Terminal run", "A saved terminal run returns its recorded outcome without "
                  "another model request or another accepted answer event."),
             ], "The new worker continues the same saved run and publishes higher event numbers."),
        step("try", "Test an interruption", "Restart the process with the same database.",
             "The example distinguishes a stopped worker from an intentional terminal run "
             "cancellation. Its database keeps the last accepted progress and checkpoint.",
             "Start an order lookup in the React page. Stop the Go process with Ctrl+C during "
             "the preview, restart it with the same RUN_DB, then select Resume saved run.",
             "server/http.go", "func (a *API) resumeRun", "func (a *API) getState", [
                 ("Keep the run ID", "The React page keeps it in the URL. POST resume uses "
                  "that ID and returns HTTP 409 if a worker is already active."),
                 ("Worker cancellation", "Stopping the service interrupts the worker. "
                  "ExecutionContext.CancelRun causes intentional terminal cancellation."),
                 ("Compatibility failures", "Keep the original recovery version and declared "
                  "configuration. Handle ErrIncompatibleCheckpoint instead of forcing a restore."),
                 ("Tests", "The server tests reopen the database with fresh objects and resume "
                  "both tool-ready and tool-completed states. They also check terminal reuse."),
             ], "The React page catches up with the new worker through the same API."),
    ],
}

EXTRA_SCENARIOS = [SERVER, CLIENT, RECOVERY]

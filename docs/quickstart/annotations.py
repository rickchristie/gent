"""Bind reading notes to source text so edits cannot leave arrows on the wrong line."""

from pathlib import Path

HERE = Path(__file__).resolve().parent

# Each entry matches one note, in its written order. None keeps a related concept separate
# from line-specific notes. A range uses two unique source lines, inclusive.
TARGETS = {
    "basic/setup": ['"github.com/rickchristie/gent"',
                    ('"github.com/rickchristie/gent/agents/react"',
                     '"github.com/rickchristie/gent/executor"'),
                    ('"github.com/rickchristie/gent/schema"',
                     '"github.com/rickchristie/gent/toolchain"')],
    "basic/model": ["key, modelName :=", "model := models.NewOpenAI", None],
    "basic/tool": ['type OrderInput struct', "return gent.NewToolFunc(",
                   ('if input.OrderID !=', 'return OrderStatus{OrderID:')],
    "basic/agent": [("chain :=", "chain.RegisterTool"),
                    ("loop := react.NewAgent", "WithCriticalRules"),
                    ("term :=", '"Give a short answer'), "return executor.NewRunner"],
    "basic/task": ["data :=", "execCtx :=", ("if limits[i].Key", "execCtx.SetLimits")],
    "basic/execute": [("result, err :=", 'return "", err'),
                      ("if result.TerminationReason", 'return "", fmt.Errorf("run ended'),
                      ("answer, ok :=", "return answer.Text")],
    "basic/run": ["runner := newRunner", ("ctx, cancel :=", "defer cancel()"), None],
    "server/model": ["WithRecoveryVersion", "func newAgent(model gent.Model)"],
    "server/save": ["next, err := gent.ApplyBatch", "data, err := w.store.codecs.Marshal",
                    ("if bucket == nil", "return errStaleWriter"), "return w.store.db.Update"],
    "server/observe": ["gent.WithRecorder(writer), gent.WithObserver(seq)",
                       "ctx := gent.NewExecutionContext(worker", "_, runErr = runner.Execute"],
    "server/state": ["seq, err := trace.RestoreSequencer", "return browserState",
                     "saved, err := a.store.load"],
    "server/subscribe": [("updates, unsubscribe :=", "BufferSize:"),
                         'Frame{Kind: "ready"', ("go func()", "conn.ReadMessage()")],
    "server/events": ["socketWrite(conn, eventFrame(event))", 'log.Printf("write event:',
                      ("if a.active(id) != live", "if err := conn.WriteControl")],
    "server/run": ['"POST /api/runs"', '"GET /api/runs/{id}/state"',
                   '"GET /api/runs/{id}/events"', '"POST /api/runs/{id}/resume"'],
    "react/cursor": [None, None, None],
    "react/reduce": ["if (number <= previous)", "if (number !== previous + 1n)",
                     ('if (event.type === "execution_content")', "text: payload.content"),
                     "if (preview.id !== event.modelCallId"],
    "react/buffer": ['if (frame.kind === "ready")', ("if (!state || loading)", "buffer.push"),
                     "if (buffer.length > 1024"],
    "react/catchup": ["if (!valid() || !ready || loading)", "poll = setTimeout",
                      "if (terminal(snapshot))"],
    "react/hook": ["}, [runId]);", ("return () =>", "cancelAnimationFrame(frame)")],
    "react/render": ['{answer.text || "(empty answer)"}',
                     ".flatMap((answer) => answer.sourceMessageIds", None],
    "react/run": [None, '"/api":', None],
    "recovery/load": ["err := s.codecs.Unmarshal", "view, err = s.read(bucket, runID)"],
    "recovery/claim": ["err := s.db.Update", None],
    "recovery/tools": [None, "status = gent.ToolRecoveryRetryAllowed",
                       "status := gent.ToolRecoveryUnresolved", None],
    "recovery/resume": ["return runner.Resume", "return runner.Resume",
                        "worker context.Context", None],
    "recovery/try": ["id := r.PathValue", None, "writeError(w, err)", None],
}

STEP_NOTES = {
    "react/cursor": [
        ("export class Gap", "Name a missing event",
         "The reducer throws Gap when the next event number is missing. Reload the saved state."),
        ('if (typeof value !== "string"', "Validate the wire value",
         "Accept a canonical non-negative decimal string before converting it to an integer."),
        ("return BigInt(value)", "Compare without rounding",
         "BigInt preserves every uint64 event number, including values above 2^53."),
    ],
    "react/catchup": [
        ("state = catchUp(snapshot, buffer)", "Install the snapshot and drain the buffer",
         "Skip events covered by the snapshot. Apply the later events in order before going live."),
    ],
    "react/render": [
        ("const parts = preview.parts.filter", "Hide only the replaced preview",
         "Keep unrelated metadata. Remove the parts named by the accepted answer's source IDs."),
    ],
    "recovery/claim": [
        ("w.fence, err = bucket.NextSequence()", "Give the new owner a higher number",
         "The Save transaction checks this value. An older worker cannot advance the run."),
    ],
}

# Supporting files also get useful explanations when opened in the complete-source view.
FILE_NOTES = {
    "server/main.go": [
        ("signal.NotifyContext", "Own the worker context",
         "Service shutdown cancels workers. A browser disconnect does not cancel them."),
        ('env("RUN_DB", "runs.db")', "Keep the same database",
         "Reopen this file after a restart to resume the same run and its event sequence."),
        ("server.Shutdown(ctx)", "Finish shutdown",
         "Stop accepting HTTP work, then wait for the agent workers before closing the database."),
    ],
    "server/view.go": [
        ("IncludeChunkText: true", "Choose visible content",
         "Enable previews and accepted output. Use the same policy for events and snapshots."),
        ("type BrowserState struct", "The browser contract",
         "This display view is separate from the lossless RunView used to restore the agent."),
        ("strconv.FormatUint(snapshot.LastEventNumber, 10)", "Keep the cursor exact",
         "Encode Gent's uint64 as a decimal string. React compares it with BigInt."),
        ("func eventFrame", "Use one event sequence",
         "The event envelope and snapshot use the same run-wide event numbers."),
    ],
    "client/src/main.tsx": [
        ('new URLSearchParams(location.search)', "Remember the run in the URL",
         "A refresh reads the same run ID and starts the subscribe-buffer-snapshot protocol."),
        ("history.replaceState", "Keep the page shareable",
         "Update the URL after creation. A resumed run keeps the same ID."),
    ],
    "client/src/connection.ts": [
        ("return new WebSocket(url)", "Open the accepted-event feed",
         "Use ws or wss on the current origin. Wait for the ready frame before loading state."),
        ("    closeCurrent = () => {", "Release the old connection",
         "Clear timers, abort the state request, remove handlers, and close the old socket."),
        ("state = catchUp(snapshot, buffer)", "Install and catch up",
         "Skip buffered events covered by the snapshot. Apply the rest before live mode."),
    ],
    "client/src/style.css": [
        ("main {", "Limit reading width", "Keep the sample app readable on a wide screen."),
        ("white-space: pre-wrap", "Preserve streamed text",
         "Display text line breaks without adding layout markup to the stream."),
    ],
    "client/tsconfig.json": [
        ('"target": "ES2022"', "Support exact cursors",
         "The client uses native BigInt to compare Gent's event numbers without precision loss."),
        ('"strict": true', "Check the browser contract",
         "Type checking catches mismatches in the event, state, and component interfaces."),
    ],
    "client/package.json": [
        ('"dev":', "Run the development UI", "Vite proxies API requests to the Go server."),
        ('"build":', "Check and build", "Check types before creating the static production files."),
        ('"test":', "Verify the stream reducer",
         "Tests cover duplicates, gaps, recovery of the connection, and a real Gent trace."),
    ],
    "client/index.html": [
        ('id="root"', "Mount React", "main.tsx creates the React root in this element."),
        ('src="/src/main.tsx"', "Load the application",
         "Vite replaces this source entry with the production bundle when you build."),
    ],
    "server/go.mod": [
        ("module example.com/gent-streaming", "A separate example module",
         "The server has its own dependency list and tests, separate from Gent's root module."),
        ("replace github.com/rickchristie/gent", "Use the matching Gent checkout",
         "After download, set this path to the checkout with the API revision in this guide."),
    ],
}


def language(path):
    return {".go": "go", ".ts": "typescript", ".tsx": "tsx", ".json": "json",
            ".html": "markup", ".css": "css", ".md": "markdown",
            ".mod": "go"}.get(Path(path).suffix, "plain")


def line_number(code, needle):
    matches = [number for number, line in enumerate(code.splitlines(), 1) if needle in line]
    if len(matches) != 1:
        raise ValueError(f"Expected one line for {needle!r}, found {matches}")
    return matches[0]


def note(code, target, title, text, offset=0):
    if target is None:
        return dict(title=title, text=text)
    first, last = (target, target) if isinstance(target, str) else target
    start, end = line_number(code, first), line_number(code, last)
    if start > end:
        raise ValueError(f"Reversed note range: {first!r} through {last!r}")
    return dict(title=title, text=text, first=start + offset, last=end + offset, anchor=first)


def step_reading(scenario, step):
    key = f"{scenario['id']}/{step['id']}"
    path = "example/main.go" if scenario["id"] == "basic" else step["location"]
    full = (HERE / path).read_text()
    start = full[:full.index(step["code"])].count("\n") + 1
    targets = TARGETS[key]
    if len(targets) != len(step["notes"]):
        raise ValueError(f"Note targets do not match {key}")
    notes = [note(step["code"], target, title, text, start - 1)
             for target, (title, text) in zip(targets, step["notes"])]
    notes.extend(note(step["code"], target, title, text, start - 1)
                 for target, title, text in STEP_NOTES.get(key, []))
    notes.sort(key=lambda item: item.get("first", float("inf")))
    return dict(path=path, start=start, language=language(path), notes=notes)


def file_reading(path, source, readings):
    notes = []
    for reading in readings.values():
        if reading["path"] == path:
            notes.extend(item for item in reading["notes"] if "first" in item)
    for target, title, text in FILE_NOTES.get(path, []):
        notes.append(note(source, target, title, text))
    if Path(path).name == "README.md":
        notes.append(note(source, "# " + source.splitlines()[0][2:], "Start here",
                          "This file includes the project setup, run commands, and test commands."))
    unique = {(item["first"], item["title"]): item for item in notes}
    return dict(path=path, start=1, language=language(path),
                notes=sorted(unique.values(), key=lambda item: item["first"]))

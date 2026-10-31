# Gent interactive explainer

Open [gent-interfaces.html](gent-interfaces.html) directly in a browser. The generated file embeds
its data, scripts, styles, and fonts and works offline. Five experiments teach the current contracts
through changes to an agent's behavior, memory, saving, recovery, and retrieval. Exact declarations
are available in contextual dialogs and a secondary reference view.

The learning view starts with a concrete decision or failure, then makes the consequence visible:

- Follow a request through tool use, answer rejection, acceptance, or malformed output.
- Compact and reorder a prompt while inspecting the original evidence and earlier selections.
- Lose an acknowledgment and compare durable, accepted, and checkpoint cursors before retrying.
- Restore a worker and reconcile a tool effect, with and without external deduplication.
- Compare keyword and semantic retrieval, blend scores, and page through ranked tools.

Each experiment includes a prediction question and connects to its relevant interfaces. These are
small teaching models, not a JavaScript port of Gent. Keeping the models local makes failures safe
to explore and repeat, without credentials or an external service. A lesson can be linked with
`#view=learn&lesson=save` (or `loop`, `memory`, `restart`, `search`). Experiment state is temporary;
review notes persist separately. Use [Quickstart](../quickstart/README.md) for tutorials.

The guide contains every named interface in non-test Go source, seven structural capability checks,
and selected concrete types that explain ownership and continuation. The contract browser includes
exact declarations, source locations, method comments, embedded interfaces, signature comparisons,
and per-contract review notes. Notes stay in local browser storage, keyed by source fingerprint;
export them before changing browsers or discarding a source snapshot.

## Rebuild

Run from the repository root with Go and Node.js 20 or later. Extraction and rendering are separate
so the renderer also works in environments that do not permit Node to launch subprocesses. Keep
the extracted catalog in `/tmp` as inspection evidence rather than maintaining a second API list.

```sh
go run docs/interface-review/extract.go "$PWD" > /tmp/gent-interface-catalog.json \
  2> /tmp/gent-interface-extract-errors.log
node docs/interface-review/build.mjs > /tmp/gent-interface-build.log 2>&1
```

Check both exit statuses. If the Go cache is read-only, set `GOCACHE=/tmp/gent-interface-go-cache`.
The Node build accepts a catalog path as its first argument. It has no npm dependencies.
Font bytes and license notices are in `assets/` so this page can build on its own. The build embeds
them in the output for offline use.

`extract.go` uses Go's parser, excludes test/dependency/hidden/documentation directories, and reads
the current checkout, including uncommitted refactor work. Its build tag keeps it out of `go test`.
The source fingerprint covers all scanned Go input files. The comparison uses `HEAD` declarations
with the same name in the same file, ignoring comments and bodies. This is not a compatibility
test: an unchanged signature can have changed behavior, and a move can appear as a new declaration.

The build rejects undocumented or removed interfaces and broken related-contract references.
It also requires the five lessons to cover every named interface exactly once, so new API surfaces
cannot silently become reference-only material.
Rerun extraction whenever Go files change; the page is a snapshot, not a live source viewer.
Comment edits can move declarations and invalidate source links. Extract again before a release,
then rebuild and check the page with that same catalog. The browser check accepts a catalog path
as its first argument, like the builder, so separate checks can use separate temporary files.
After a commit, a clean checkout has no previous signatures because the comparison uses `HEAD`.

## Maintain the explanations

- `content.mjs` explains responsibilities, contracts, relationships, and review questions.
- `lesson-models.mjs` contains deterministic teaching transitions and the lesson-to-interface map.
- `lessons.js` and `lessons.css` turn transitions into experiments and contextual explanations.
- `template.html`, `styles.css`, and `app.js` implement the offline review interface.
- `extract.go` captures declarations and public methods without using regex to parse Go.
- `build.mjs` checks coverage and produces the standalone HTML.
- `browser.mjs` checks the generated artifact, not a development-server approximation.
- `lessons-browser.mjs` exercises learner choices, failure branches, and responsive lessons.

The recovery examples illustrate contracts; they do not run Gent. Keep the distinction between a
store commit and Gent receiving its acknowledgment. A lost acknowledgment can leave storage ahead
of the accepted local cursor. Retrying reconciles the same batch but does not revive the canceled
execution context. Tool-ready evidence likewise does not prove an external side effect completed.
The history slider inspects selection revisions; it does not rewind the evidence ledger. Summary
text and search scores are illustrative fixtures, not results from a model or retrieval engine.
Keep range inputs attached during updates so pointer dragging and keyboard scrubbing both work.

The anonymous capabilities are intentionally listed separately: they are structural type assertions,
not named exported interfaces. Supporting concrete types are labeled as such. Keep these categories
explicit so the interface count remains meaningful.

## Verify

Browser checks share Quickstart's Playwright dependency so documentation tests use the same version.
Run `npm ci --prefix docs/quickstart` from the repository root to install it. Use its installed
Chromium, or set `GENT_REVIEW_CHROMIUM` to an available executable.

```sh
node docs/interface-review/browser.mjs > /tmp/gent-interface-browser-test.log 2>&1
node docs/interface-review/lessons-browser.mjs > /tmp/gent-lessons-browser-test.log 2>&1
go test ./... -count=1 -timeout 300s > /tmp/gent-interface-native-test.log 2>&1
CGO_ENABLED=0 go test ./... -count=1 -timeout 300s > /tmp/gent-interface-portable-test.log 2>&1
go test -race ./... -count=1 -timeout 300s > /tmp/gent-interface-race-test.log 2>&1
```

Browser coverage includes all 47 interface declarations, the 24 supporting types, source locations,
filters, embedded contracts, comparison views, every recovery phase, all save outcomes, keyboard
navigation, persistent notes, exact Markdown export, and widths down to 320px. Networking is
disabled. Screenshots and reports are written to `/tmp/gent-interface-*` and `/tmp/gent-learning-*`.
Lesson checks also verify all 47 contextual signatures, earlier prompt selections, cursor
divergence and reconciliation, compatibility failures, duplicate effects, ranking, and paging.

Changing the interface set requires updating the explicit inventory expectations in the browser
test alongside the descriptions. This deliberately makes an expanded API a visible review event.

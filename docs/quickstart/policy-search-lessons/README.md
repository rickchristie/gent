# Gent policy search lessons

Share `../dist/gent-policy-search-lessons.html`. It is the complete deliverable. Open it in a
browser. It has no external scripts, fonts, images, network calls, or server requirement.

The eight lessons explain the standard ReAct loop with hybrid policy search. They include an
interface explorer, policy field editor, score experiment, five execution paths, a Go example,
output modes, and a quiz. Browser storage keeps lesson completion when storage is available.
The interface shows a message and keeps session progress when storage is unavailable.

The browser uses fixed scores to teach fusion. It does not run Bleve, ONNX, or an LLM. The policy
rules are invented. The Go example uses the real Gent components and can make live API calls when
the user runs it with credentials. Its tests use local fixtures and make no model API requests.

## Source and build

Run from the repository root:

```bash
python3 docs/quickstart/policy-search-lessons/build.py
```

`build.py` reads exact interface declarations from the current Go files. It also embeds
`example/main.go`, the source-file list, the base Git revision, and a digest of the source bytes.
This matters because this working copy contains local API changes. A released Gent version can
have different names or contracts.

Edit `template.html`, `lesson.css`, and `lesson.js`, then rebuild. Do not edit the generated file.
The build reads files only and writes one HTML file. It needs Python 3 and Git, with no packages
to install. The generated file needs neither tool.

The example builds the policy indexes once and creates an ExecutionContext per question. This
keeps histories separate and prevents repeat indexing. The application owns the embedder. The
current PolicySearchTool does not expose a Close method or custom index injection. Lesson 7
explains when a custom tool is needed for explicit backend ownership or custom weights.

## Verification

```bash
node --check docs/quickstart/policy-search-lessons/lesson.js
node docs/quickstart/tests/gent-policy-lessons.mjs
go test ./... -count=1 -timeout 300s
CGO_ENABLED=0 go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 300s
```

The root `./...` pattern includes both lesson examples under `docs/quickstart/`.

The browser check uses the installed Playwright dependency from `docs/quickstart`. If its expected
browser is not installed, set `GENT_LESSON_CHROMIUM` to an installed Chromium executable. Tests
open the final file with networking disabled and fail on console errors or network requests.
Screenshots go to `docs/quickstart/screenshots/gent-policy-lessons-*.png`.
Every displayed interface and source line is compared with the current Go file. This check catches
an old generated lesson after an API change, even when the page still loads and its controls work.

These are static-file checks, so they use the existing documentation test pattern. They do not
start application servers or use `.dev`. There is no shared `web/playtest/fixtures.ts` in this
checkout. The script supplies its own console-error and network-request checks.

If the default Go cache is read-only, set `GOCACHE` to a writable temporary directory. Go HTTP
tests need permission to open local listening sockets. Native inference tests require the local
ONNX libraries and model files. A passing portable suite does not prove native inference works.

| Lesson | Checked behavior |
| --- | --- |
| 1 | Map selection opens the correct contract. |
| 2 | All 21 declarations and source lines match Go; metadata advice and filters are correct. |
| 3 | Display-only fields stay out of both index inputs; edits and reset work. |
| 4 | Query, weights, top-K, score values, and ranking changes match expected results. |
| 5 | All five paths stop, move back, restart, and show the expected counts. |
| 6 | Downloaded Go bytes match the source; clipboard fallback selects all code. |
| 7 | Output mode controls show the current result contract. |
| 8 | The quiz handles missing, wrong, and correct answers. |
| Shared | Progress survives reload; keyboard controls, narrow layout, and print work. |

`example/main_test.go` runs real policy retrieval through the registry, ReAct loop, and runner.
It checks the complete tool result presented to the model, final answer, native definitions,
and fresh history when a runner is reused. It also checks canceled startup. A fixed embedder
and scripted model keep these tests repeatable without credentials or downloads.

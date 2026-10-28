# Gent coding quick starts

Use this directory for Gent's interactive documentation.

Share [dist/gent-quick-start.html](dist/gent-quick-start.html). It contains four step-by-step
guides, the styles and scripts, all example source files, and project ZIP downloads. It needs no
server or network
connection to read. There is no completion tracker or stored lesson progress.

1. **Simple agent:** one Go file with a model, tool, standard ReAct loop, task, and result.
2. **Saved streaming API:** the same loop with a database recorder, accepted events, and API routes.
3. **React streaming client:** buffer events, load state, skip covered events, and keep listening.
4. **Resume a saved run:** load the full saved state, fence the writer, and restore fresh bindings.

The simple agent uses live model credentials when run. The server also supplies a local model
for a complete browser and recovery demo with no credentials. The guide does not run Go or React
inside its HTML file. Downloaded projects need their normal build tools and dependencies.

The earlier [policy search guide](dist/gent-policy-search-lessons.html) is also here. Its source
and example are in [policy-search-lessons/](policy-search-lessons/README.md).

Keep the lesson sources, examples, browser tests, and both standalone HTML files in Git. The local
ignore file excludes installed dependencies, Python caches, the React build, the demo database,
and test screenshots. The HTML files embed their assets, so each can be shared on its own.

## Source and build

Run from the repository root:

```bash
python3 docs/quickstart/build.py
python3 docs/quickstart/policy-search-lessons/build.py
```

Edit `basic_steps.py` and `scenarios.py` for lesson text. Edit `example/`, `server/`, and `client/`
for the example programs. `template.html`, `style.css`, and `app.js` define the guide interface.
The builder extracts code from those tested files and embeds the complete projects as ZIP data.
Do not edit the generated HTML directly. Python 3 and Git are only needed to rebuild the guide.

`reading.js` adds syntax colors and source line numbers without changing the copied text. It
uses the embedded Prism 1.30.0 grammars in `vendor/`; no browser request is needed. The custom
forest theme uses separate colors for keywords, functions, types, strings, and comments.

`annotations.py` binds each explanation to unique source text. The build rejects missing or
ambiguous targets. It also maps fragment lines to their full-file line numbers. This prevents an
arrow from silently pointing to unrelated code after a source edit. Related concepts that are
not in the shown code stay below it, without a false line reference.

At desktop widths, navigation, code, and explanations form three columns. The browser measures
wrapped lines and keeps note cards apart before drawing their arrows. Selecting a note focuses
its source line. At smaller widths, notes move below the code and retain their line buttons.
The page owns vertical scrolling; code blocks have no internal height limit or scroll area.

The output records the base Git revision and a digest of the Gent API source. Use a checkout
with the same API when running the projects. The downloaded server README explains how to set
its local Go module replacement. The simple guide provides an editable checkout path.
The default path is a placeholder so a committed guide contains no local checkout path.

The saved-state API is small enough to teach each boundary. It uses one bbolt process and short
runs. Its browser view is a declared subset of the full trace. The server README explains the
storage tradeoff, local access scope, and safe handling of uncertain tool outcomes.

## Verification

From the repository root:

```bash
node --check docs/quickstart/app.js
node --check docs/quickstart/reading.js
node docs/quickstart/tests/gent-quick-start.mjs
node docs/quickstart/tests/gent-streaming.mjs
node docs/quickstart/tests/gent-policy-lessons.mjs
go test ./... -count=1 -timeout 300s
CGO_ENABLED=0 go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 300s
```

Also run all three Go test commands from `server/`, which has its own module. Run `npm test` and
`npm run build` from `client/`. The root `./...` pattern includes both lesson examples now that
they are under `docs/`. It skips nested modules, so the separate server commands are required.

The guide browser check compares snippets and downloads with disk source, tests all scenario
links, keyboard use, history, offline use, phone layout, and print. It also checks token colors,
exact source line numbers, arrow positions, card spacing, and the single page scrollbar.
The streaming browser check runs the real server and React build, refreshes during output, and
restarts the server with the same database. Both record browser errors and save screenshots under
`docs/quickstart/screenshots/`.

The React reducer is checked against a real Gent event fixture and its complete snapshot. The
server tests use bbolt, the real loop and registry, actual HTTP/WebSocket calls, and database
reopen with fresh runtime objects. They make no live provider calls.

This directory owns the Playwright dependency used by the documentation browser tests.
Set `GENT_LESSON_CHROMIUM` to a Chromium executable if needed. There is no `.dev` setup or shared
`web/playtest/fixtures.ts` in this repo. These tests start their own servers and close them on exit.
Set `GOCACHE` to a writable folder if needed. Socket and native ONNX tests need the appropriate
local permissions and runtime.

Run `npm ci --prefix docs/quickstart` from the repository root to install Playwright.
Run `npx --prefix docs/quickstart playwright install chromium` to install its browser.
Use `npm ci --prefix docs/quickstart/client` before testing or building the React project.

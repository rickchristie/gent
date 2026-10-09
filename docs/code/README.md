# Gent field guide

Open [`../dist/gent.html`](../dist/gent.html) directly in a browser. This is the reader-facing
documentation: one file containing the complete interactive guide, fonts, code references, and
example. No server, installation, or internet connection is needed to read it.

## Build

From the repository root, with Node.js 20 or later:

```sh
node docs/code/build.mjs
```

The build itself has no dependencies. It reads Gent declarations and `examples/history/main.go` from
this checkout and writes `docs/dist/gent.html`. Missing or renamed declarations fail the build so
the atlas cannot silently keep referencing a removed type. A source fingerprint identifies the Go
inputs. Rebuilding the same sources produces the same file; generated dates and network resources
are avoided.

To serve the result locally, run `node docs/code/preview.mjs` and open `http://127.0.0.1:4173`.

## Maintain

- `src/template.html`: chapter explanations and accessible controls.
- `src/content.mjs`: teaching copy, component relationships, and integration sketches. Entries
  without a curated public-API sketch receive their actual Go type declaration at build time.
- `src/simulations.mjs`: small, pure educational state transitions for history, saving, viewers, and
  budgets. These illustrate Gent contracts; they do not execute the Go library.
- `src/app.js`: browser interactions, code inspection, playback, navigation, and keyboard behavior.
- `src/styles.css`: responsive layout, typography, motion, and print treatment.
- `assets/`: locally bundled Fontsource 5.3.0 fonts and their SIL Open Font License notices. The
  notices are also embedded in the guide’s colophon.

The journal lab deliberately separates the store cursor from Gent’s accepted cursor. A lost
acknowledgment can leave storage ahead. Retrying the identical batch reconciles acceptance without
reviving the stopped execution context. Keep this distinction when changing the examples.

The recovery examples deliberately stop at an uncertain external operation until the application
supplies a `ToolRecovery` decision. They must never imply that a checkpoint proves an external side
effect completed, or that Gent guarantees exactly-once effects.

## Verify

Install the browser test dependency once:

```sh
cd docs/code
npm ci
npx playwright install chromium
```

Run from `docs/code`, saving command output as required by this repository:

```sh
npm run build > /tmp/gent-guide-build.log 2>&1
npm test > /tmp/gent-guide-unit-test.log 2>&1
npm run test:browser > /tmp/gent-guide-browser-test.log 2>&1
npm run test:design > /tmp/gent-guide-design-test.log 2>&1
```

The browser checks open the actual generated HTML over `file://` with networking disabled. They
cover every atlas entry, the failure simulations, context revisions, quiz, mobile overflow, reduced
motion, and browser errors. Screenshots are saved under `/tmp/gent-guide-*.png` for visual review.
The design checks exercise sticky navigation, atlas filtering while scrolling, all contracts at
desktop and mobile sizes, context selection markers, and both clipboard permission paths.

## Screenshot review

The comprehensive suite writes to **`docs/code/screenshots/`**, which is gitignored. Run from
`docs/code` after building the HTML:

```sh
npm run screenshots -- --label=review > /tmp/gent-guide-screenshots.log 2>&1
```

Open `screenshots/review/index.html`. Its filters select device and interaction group; click any
thumbnail for the original PNG. The numbered contact sheets support reviewing every capture, and
`manifest.json` records the build hash, viewport, scroll position, coverage, and browser errors.

Use a fresh label for each run, such as `before` and `after`. Existing folders are never overwritten
because their screenshot IDs may already be cited in a review. Omit the label to use a timestamp.
The suite opens the actual standalone HTML over `file://` with networking disabled and fails on
browser errors, horizontal page overflow, missing chapters, or missing desktop inspectors.

The page sweep overlaps screenshots to account for sticky navigation and atlas filters. Tall
panels and dialogs receive additional scroll captures instead of an unreadably scaled full-page
image. The state scenarios cover every walkthrough step, map selection, parser branch, history
revision, journal and recovery outcome, viewer phase, budget boundary, expandable note, recipe,
atlas inspector, search state, quiz result, font license, and motion control. Mobile repeats
layouts that change materially; tablet checks the middle breakpoint.

- `test/screenshots.mjs`: browser profiles, capture geometry, coverage checks, and manifest.
- `test/screenshot-scenarios.mjs`: user interactions and teaching states.
- `test/screenshot-gallery.mjs`: offline gallery and contact sheets.
- [Design review](design-review.md): baseline findings and the resulting refinement.

Native `<button>`, `<input>`, `<select>`, `<details>`, and `<dialog>` elements provide keyboard
behavior. The motion toggle stops autoplay; the system’s reduced-motion preference is honored on
initial load. The `/` shortcut focuses atlas search. Escape closes an inspector and restores focus
to its opener.

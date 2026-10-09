# Gent field guide: screenshot review

Reviewed the generated HTML before changing its design. The baseline contains **312 captures** at
1440 × 1000, 390 × 844, and 900 × 1000. Every capture was visually inspected in the 78 numbered
contact sheets; selected desktop and mobile originals were also opened at native resolution to
check typography and clipping. The gallery links to every original.

- [Baseline gallery](screenshots/before/index.html)
- [Capture manifest](screenshots/before/manifest.json)
- [Complete review notes, covering IDs 001–312](screenshots/before/review-notes.json)

These artifacts are intentionally gitignored. Keep a run's directory intact: the numbered evidence
below refers to that specific build. The manifest records its SHA-256 hash.

## Keep the visual identity

The paper background, dark green journal, serif chapter headings, restrained orange accents, and
loop illustration give the guide a coherent identity. The hero stacks well on mobile (202, 307).
The inspector's job → reason → implementation → Go contract structure is consistent across all
47 abstractions. The quiz gives useful corrective feedback. These should survive the refinement.

## Critique and one refinement pass

1. **Teaching text is too small relative to the generous panels** (023–033, 106–110, 217–219).
   Increase simulation copy, metadata, and control sizes. Keep the display typography; make the
   explanations comfortable to read.
2. **Code is small, widely spaced, and hidden behind nested scrolling** (132–139, 174–176,
   269–276, 281–297). Give `pre` and `code` the same reading scale, wrap long lines, and use the
   outer dialog/page for vertical scrolling. Keep the inspector name in its sticky header.
3. **Navigation and atlas filters disappear during long mobile browsing** (205–230). Keep chapter
   navigation and filters available while scrolling. Readers should be able to change chapters
   or refine a search without returning to the top.
4. **The notebook requires readers to match active IDs themselves** (053–056, 241–244). Mark entries
   belonging to the inspected context revision. Keep originals, summaries, and compaction markers.
5. **Failure and uncertainty resemble successful progress** (069–105, 247–263). Add explicit state
   headings and restrained warning colors for lost acknowledgment, stale writers, unresolved tools,
   stale viewers, and canceled budgets. Preserve the stopped execution after reconciliation.
6. **Viewer tiles use only a small part of the row** (093–099, 257–259). Distribute tiles across the
   row and enlarge numbers and labels so readers can compare saved and applied sequences.
7. **Copy feedback exposes a browser exception and is too narrow on mobile** (115, 273). Select the
   code and give a short keyboard-copy instruction. Log the exception for diagnosis; constrain the
   toast to a useful width.

Browser measurements confirm the typography issue: the ToolCallResult `pre` used 16px type with a
28.8px line box while its nested code used 11px glyphs. Its 674px content was confined to a 420px
inner scroller inside an already scrolling dialog. Walkthrough body copy was 12px and observations
10px. On mobile, the chapter navigation was more than 8,600px above the viewport by the viewer
chapter. These measurements support the critique independently of contact-sheet scaling.

## Capture quality

The baseline revealed two issues in the new screenshot helper itself. Tall element screenshots
can place the sticky masthead inside the captured panel (189, 241–244, 298–299); the refined helper
uses overlapping real viewport slices for those panels. Copy feedback also remained visible in
subsequent search screenshots (118–121, 277–280); the helper now waits for the toast to disappear.
Neither artifact is treated as evidence of a broken page layout.

The suite covers all chapters with overlapping page scrolls, every walkthrough step, every map
selection, XML and Markdown parsing branches, all history revisions and entry inspectors, every
journal and recovery outcome, all viewer phases, budget boundaries, expanded notes, recipes,
all 47 abstraction inspectors, search states, quiz feedback, licenses, keyboard focus, autoplay,
paused motion, and reduced motion. Mobile repeats states whose layout changes; tablet checks the
middle breakpoint. Simulations run locally with networking disabled.

## Validation after the pass

The rebuilt [standalone guide](../dist/gent.html) passed all 11 simulation tests, the existing
browser suite, and the added design interaction checks. The latter exercise all 47 contracts at
both desktop and mobile widths: code fits without an inner scroll area, the component name remains
visible while scrolling, and the exact source text is preserved. They also check sticky chapter
navigation, atlas filtering while browsing lower entries, compaction/rewind markers, and denied
and granted clipboard access.

The final [refined gallery](screenshots/refined/index.html) contains **378 screenshots and 95 review
sheets**. Its [manifest](screenshots/refined/manifest.json) confirms all 11 chapters at desktop and
mobile widths, all 47 desktop inspectors, zero browser errors, zero external requests, and zero
horizontal page overflow. Additional captures reflect the corrected viewport slicing and expanded
scroll coverage. The intermediate `after` run is retained as verification evidence too.

Visual checks after refinement covered the page layouts, active history selections, journal
failures, uncertain-tool outcomes, viewer gaps, exceeded budgets, recipes, long inspectors, mobile
navigation, and atlas filters. The code and labels are easier to read, warning states are explicit,
and the mobile controls stay available during the long guide. The hero and chapter identity remain
coherent. Long Go examples still require ordinary vertical scrolling; all their content is now
available in that single scroll flow.

The simulation semantics and extracted Go declarations are unchanged. No Go library code was
modified during this design pass.

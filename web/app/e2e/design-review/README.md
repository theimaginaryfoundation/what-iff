# Design review report

A before/after view of every screen the visual suite covers, as one
self-contained HTML file.

```bash
npm run design:review           # from web/app/
make design-review              # from the repo root
```

Writes `.dev/design-review/report.html`. Add `-- --open` to open it.

## Why this exists separately from the visual suite

The visual suite answers "did anything change by accident?". This answers
"what did we change on purpose, and what does it look like?" — and the two
are close to opposites in practice.

When a designer changes a screen, they regenerate its baseline and commit it.
From that moment `toHaveScreenshot()` compares the new render against the new
baseline and passes. The visual report is green and empty. The change is
invisible *precisely because the workflow was followed correctly*, and the
only remaining record of what the screen used to look like is a binary blob
in the git history that no review tool renders.

So the "before" here is the baseline at the merge base and the "after" is the
baseline on this branch. Nothing is rendered, nothing is executed, no backend
or browser or Docker is involved, and there are no npm dependencies — which is
what makes it a second-long command rather than a twenty-minute one.

## What the report contains

- **Every covered screen**, changed ones first, desktop and mobile side by
  side, sized so a phone reads as a phone next to a laptop.
- **Four ways to compare** a changed screen: a drag slider, side by side,
  onion-skin cross-fade, and a pixel difference overlay with adjustable
  sensitivity. All four are computed in the browser from the two images the
  page already holds.
- **Impact**: what categories of file changed, which feature areas, and how
  much of the frontend change is design surface rather than logic.
- **"Changed, but not shown above"**: templates, styles and assets the branch
  touched that no screen in the report renders. The quiet failure mode of any
  visual suite is a restyled component with no baseline; this is the line
  that makes it loud.

## Layout

| File | Role |
|---|---|
| `cli.mjs` | Argument parsing, output paths, the terminal summary. |
| `collect.mjs` | Builds the model. Knows about screens, statuses and impact; produces no HTML. |
| `render.mjs` | Model to HTML. Deduplicates images and inlines everything. |
| `assets/report.css`, `assets/report.js` | The report's own UI, inlined at render time. |
| `lib/git.mjs` | Refs, blobs, changed files. The only place that shells out to git. |
| `lib/screens.mjs` | Baseline paths to screens; spec titles and doc comments. |
| `lib/png.mjs` | IHDR dimensions, nothing more. |
| `lib/impact.mjs` | File categories, feature areas, the name-match heuristic. |

`collect.mjs` returning a plain object rather than writing files is what lets
`--json` and the HTML report share one definition of "which screens changed",
instead of two that drift.

## Known limits

- **Only screens with committed baselines appear.** "No screen changed" is not
  "nothing changed" — read the uncovered list alongside it.
- **Related files are matched by name, not by imports.** A shared component
  that renders a screen without sharing a word with its name will be missed.
  The report says so where it shows them.
- **Unchanged means byte-identical.** A re-encoded baseline that renders the
  same reads as changed. That is deliberate: it is still something a reviewer
  should ask about.

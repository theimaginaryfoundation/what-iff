# Design review report

A before/after view of every screen the visual suite covers, as one
self-contained HTML file.

```bash
npm run design:review           # from web/app/
make design-review              # from the repo root
```

Writes `.dev/design-review/report.html`. Add `-- --open` to open it.

## On a pull request: the `design-review` label

Add the **`design-review`** label to a PR and
`.github/workflows/design-review.yml` builds the report for it, uploads it
as the `design-review` artifact, and posts a sticky comment listing the
screens that moved and by how much. Pushing to a labelled PR refreshes both.

Opt-in rather than automatic, for the reason every label gate in this repo
exists: most PRs do not change a screen, and a report that appears on all of
them is one people learn to scroll past.

The job runs no browser, no backend and no `npm ci`, so it costs seconds
rather than minutes. Fork PRs get the artifact but no comment — their token
is read-only, and the alternative (`pull_request_target`) would hand a write
token to a job running the PR's own code.

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
| `markdown.mjs` | The model as a PR comment. Same source as the HTML, so the two cannot disagree. |
| `collect.mjs` | Builds the model. Knows about screens, statuses and impact; produces no HTML. |
| `render.mjs` | Model to HTML. Deduplicates images and inlines everything. |
| `assets/report.css`, `assets/report.js` | The report's own UI, inlined at render time. |
| `lib/git.mjs` | Refs, blobs, changed files. The only place that shells out to git. |
| `lib/screens.mjs` | Baseline paths to screens; spec titles and doc comments. |
| `lib/png.mjs` | Dimensions from the header, plus a decoder for 8-bit non-interlaced PNGs. |
| `lib/diff.mjs` | Changed-pixel comparison. Mirrors the rule `assets/report.js` runs on a canvas. |
| `lib/impact.mjs` | File categories, feature areas, the name-match heuristic. |

`collect.mjs` returning a plain object rather than writing files is what lets
the HTML report, `--json` and the PR comment share one definition of "which
screens changed", instead of three that drift.

The changed-pixel figure is computed twice on purpose: in Node, so the
comment and the JSON can quote a number without rendering anything, and in
the browser, so the report's sensitivity slider can move without a round
trip. Both implement the same rule from the same default threshold, and a
test pins the boundary. Changing one means changing the other.

## `gh` is optional everywhere

The only thing the GitHub CLI contributes is the pull request number and
title in the report's header (`lib/gh.mjs`). A missing `gh`, an
unauthenticated one, no network, or a branch with no PR all return null and
cost one header line. The CI workflow never depends on it being present in
the runner image, and nothing in the HTML, the JSON or the PR comment is
derived from it.

It is consulted only when the report's head is the checked-out branch.
Asked for any other ref, `gh pr view` would return the *current* branch's
pull request and stamp a report about one change with another change's
title, so the lookup is skipped instead.

## Known limits

- **Only screens with committed baselines appear.** "No screen changed" is not
  "nothing changed" — read the uncovered list alongside it.
- **Related files are matched by name, not by imports.** A shared component
  that renders a screen without sharing a word with its name will be missed.
  The report says so where it shows them.
- **Unchanged means byte-identical.** A re-encoded baseline that renders the
  same reads as changed. That is deliberate: it is still something a reviewer
  should ask about.

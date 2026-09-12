---
name: design-review
description: >-
  Produce a visual before/after review of a branch or pull request — a
  self-contained HTML report showing every screen the Playwright visual suite
  covers, as it looked on main and as it looks now, with a slider/onion/pixel-
  difference comparison, plus what the change touched and which design files
  no screen covers. Use whenever someone wants to SEE a change rather than
  read its diff: "what does my PR look like", "show me the visual impact",
  "design review for this branch", "did the layout change", "before and after
  screenshots for the PR", "which screens did I break". Also use when a
  designer's change lands on a screen with no visual baseline and one should
  be added.
---

# Design review

Turns a branch into a picture of itself. The audience is whoever has to
answer "does this look right?" — usually the person who designed it, who
should not have to read a unified diff of a Tailwind class list to find out.

## The one thing to understand first

A passing visual suite proves nothing about an intentional design change.

When a designer changes a screen they also regenerate its baseline and commit
it, so by review time `toHaveScreenshot()` compares the new render against
the new baseline and passes. The Playwright report is green and empty. The
change is invisible *because it was done correctly*.

So this report does not read a test run. It reads git: the **before** is the
baseline committed at the merge base, the **after** is the baseline on this
branch. That is why it needs no backend, no browser and no Docker, and why it
finishes in about a second.

The visual suite still matters — it is what catches the *unintended* change,
where the code moved and the baseline did not. That is a different question
with its own command; see "When to also run the suite" below.

## Produce the report

From `web/app/`:

```bash
npm run design:review
```

Or from the repo root, `make design-review`. Either writes
`.dev/design-review/report.html` — one self-contained file with the images
inlined, so it can be sent to someone who will never clone the repo.

Useful variants:

| Goal | Command |
|---|---|
| My uncommitted work, against `main` | `npm run design:review` |
| My branch as committed | `npm run design:review -- --head HEAD` |
| Someone else's PR | `git fetch origin pull/<N>/head:pr-<N>` then `-- --head pr-<N>` |
| Everything since a release | `npm run design:review -- --base v1.4.0` |
| Open it immediately | add `-- --open` |
| Machine-readable, for a CI assertion | add `-- --json <path>` |

The default `--head` is the **working tree**, not `HEAD`. An uncommitted or
untracked baseline shows up, which is the point when iterating.

## Read the report back to the user

Do not just hand over a file path. Open the JSON model (`--json`) and say
what is in it, because that is what the person asked:

1. **Which screens changed**, by name, and how much of each differs. A screen
   at 15% differing pixels was restructured; one at 0.3% moved a border.
2. **Anything new or removed.** A removed baseline with no explanation in the
   PR is worth a question — it usually means a spec was deleted or renamed
   rather than a screen genuinely retired.
3. **The "changed, but not shown above" list.** These are templates, styles
   and assets the branch touched that no screen in the report renders. This
   is the report's most actionable output and the easiest to skip past.
4. **Send the HTML file to the user** so they can actually look at it.

## When nothing changed

`0 changed · 0 new · 0 removed` with design files in the impact list means one
of two things, and they need different responses:

- **The change is on a screen with no visual coverage.** Check the uncovered
  list. Offer to add a visual spec — see below.
- **The designer changed the code but did not regenerate the baselines.** The
  report cannot tell you this; the visual suite can, and will fail. Say so
  rather than reporting "no visual impact", which would be wrong.

## Adding coverage for an uncovered screen

If a changed screen has no baseline, the fix is a new spec in
`web/app/e2e/tests/visual/`. Follow the `playwright-e2e` skill for the
mechanics and `web/app/e2e/tests/visual/README.md` for what belongs there.
Two constraints that decide whether a screen *can* be covered at all:

- **It must be deterministic.** Nothing downstream of an assistant reply,
  no timestamps, no generated names. Existing specs use fixed persona names
  for exactly this reason.
- **Baselines are generated in Docker**, never on a developer's machine —
  macOS font rasterization does not match CI's. Use
  `npm run e2e:mock-llm:visual:docker:update`.

A new spec's screenshot name becomes the screen's identity in every future
report, so name it for the screen (`gallery-empty`), not for the change.

## When to also run the suite

Run `npm run e2e:mock-llm:visual:docker` when you need to know whether the
branch changed a screen *by accident* — a shared component edit, a global
style change, a dependency bump. It renders the app and compares against the
committed baselines, which is the check this report deliberately does not do.

Expect it to take minutes and to need Docker. It is not part of producing a
design review and should not be run automatically as though it were.

## What the report will not tell you

Be straight about these when reporting findings:

- **The related-files and uncovered lists are name matches, not a dependency
  graph.** A shared component that renders a screen without sharing a word
  with its name will be missed.
- **Only screens with baselines appear.** Coverage is six screens' worth at
  the time of writing; "no screen changed" is not "nothing changed".
- **Byte-identical is the test for unchanged.** A re-encoded but visually
  identical baseline reads as changed, and should — it is worth asking about.

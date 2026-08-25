---
name: playwright-version
description: >-
  Upgrade or change the Playwright version in this repo without silently
  invalidating the committed visual baselines. Use when bumping
  `@playwright/test`, when Dependabot or `npm install` proposes a Playwright
  update, when the CI container tag and the installed version disagree, when
  visual specs fail with diffs that look like unexplained UI changes, or when
  adding a new place that needs to know the Playwright version. Also use to
  answer "which Playwright version are we on" and "why is the version pinned
  exactly".
---

# Playwright version: one source of truth

`web/app/package.json` → `devDependencies["@playwright/test"]`
is the source of truth. It is pinned **exactly** — `1.62.1`, never `^1.62.1`.

Everything else derives from it through one resolver:

```bash
web/app/e2e/scripts/playwright-version.sh   # prints e.g. 1.62.1
```

Its only consumer now is `scripts/ci-image.sh`, which folds the resolved
version into the CI base image's tag (see the `ci-image-version` skill for
that layer). Everything that used to read `playwright-version.sh` directly —
`e2e/scripts/visual-docker.sh`, `.github/workflows/e2e-mock.yml`'s `version`
job — now calls `scripts/ci-image.sh e2e` instead, so both resolve the same
baked image ref rather than each deriving a bare Playwright tag on its own.

## Why exact, and why this matters more than a normal dependency

The PNGs under `e2e/tests/visual/**-snapshots/` were rasterized by the browsers
bundled inside one specific CI base image (`docker/ci/Dockerfile`'s `e2e`
target, tagged `...:e2e-pwX.Y.Z-<digest>`). Compare them using a different
image and you are checking against a renderer that never produced them — font
hinting and antialiasing shift, and the failures read as real UI regressions.
Version skew here does not break the build honestly; it produces **plausible,
wrong diffs**.

A range makes that skew invisible: `npm install` or a bot can move the
installed version inside `^1.62.1` while the image tags stay literal. Exact
pinning makes the version a fact rather than a constraint, which is what lets
an image tag be derived from it at all.

`playwright-version.sh` enforces both halves — it refuses a range, and it
refuses a `node_modules` that disagrees with `package.json`.

## Upgrading

1. **Bump the pin** in `web/app/package.json` to the new exact
   version, then `npm install` so the lockfile follows.
2. **Confirm the resolver agrees:**
   ```bash
   web/app/e2e/scripts/playwright-version.sh
   ```
   A mismatch error here means the install did not take.
3. **The CI base image updates itself.** `scripts/ci-image.sh` resolves the
   version through this same script and folds it into the image tag it
   computes (`ghcr.io/theimaginaryfoundation/what-iff-ci:e2e-pw<version>-<hash>`),
   so `visual-docker.sh` and the `ci-image.yml`/`e2e-mock.yml` workflows all
   pick up the new Playwright base image automatically — nothing to edit by
   hand. If the OS suffix Playwright publishes under has moved on (Jammy →
   Noble), that's the one thing still hardcoded, in `docker/ci/Dockerfile`'s
   `FROM mcr.microsoft.com/playwright:v${PLAYWRIGHT_VERSION}-noble` line.
4. **Regenerate the visual baselines in the new image** — this is the step
   that is easy to skip and expensive to skip:
   ```bash
   cd web/app && npm run e2e:mock-llm:visual:docker:update
   ```
5. **Review the regenerated PNGs before committing.** A version bump legitimately
   shifts rendering by a pixel or two; a real UI change looks different. If you
   cannot tell them apart, that is a signal to bump the version in its own
   commit, separate from any UI work, so the diff has one cause.
6. Run the visual specs once more without `:update` to confirm they now pass
   against the baselines you just made.

## Do not

- **Do not hardcode a version anywhere new.** Call the resolver. The reason CI
  needs a separate `version` job is that `container.image` is evaluated before
  a job's steps run, so it can only read `needs.*.outputs` — that indirection
  exists precisely to avoid a second hardcoded copy.
- **Do not "fix" a failing visual spec by regenerating baselines** unless you
  have established the version is in sync. Regenerating hides both a real UI
  regression and a version skew, and the two are indistinguishable afterwards.
- **Do not relax the pin to a range** to quiet a dependency bot. Pin the new
  exact version instead, and take step 4 with it.

## Symptoms that point here

- Visual specs fail in CI but pass locally, or vice versa.
- `playwright-version: installed … != declared …` — run `npm ci`.
- Playwright errors about the driver and browser versions not matching: the
  container tag and `@playwright/test` have drifted apart.
- Baseline diffs across many unrelated screens at once — a renderer change,
  not a UI change.

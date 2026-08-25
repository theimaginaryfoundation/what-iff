# Visual

`expect(page).toHaveScreenshot()` specs with committed baselines, tagged
`@mock-only` and `@visual`. Chromium-only (`chromium-desktop` +
`chromium-mobile` — `webkit-mobile` is excluded at the config level in
`playwright.config.base.ts`).

Six screens are covered so far — the app's stable, deterministic surfaces
only, nothing downstream of an assistant reply:

| Spec                              | Screens                                                        |
| --------------------------------- | -------------------------------------------------------------- |
| `auth.visual.spec.ts`             | login page, register page                                      |
| `personalities.visual.spec.ts`    | empty personalities list (fresh user), personality detail page |
| `chat.visual.spec.ts`             | chat composer before any message is sent                       |
| `profile-settings.visual.spec.ts` | Profile & Settings modal, profile tab                          |

These specs run under `e2e/playwright.config.mock-llm.visual.ts` (`npm run e2e:mock-llm:visual`),
which extends the mock config and sets reduced motion on each project, so no
spec has to opt into it. `visual.helpers.ts` holds `commonMasks()` (sidebar
username/avatar button). Baselines are generated and checked inside the official Playwright
Docker image so macOS font rendering never fights CI — see "Visual
regression" → "Updating snapshots" in `e2e/README.md` for the exact
commands and the Docker networking recipe that makes it work on macOS.

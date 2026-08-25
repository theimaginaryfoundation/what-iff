/**
 * Named call-level timeout budgets for the e2e suite: each one is passed to a
 * single `waitFor`/`expect` and answers "how long is this one thing allowed to
 * take". They are shared across environments because the thing being waited
 * for is the same everywhere.
 *
 * These exist so identical millisecond values that serve different purposes
 * don't drift silently — grouping by reason (not by number) makes it obvious
 * when a call site is using the wrong budget, and lets each budget's reason
 * change independently.
 *
 * Run-level budgets (a whole test, the dev-server boot) are NOT here. Each
 * playwright.config.<env>.ts sets its own inline, because what makes a run
 * slow is a property of that environment. Every value below must stay under
 * the smallest of those: a call-level budget that exceeds the test timeout
 * doesn't fail where you'd expect — the test is killed by the run-level
 * timeout, so the failure names the test rather than the assertion that
 * actually overran, and the trace stops short of it.
 *
 * Sizing basis (measured, not guessed) — 117 tests, all three projects, in the
 * CI container:
 *
 *     median 12.1s · p90 16.3s · p95 16.9s · p99 29.6s · max 30.3s
 *
 * The same suite locally: median 8.1s, max 23.8s. CI runs about 1.4x slower at
 * both ends, so local timings predict CI closely. The p95-to-p99 cliff is the
 * three journey specs, which genuinely do a full register-through-chat flow.
 */

// --- Run-level, per environment ---------------------------------------------
//
// Consumed by the matching playwright.config.<env>.ts. They live here so the
// four sit side by side and can be compared, but each config still applies its
// own — nothing inherits a shared default.

/** Mock backend: replies echo in-process. 1.5x the 30.3s CI maximum. */
export const MOCK_TEST_TIMEOUT = 45_000;

/** Local backend running a real small model on the same machine. */
export const LOCAL_TEST_TIMEOUT = 45_000;

/**
 * The Playwright API-test suite (e2e/api-tests/): no browser to add latency,
 * but the messages spec polls a background job for the mock LLM's reply, so
 * this isn't as tight as a pure HTTP round trip. Same value as
 * MOCK_TEST_TIMEOUT for now since both hit the same mock backend; kept as
 * its own constant so it can move independently once there's real data.
 */
export const API_TEST_TIMEOUT = 45_000;

/**
 * An auth/navigation redirect completing (e.g. away from /auth/register
 * after submit, or to /auth/login after logout). Generous enough to absorb
 * a slow dev-server response without being so long that a genuine hang goes
 * unnoticed.
 */
export const AUTH_REDIRECT_TIMEOUT = 15_000;

/**
 * An LLM assistant reply arriving. Sized for the slowest environment it runs
 * against: a real model behind a network hop, where the mock backend echoes
 * near-instantly. Kept below the tightest test timeout (45s on mock/local) so
 * a reply that never arrives fails on *this* assertion rather than being cut
 * off by the run-level timeout.
 */
export const LLM_REPLY_TIMEOUT = 40_000;

/**
 * Default for every `expect()` that doesn't name its own budget — set on
 * `baseConfig.expect` so all four environments inherit it.
 *
 * Playwright's own default is 5s, which was the wrong side of the CI/local
 * gap: at ~1.4x, an assertion passing locally at 4s has no margin in CI. Two
 * real failures in this suite came from exactly that — a streaming reply body
 * and an async billing gate, both of which pass locally and lose the race
 * under CI load. 10s costs nothing on a passing run (assertions resolve as
 * soon as they're true) and only lengthens genuine failures.
 */
export const ASSERTION_TIMEOUT = 10_000;

/**
 * Probing for optional, possibly-absent UI (e.g. a one-time announcement
 * modal) — short because the expected outcome is often "not shown", and a
 * long timeout here would just slow down the common case.
 */
export const OPTIONAL_UI_PROBE_TIMEOUT = 5_000;

/**
 * A UI reaction to purely local/optimistic state — not a network round trip —
 * such as the "Stop response" button appearing right after a message is sent,
 * or a message rendering once a background refetch (e.g. `reloadActiveThread`
 * on tab return) resolves. Tighter than ASSERTION_TIMEOUT because nothing here
 * is waiting on an LLM or a slow endpoint.
 */
export const UI_REACTION_TIMEOUT = 5_000;

/**
 * Regression test for PR #322: asserts a UI state change happens
 * *immediately* rather than only once a whole backend job reaches a terminal
 * status. Deliberately far tighter than any other budget here — the point of
 * the assertion is the tightness itself, so this must not be merged with a
 * looser constant even if the millisecond value happens to match one later.
 */
export const IMMEDIATE_UI_UPDATE_TIMEOUT = 1_000;

/**
 * Waiting for the backend to acknowledge a write the UI has already applied
 * optimistically — see `ThreadListPanel.waitForThreadPatch`.
 *
 * Deliberately larger than ASSERTION_TIMEOUT, because it measures something
 * different. An assertion waits for state the browser already holds; this
 * waits for a request to reach a containerized API and come back. Reusing the
 * assertion budget here is what made the rename spec flaky on `webkit-mobile`
 * in CI: 10s was ample locally and too tight on a contended runner.
 *
 * Still well under the tightest test timeout (45s on mock/local), so a genuine
 * miss fails with "no PATCH observed" rather than being cut off by the
 * run-level timeout, which names the test instead of the cause.
 */
export const MUTATION_ACK_TIMEOUT = 20_000;

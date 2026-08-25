import { createHash, randomUUID } from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { gzip as gzipCallback } from 'node:zlib';
import { test as base, type BrowserContext, type CDPSession, type Page } from '@playwright/test';

const gzip = promisify(gzipCallback);

/**
 * Frontend code coverage for the Playwright suite (`web-e2e` in Codecov).
 *
 * Off unless `E2E_COVERAGE=1`, and off on any engine but Chromium — V8
 * coverage is a CDP feature, so `page.coverage` does not exist on WebKit.
 * When off, every export here is a pass-through and the suite behaves
 * exactly as it did before this file existed.
 *
 * What it produces: one raw JSON file per test under `coverage/web-e2e/raw/`,
 * holding the V8 entries that test executed. Nothing is analysed here — the
 * shards only collect. `e2e/scripts/merge-coverage.mjs` turns the whole
 * collection into a single `lcov.info` afterwards, which is what gets
 * uploaded.
 */
export const COVERAGE_ENABLED = process.env['E2E_COVERAGE'] === '1';

/**
 * Root of the raw-entry collection. Overridable so a CI shard can write
 * somewhere the artifact upload owns without the suite hard-coding a path.
 * Relative to the app root (`web/app/`), matching how every
 * other Playwright output directory in this repo is rooted.
 */
export const RAW_COVERAGE_DIR = path.resolve(
  __dirname,
  '..',
  '..',
  process.env['E2E_COVERAGE_RAW_DIR'] ?? 'coverage/web-e2e/raw',
);

/**
 * A V8 coverage entry as Playwright hands it back, plus the `sourceMap` slot
 * monocart reads. Playwright's own type has no `sourceMap`, and the entries
 * are otherwise passed through untouched, so this is deliberately loose.
 */
export interface RawCoverageEntry {
  url: string;
  scriptId?: string;
  source?: string;
  functions?: unknown[];
  sourceMap?: unknown;
}

/**
 * Coverage is started once per page and never twice: the `page` fixture below
 * starts it explicitly (awaited, before anything can navigate) while
 * `context.on('page')` starts it for any *other* page the test opens. Both
 * paths can fire for the same page, so the in-flight promise is memoized and
 * the second caller simply awaits the first.
 */
const startedPages = new WeakMap<Page, Promise<void>>();

function startCoverage(page: Page): Promise<void> {
  let started = startedPages.get(page);
  if (!started) {
    // `resetOnNavigation: false` is the whole point — a test navigates
    // several times (login, then the app), and the default would throw away
    // everything executed before the last `goto`.
    started = page.coverage.startJSCoverage({ resetOnNavigation: false }).catch((err: unknown) => {
      // Never fail a test over coverage. A page that closed underneath us, or
      // a target that went away mid-start, costs one test's attribution — not
      // the run.
      console.warn(`coverage: could not start on ${page.url()}:`, err);
    });
    startedPages.set(page, started);
  }
  return started;
}

async function stopCoverage(page: Page): Promise<RawCoverageEntry[]> {
  const started = startedPages.get(page);
  if (!started) return [];
  startedPages.delete(page);
  await started;
  // Timeouts destroy the page before teardown reaches it, and a popup can
  // close itself; either way the CDP session is gone and there is nothing
  // left to collect.
  if (page.isClosed()) return [];
  try {
    return (await page.coverage.stopJSCoverage()) as RawCoverageEntry[];
  } catch (err) {
    console.warn(`coverage: could not stop on ${page.url()}:`, err);
    return [];
  }
}

/**
 * Entries worth keeping. V8 reports every script the page ever parsed,
 * including `chrome-extension://` and `data:` URLs and anything the browser
 * itself injects; only scripts the app actually served can map back to
 * `src/`, and the rest would be dead weight in the artifact.
 *
 * `origin` is the document's own origin when the caller knows it. Nothing the
 * app loads from a third party can map back to `src/`, and downstream tooling
 * that identifies which build a collection measured needs every entry to come
 * from a single origin. Today's only third-party script (`js.stripe.com/v3/`)
 * has no `.js` pathname and is dropped by the test below regardless, so this
 * is here for the next one rather than for that one.
 */
function isApplicationEntry(entry: RawCoverageEntry, origin?: string | null): boolean {
  let url: URL;
  try {
    url = new URL(entry.url);
  } catch {
    return false;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return false;
  if (origin && url.origin !== origin) return false;
  // Inline `<script>` blocks are reported under the document URL and carry no
  // sourcemap, so they can never resolve back to a `src/` file.
  if (!/\.m?js$/.test(url.pathname)) return false;
  // Vendor code. The dev server hands pre-bundled dependencies out of the Vite
  // cache under `/@fs/`; the merge step would discard them anyway, but they are
  // two thirds of the collected bytes, so they are dropped before they are ever
  // written.
  return !url.pathname.startsWith('/@fs/') && !url.pathname.includes('/node_modules/');
}

/**
 * Source text and sourcemaps are identical across every test in a run — the
 * dev server serves one `main.js` — so they are written once, content-addressed,
 * and referenced by hash from the per-test files. Storing them inline instead
 * would multiply a multi-megabyte bundle by the test count and make the CI
 * artifact unusable.
 */
const sourcesDir = path.join(RAW_COVERAGE_DIR, '..', 'sources');
const writtenSources = new Set<string>();
const sourceMapCache = new Map<string, unknown>();

/**
 * Resolves the `//# sourceMappingURL=` of a script into an actual sourcemap
 * object.
 *
 * This has to happen here, in the shard, while the dev server is still up:
 * the map is served over HTTP and the merge job runs later, on another
 * machine, with nothing left to fetch from. Monocart accepts a `sourceMap`
 * on a raw entry precisely so a producer can resolve maps ahead of time.
 *
 * Fetched with plain `fetch` rather than Playwright's `request` fixture: the
 * server is reachable from the test process either way, and this keeps the
 * coverage fixtures from pulling an extra `APIRequestContext` into every
 * test's dependency graph.
 */
async function resolveSourceMap(entry: RawCoverageEntry): Promise<unknown> {
  const source = entry.source;
  if (!source) return undefined;

  // Last match, not first: the convention is last-wins, and a bundle that
  // concatenates a dependency carrying its own trailing comment would
  // otherwise be mapped through the dependency's sourcemap — which resolves
  // cleanly and attributes the whole bundle to the wrong files.
  const matches = [...source.matchAll(/\/\/[#@]\s*sourceMappingURL=(\S+)\s*$/gm)];
  const match = matches.at(-1);
  if (!match) return undefined;
  const ref = match[1];

  if (ref.startsWith('data:')) {
    const base64 = ref.slice(ref.indexOf(',') + 1);
    try {
      return JSON.parse(Buffer.from(base64, 'base64').toString('utf8'));
    } catch (err) {
      console.warn(`coverage: unreadable inline sourcemap on ${entry.url}:`, err);
      return undefined;
    }
  }

  const mapUrl = new URL(ref, entry.url).href;
  if (sourceMapCache.has(mapUrl)) return sourceMapCache.get(mapUrl);

  try {
    const response = await fetch(mapUrl);
    if (!response.ok) {
      console.warn(`coverage: sourcemap ${mapUrl} returned ${response.status}`);
      sourceMapCache.set(mapUrl, undefined);
      return undefined;
    }
    const map: unknown = await response.json();
    sourceMapCache.set(mapUrl, map);
    return map;
  } catch (err) {
    console.warn(`coverage: could not fetch sourcemap ${mapUrl}:`, err);
    sourceMapCache.set(mapUrl, undefined);
    return undefined;
  }
}

/**
 * Writes a script's text and resolved sourcemap to `sources/` and returns the
 * hash the per-test file will reference it by.
 *
 * Called at harvest time rather than at teardown so the bundle text can be
 * released as soon as it is on disk. Holding it until teardown meant a test
 * that navigated repeatedly kept one multi-megabyte copy per navigation alive
 * in the worker at once, for no gain: the bytes are identical every time, and
 * content-addressing collapses them to a single file regardless.
 */
async function persistSource(entry: RawCoverageEntry): Promise<string> {
  const sourceHash = createHash('sha1').update(entry.source!).digest('hex');
  const sourceFile = path.join(sourcesDir, `${sourceHash}.json.gz`);
  if (!writtenSources.has(sourceFile)) {
    await fs.mkdir(sourcesDir, { recursive: true });
    await writeOnce(
      sourceFile,
      await gzipJson({ url: entry.url, source: entry.source, sourceMap: await resolveSourceMap(entry) }),
    );
  }
  return sourceHash;
}

/**
 * Per-test collection. Playwright runs one test at a time per worker process,
 * so module-level state is per-test state — no keying by test id needed.
 */
interface TestCoverage {
  /** Entries already harvested from pages the test closed before it ended. */
  entries: BankedEntry[];
  /** Pages still open, to be harvested at teardown. */
  pages: Set<Page>;
  /**
   * Top-level document navigations that replaced a document before its coverage
   * was banked. Each one is coverage this test measured and then threw away, so
   * a non-zero count means the reported percentage is an undercount rather than
   * a result. See `watchNavigations`.
   */
  lostNavigations: number;
}

/**
 * A harvested entry once its source text is on disk: everything the merge
 * needs, minus the megabytes.
 */
interface BankedEntry {
  url: string;
  scriptId?: string;
  functions?: unknown[];
  sourceHash: string;
}

let current: TestCoverage | null = null;

/** True only while a Chromium test with `E2E_COVERAGE=1` is running. */
let coverageActive = false;

const patchedContexts = new WeakSet<BrowserContext>();

/**
 * V8 coverage lives and dies with the page's CDP session, and `page.on('close')`
 * fires *after* that session is gone — too late to collect anything. So closing
 * is intercepted instead: any page or context closed mid-test hands its
 * coverage over on the way out.
 */
function patchClose(page: Page): void {
  const closePage = page.close.bind(page);
  page.close = async (options) => {
    await flushPage(page);
    return closePage(options);
  };

  const context = page.context();
  if (patchedContexts.has(context)) return;
  patchedContexts.add(context);
  const closeContext = context.close.bind(context);
  context.close = async (options) => {
    await Promise.all([...(current?.pages ?? [])].filter((p) => p.context() === context).map(flushPage));
    return closeContext(options);
  };
}

/** Per-page state for the navigation-boundary harvest. */
interface NavigationWatch {
  /** Held only to keep the session from being collected while the page lives. */
  session: CDPSession;
  /**
   * Only the top-level document matters. Stripe's iframes are document
   * navigations too, and counting them would report losses that never
   * happened.
   */
  mainFrameId: string;
  /** Whether the in-flight navigation had its coverage banked before it left. */
  banked: boolean;
}

const navigationWatches = new WeakMap<Page, NavigationWatch>();

/**
 * Methods that replace the page's document. Client-side routing is invisible to
 * V8 coverage, but these four are not.
 */
const NAVIGATION_METHODS = ['goto', 'reload', 'goBack', 'goForward'] as const;

/**
 * Banks coverage before each full-document navigation and starts a fresh
 * recording afterwards.
 *
 * `startJSCoverage({ resetOnNavigation: false })` does not survive one:
 * `Profiler.takePreciseCoverage` only reports scripts V8 still holds, and the
 * old document's scripts are gone by the time the new one loads. Measured
 * locally — a test that rendered `/auth/login` and then navigated to
 * `/auth/register` produced lcov containing `register.component.ts` and no
 * `login.component.ts` at all. The counts have to be taken while the document
 * that produced them is still alive.
 *
 * This only sees navigations the *test* starts. Catching the rest — a link
 * click, a form submit, a `location.href =` — needs a boundary inside the
 * browser, and the obvious one does not work: pausing the outgoing document
 * request with CDP `Fetch.requestPaused` blocks the renderer, so the
 * `Profiler.takePreciseCoverage` call needed to bank the coverage deadlocks
 * against the very pause that was meant to create the opportunity. That holds
 * at both `requestStage: 'Request'` and `'Response'`. `watchNavigations` below
 * therefore observes rather than intercepts, and counts what gets away.
 */
function patchNavigation(page: Page): void {
  const target = page as unknown as Record<string, (...args: unknown[]) => Promise<unknown>>;
  for (const name of NAVIGATION_METHODS) {
    const navigate = target[name].bind(page);
    target[name] = async (...args: unknown[]) => {
      if (current?.pages.has(page)) {
        // Set before the harvest rather than after: it records that this
        // navigation was seen at the right moment, so `watchNavigations` does
        // not count it. A harvest that then fails warns on its own, and is a
        // different fault from one that never got the chance to run.
        const watch = navigationWatches.get(page);
        if (watch) watch.banked = true;
        await harvest(page);
        await startCoverage(page);
      }
      return navigate(...args);
    };
  }
}

/**
 * Counts the top-level navigations that replaced a document before
 * `patchNavigation` banked its coverage.
 *
 * The canary for the gap above. A document that arrives without a harvest
 * having run first is a navigation the wrapper did not see, and everything the
 * previous document covered is already gone. Counting it turns a silent hole —
 * a document nobody harvested looks exactly like a document nobody visited —
 * into a number the merge job reports next to the total, qualifying it.
 *
 * CDP `Page.frameNavigated` rather than Playwright's `page.on('framenavigated')`
 * because the latter also fires for pushState routing, which would report a
 * false loss on every SPA route change.
 */
async function watchNavigations(page: Page): Promise<void> {
  if (navigationWatches.has(page)) return;

  let session: CDPSession;
  try {
    session = await page.context().newCDPSession(page);
  } catch (err) {
    // Costs this page its loss counting, not the test.
    console.warn(`coverage: could not open a CDP session on ${page.url()}:`, err);
    return;
  }

  const watch: NavigationWatch = { session, mainFrameId: '', banked: false };
  navigationWatches.set(page, watch);

  try {
    const { frameTree } = await session.send('Page.getFrameTree');
    watch.mainFrameId = frameTree.frame.id;

    session.on('Page.frameNavigated', ({ frame }) => {
      if (frame.id !== watch.mainFrameId || frame.parentId) return;
      // `pages` is the enrolment check as well as the null check: a page the
      // fixture is not recording has nothing to lose, and counting its
      // navigations would report losses that never happened.
      if (!watch.banked && current?.pages.has(page)) current.lostNavigations += 1;
      watch.banked = false;
    });

    await session.send('Page.enable');
  } catch (err) {
    console.warn(`coverage: could not watch navigations on ${page.url()}:`, err);
  }
}

/**
 * Starts coverage on a page and enrols it in the current test's collection.
 * Safe to call twice on the same page — the second call awaits the first.
 */
export async function attachCoverage(page: Page): Promise<void> {
  if (!coverageActive) return;
  current ??= { entries: [], pages: new Set(), lostNavigations: 0 };
  if (!current.pages.has(page)) {
    current.pages.add(page);
    patchClose(page);
    patchNavigation(page);
    // Awaited before `startCoverage`: the watcher has to be listening before
    // anything can navigate this page out from under the recording, or the
    // first navigation goes uncounted as well as unbanked.
    await watchNavigations(page);
  }
  // Always awaited, even on the second call: the `context.on('page')` listener
  // and the `page` fixture both land here for the same page, and the fixture's
  // await is what guarantees coverage is running before the test navigates.
  await startCoverage(page);
}

/**
 * Banks what a page has recorded so far, leaving it enrolled in the test.
 * Returns how many of the banked entries are first-party application code.
 */
async function harvest(page: Page): Promise<number> {
  const collected = current;
  if (!collected?.pages.has(page)) return 0;
  // Read before stopping: `stopCoverage` can await past a navigation that has
  // already begun, and the entries just recorded belong to the document that
  // is still showing.
  const origin = pageOrigin(page);
  const entries = (await stopCoverage(page)).filter((entry) => entry.source && isApplicationEntry(entry, origin));
  for (const entry of entries) {
    collected.entries.push({
      url: entry.url,
      scriptId: entry.scriptId,
      functions: entry.functions,
      sourceHash: await persistSource(entry),
    });
  }
  return entries.length;
}

/**
 * The origin a page's scripts have to be served from to count as first-party.
 *
 * Read off the document rather than off a configured base URL so it holds for
 * a deployed run too, where the host is whatever the suite was pointed at.
 * Null before the page reaches an http(s) document (`about:blank`), which is
 * also when it has nothing worth filtering.
 */
function pageOrigin(page: Page): string | null {
  try {
    const url = new URL(page.url());
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.origin : null;
  } catch {
    return null;
  }
}

/**
 * How many top-level navigations in the current test replaced a document before
 * its coverage was banked.
 *
 * Exists for `tests/functional/coverage/collection.spec.ts`. The merge job
 * reports the run-wide total, but a spec that deliberately performs a
 * page-initiated navigation has to assert the interception saw that one, and a
 * total summed across the whole suite cannot answer that. Returns 0 when
 * coverage is off.
 */
export function coverageLostNavigations(): number {
  return current?.lostNavigations ?? 0;
}

/**
 * Banks `page`'s coverage mid-test and reports how many first-party entries it
 * yielded, leaving the recording running.
 *
 * Exists for `tests/functional/coverage/collection.spec.ts`, which has to assert
 * that a hand-built context's page is being recorded. That cannot be checked
 * from the merged lcov: a page created by `browser.newContext()` runs the same
 * components as the fixture's page, so its contribution is indistinguishable
 * once everything is merged. Returns 0 when coverage is off.
 */
export async function coverageEntryCount(page: Page): Promise<number> {
  if (!coverageActive) return 0;
  const banked = await harvest(page);
  await startCoverage(page);
  return banked;
}

/**
 * The floor `coverageEntryCount` must clear for a page the suite has actually
 * driven: 1 when collection is live, 0 wherever it is inert. Lives here rather
 * than in the spec so the acceptance test can assert unconditionally — the
 * suite's eslint config rejects branching inside a test body.
 */
export function expectedRecordedScripts(browserName: string): number {
  return COVERAGE_ENABLED && browserName === 'chromium' ? 1 : 0;
}

/** Banks a page's coverage and drops it — it is closing or the test is over. */
async function flushPage(page: Page): Promise<void> {
  if (!current?.pages.has(page)) return;
  await harvest(page);
  current.pages.delete(page);
}

/**
 * Everything on disk here is gzipped. Uncompressed, a shard's collection runs
 * to hundreds of megabytes — mostly long runs of integers and repeated bundle
 * text, which deflate reduces by an order of magnitude. It is the difference
 * between an artifact CI can move around and one it can't.
 */
function gzipJson(value: unknown): Promise<Buffer> {
  return gzip(JSON.stringify(value));
}

/** Writes `file` only if it isn't there yet, atomically enough for parallel workers. */
async function writeOnce(file: string, contents: Buffer): Promise<void> {
  if (writtenSources.has(file)) return;
  writtenSources.add(file);
  try {
    await fs.access(file);
    return;
  } catch {
    // Not present yet — fall through and write it.
  }
  // Written to a per-process temp name and renamed into place: several workers
  // discover the same bundle at the same moment, and a half-written file would
  // fail the merge with no obvious cause.
  const temp = `${file}.${process.pid}.tmp`;
  await fs.writeFile(temp, contents);
  await fs.rename(temp, file);
}

/**
 * Harvests every page the test touched and writes one raw file for it.
 *
 * The entries are already banked and their script text is already on disk —
 * `harvest` does that as each page gives its counts up. All that is left here
 * is draining the pages still open and writing the per-test index.
 * `e2e/scripts/merge-coverage.mjs` puts the two halves back together.
 */
async function finalize(testTitle: string): Promise<void> {
  const collected = current;
  if (!collected) return;

  // Drained before `current` is cleared: `flushPage` works against the live
  // collection, so clearing first would silently drop every still-open page.
  await Promise.all([...collected.pages].map(flushPage));
  current = null;

  if (collected.lostNavigations > 0) {
    // Warned per test as well as counted, because the aggregate number in the
    // run summary says how much was lost but not which test lost it.
    console.warn(
      `coverage: ${collected.lostNavigations} navigation(s) in "${testTitle}" replaced the ` +
        'document before their coverage was banked; that coverage is missing from the report',
    );
  }

  if (collected.entries.length === 0) return;

  await fs.mkdir(RAW_COVERAGE_DIR, { recursive: true });
  await fs.writeFile(
    path.join(RAW_COVERAGE_DIR, `${randomUUID()}.json.gz`),
    await gzipJson({ testTitle, entries: collected.entries, lostNavigations: collected.lostNavigations }),
  );
}

/**
 * The suite's base `test`, with coverage collection woven in. `fixtures/index.ts`
 * builds on this rather than on `@playwright/test` directly, so every spec in
 * the suite gets it without opting in.
 *
 * The built-in `context` and `page` fixtures are *overridden* rather than
 * wrapped in an `auto` fixture: an override is guaranteed to run before
 * anything that depends on `page` (`authenticatedPage`, and the specs
 * themselves), which is what makes "coverage is started before the first
 * navigation" a structural property instead of an ordering coincidence.
 */
export const test = base.extend<{ coverageSession: void }>({
  /**
   * Owns the per-test collection window. `auto`, because a test that takes only
   * `browser` and builds its own context — `profile.spec.ts` does — never
   * instantiates `context`, and would otherwise find `attachCoverage` inert.
   * `context` below depends on it so the ordering is a dependency rather than a
   * coincidence: setup first, teardown (and so `finalize`) last.
   */
  coverageSession: [
    async ({ browserName }, use, testInfo) => {
      coverageActive = COVERAGE_ENABLED && browserName === 'chromium';
      if (coverageActive) current = { entries: [], pages: new Set(), lostNavigations: 0 };

      await use();

      if (coverageActive) {
        coverageActive = false;
        try {
          await finalize(testInfo.titlePath.join(' > '));
        } catch (err) {
          console.warn('coverage: could not write raw coverage:', err);
        }
      }
    },
    { auto: true },
  ],

  context: async ({ context, coverageSession }, use) => {
    void coverageSession; // depended on for ordering only
    if (coverageActive) {
      // Pages the test opens itself (`context.newPage()`, popups) are caught
      // here; the `page` override below covers the one Playwright creates.
      context.on('page', (page) => void attachCoverage(page));
      await Promise.all(context.pages().map(attachCoverage));
    }

    await use(context);

    // Drained here, not left to `finalize`: Playwright closes this context as
    // soon as the override returns, and a closed page has no coverage to give.
    if (coverageActive) {
      await Promise.all([...(current?.pages ?? [])].filter((page) => page.context() === context).map(flushPage));
    }
  },

  page: async ({ page }, use) => {
    await attachCoverage(page);
    await use(page);
  },
});

export { expect } from '@playwright/test';

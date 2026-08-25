import { errors, type Locator, type Page } from '@playwright/test';
import { OPTIONAL_UI_PROBE_TIMEOUT } from '../timeouts';

/**
 * Sidebar nav destinations, keyed the way tests talk about them. The sidebar
 * splits these across two "modes" (layout/sidebar/nav.helpers.ts): app mode
 * shows chat/personalities/gallery, config mode shows the rest. `clickThroughTo()` hides
 * that split.
 */
export const navSections = {
  chat: { label: 'Chat', route: '/chat', mode: 'app' },
  personalities: { label: 'Personalities', route: '/personality', mode: 'app' },
  gallery: { label: 'Gallery', route: '/gallery', mode: 'app' },
  memories: { label: 'Memories', route: '/memories', mode: 'config' },
  modes: { label: 'Modes', route: '/mode', mode: 'config' },
  skills: { label: 'Skills', route: '/skills', mode: 'config' },
  tools: { label: 'Tools', route: '/integrations', mode: 'config' },
  jobs: { label: 'Jobs', route: '/agent-jobs', mode: 'config' },
} as const;

export type NavSection = keyof typeof navSections;

/**
 * Sidebar quick actions, by their aria-label. Most are *contextual* — the
 * sidebar only renders the block they live in while the matching view is
 * open (e.g. "Add memory" exists only on /memories), which is why
 * `quickAction()` takes the section to be on first. "Open command palette"
 * and "Help and feedback" are the two that are always present.
 */
export const quickActions = [
  'New thread',
  'All threads',
  'Add memory',
  'Create skill',
  'Create mode',
  'Import Conversations',
  'Open command palette',
  'Help and feedback',
] as const;

export type QuickAction = (typeof quickActions)[number];

/**
 * The in-flight or settled announcement probe for a page. A `WeakMap` so a
 * closed page's entry disappears with it, and per-page rather than
 * per-`AppShell` because callers construct a new `AppShell(page)` each time.
 *
 * Every account in this suite is brand new, and `AnnouncementService` always
 * shows the modal to an account that has never seen it — so on the accounts
 * this suite creates, the modal is not "possibly absent", it is "definitely
 * coming, on its own async schedule" (an Angular effect behind a preferences
 * fetch). A boolean "did we see it within one probe" cache would be wrong: a
 * probe that misses by a few hundred milliseconds under load doesn't mean the
 * modal was never coming, and caching that as "settled" leaves it open to
 * intercept every later click for the rest of the test — the exact failure
 * mode a plain per-page boolean produced under contended CI/mobile runs.
 *
 * Memoizing the *promise* instead fixes both directions at once: the first
 * caller starts the one real probe (bounded, retried, exactly as before);
 * every other call site — `clickThroughTo()`, `prepareSidebar()`, `quickAction()`'s
 * chain of both — awaits that same probe instead of starting its own, so
 * there is one full-budget attempt per page rather than N independent
 * shorter gambles, and nothing returns before that attempt is actually done.
 */
const announcementProbe = new WeakMap<Page, Promise<void>>();

/**
 * Pages with a `'load'` listener already wired to clear their cached probe.
 * Several POMs (`ThreadListPanel.navigateTo()`, `PersonalitiesPage.navigateTo()`, and
 * others) call `page.goto(...)` directly rather than the in-app router, which
 * is a hard navigation: the whole Angular app — and with it the announcement
 * effect — reboots. `Page` outlives that; it is the tab, not the document, so
 * a probe result cached against the old document would wrongly stand in for
 * the new one. `'load'` fires only on a real navigation, never on the
 * Angular router's client-side route changes, so this clears exactly when it
 * needs to and no more often.
 */
const navigationResetWired = new WeakSet<Page>();

function ensureProbeResetsOnNavigation(page: Page): void {
  if (navigationResetWired.has(page)) {
    return;
  }
  navigationResetWired.add(page);
  page.on('load', () => announcementProbe.delete(page));
}

/** Chrome that wraps every authenticated page (sidebar, announcement modal). */
export class AppShell {
  constructor(private readonly page: Page) {
    this.recentThreadsSection = this.page.locator('.app-sidebar__recent');
  }

  /**
   * New accounts see a one-time "what's new" announcement modal that
   * intercepts clicks until dismissed; it can appear async after navigation.
   * Call this after landing on an authenticated page and before interacting
   * with anything in the main content area.
   */
  async dismissAnnouncementIfPresent(): Promise<void> {
    // Wired here rather than in the constructor: this is the only method that
    // reads or writes the probe, so first use is the earliest point the
    // listener can matter, and a POM constructor that attaches a page listener
    // would do it for every test that merely names the fixture.
    ensureProbeResetsOnNavigation(this.page);
    // See `announcementProbe` above: the first call for this page starts the
    // real probe below and every other call — concurrent or later — awaits
    // that same run rather than starting its own.
    const inFlight = announcementProbe.get(this.page);
    if (inFlight) {
      return inFlight;
    }
    const probe = this.probeAndDismissAnnouncement();
    announcementProbe.set(this.page, probe);
    return probe;
  }

  /**
   * The two timeouts below mean different things, which is the reason for the
   * two separate try blocks: a *visibility* timeout means no modal is there,
   * which is the common case and returns quietly; a *click* timeout means the
   * modal is there but won't accept input, which is a real failure and throws
   * once the retries are used up.
   */
  private async probeAndDismissAnnouncement(): Promise<void> {
    const gotIt = this.page.getByRole('button', { name: 'Got it' });
    // Bounded, and retried: more than one announcement can be queued, and a
    // click landing during the previous one's fade-out is swallowed by the
    // outgoing backdrop. Every wait has a timeout so a modal that refuses to
    // close fails the assertion that follows rather than hanging the test.
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        await gotIt.waitFor({
          state: 'visible',
          timeout: OPTIONAL_UI_PROBE_TIMEOUT,
        });
      } catch (err) {
        if (err instanceof errors.TimeoutError) {
          return; // Not shown (any more) — nothing to dismiss.
        }
        throw err; // Broken selector/detached locator — a real failure, surface it.
      }
      try {
        // No per-call timeout override here: click() is an auto-waiting
        // action, and the waitFor() above already established visibility, so
        // Playwright's default action timeout is enough.
        await gotIt.click();
      } catch (err) {
        // A visibility timeout above means "nothing to dismiss"; a timeout
        // here means the modal IS there but won't accept the click — that's
        // a real interaction failure, not an absence, so let it propagate.
        if (err instanceof errors.TimeoutError && attempt < 2) {
          continue; // Retry — the previous modal's fade-out may still be swallowing clicks.
        }
        throw err;
      }
      await gotIt.waitFor({ state: 'hidden', timeout: OPTIONAL_UI_PROBE_TIMEOUT }).catch(() => undefined);
    }
  }

  /** Sidebar button that opens the Profile & Settings modal. */
  openProfileButton() {
    return this.page.getByRole('button', { name: /^Open profile for/ });
  }

  /**
   * "Recent Threads" section of the sidebar. Its per-thread status icon
   * renders inconsistently run-to-run (a connection/read-state indicator
   * that hasn't always settled by screenshot time) even with masking
   * elsewhere on the page, so visual specs that reach a chat screen mask
   * this whole region rather than chase the exact flaky element.
   */
  readonly recentThreadsSection: Locator;

  /**
   * On narrow viewports (<1024px) the sidebar is off-canvas by default and
   * only reachable via a hamburger button in the main pane
   * (`app-layout__mobile-menu`, aria-label "Open navigation menu"). Desktop
   * layouts never render this button. Call before interacting with anything
   * inside the sidebar so the same POM works across all three projects.
   */
  async openMobileSidebarIfPresent(): Promise<void> {
    const menuButton = this.page.getByRole('button', {
      name: 'Open navigation menu',
    });
    if (await menuButton.isVisible().catch(() => false)) {
      await menuButton.click();
    }
  }

  /**
   * A collapsed desktop sidebar renders icon-only nav and drops the
   * contextual quick-action blocks entirely, so anything reaching for them
   * has to expand first. No-op when already expanded.
   */
  async expandSidebarIfCollapsed(): Promise<void> {
    const expand = this.page.getByRole('button', { name: 'Expand sidebar' });
    if (await expand.isVisible().catch(() => false)) {
      await expand.click();
    }
  }

  /** Dismiss announcement + open/expand the sidebar. Safe to call repeatedly. */
  async prepareSidebar(): Promise<void> {
    await this.dismissAnnouncementIfPresent();
    await this.openMobileSidebarIfPresent();
    await this.expandSidebarIfCollapsed();
  }

  /** The nav tab (link, or button for Chat) for a section, by its aria-label. */
  navTab(section: NavSection) {
    return this.page.getByLabel(navSections[section].label, { exact: true }).first();
  }

  /**
   * Goes to a section by URL. The suite's default way to reach a page: it is
   * one hop instead of a mode switch plus a click, it cannot be knocked off
   * course by whatever the previous test left on screen, and it doesn't make
   * every spec depend on the sidebar's markup. Reach for `clickThroughTo()`
   * only when exercising the click path is the point of the test.
   */
  async navigateTo(section: NavSection): Promise<void> {
    await this.page.goto(navSections[section].route);
    await this.dismissAnnouncementIfPresent();
  }

  /**
   * Navigates via the sidebar, switching nav mode first when the target lives
   * in the other one. Waits for the route so callers can assert on page
   * content immediately.
   *
   * Prefer `navigateTo()` unless the click path itself is under test — this is
   * the nav-wiring guard (tests/functional/nav/sidebar-nav.spec.ts), not a
   * general-purpose way to get somewhere.
   *
   * Note `clickThroughTo('chat')` clicks the Chat *toggle*: from the Thread Manager it
   * returns to the last open thread rather than staying put (see
   * `toggleThreadManager` in app-sidebar.component.ts).
   */
  async clickThroughTo(section: NavSection): Promise<void> {
    const target = navSections[section];
    await this.prepareSidebar();

    // Switch modes by probing for the *switcher*, not for the target tab: the
    // switcher into a mode only exists while in the other one, so this can't
    // misfire the way a "is the tab there yet?" probe can while the sidebar is
    // still rendering.
    const switcher =
      target.mode === 'config'
        ? this.page.getByRole('button', {
            name: /Switch to configuration mode|^Configuration$/,
          })
        : this.page.getByRole('button', {
            name: /Switch to app mode|^Exit config$/,
          });
    if (
      await switcher
        .first()
        .isVisible()
        .catch(() => false)
    ) {
      await switcher.first().click();
    }

    const tab = this.navTab(section);
    await tab.waitFor({ state: 'visible' });
    await tab.click();
    await this.page.waitForURL(new RegExp(`${target.route}(/|\\?|$)`));
  }

  /**
   * Clicks a sidebar quick action. The contextual ones only exist on their own
   * view, so pass `section` to have the shell navigate there first.
   */
  async quickAction(action: QuickAction, section?: NavSection): Promise<void> {
    if (section) {
      await this.clickThroughTo(section);
    }
    await this.prepareSidebar();
    await this.page.getByRole('button', { name: action, exact: true }).first().click();
  }
}

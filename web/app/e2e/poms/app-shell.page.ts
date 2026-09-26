import { type Locator, type Page } from '@playwright/test';

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

/** Chrome that wraps every authenticated page (the sidebar and its controls). */
export class AppShell {
  constructor(private readonly page: Page) {
    this.recentThreadsSection = this.page.locator('.app-sidebar__recent');
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

  /** Open/expand the sidebar. Safe to call repeatedly. */
  async prepareSidebar(): Promise<void> {
    // The probes below don't wait, so first wait for the shell itself: the sidebar, plus the
    // mobile menu button on narrow viewports, render together once the layout is up.
    await this.page.locator('aside.app-sidebar').waitFor({ state: 'attached' });
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

    // The switcher into a mode only exists while in the other one. Wait until
    // the sidebar shows either the target tab or that switcher, then switch
    // only if the tab isn't there. A bare isVisible() probe here doesn't wait,
    // so it misfires while the sidebar is still rendering.
    const switcher = (
      target.mode === 'config'
        ? this.page.getByRole('button', {
            name: /Switch to configuration mode|^Configuration$/,
          })
        : this.page.getByRole('button', {
            name: /Switch to app mode|^Exit config$/,
          })
    ).first();
    const tab = this.navTab(section);
    await tab.or(switcher).first().waitFor({ state: 'visible' });
    if (!(await tab.isVisible())) {
      await switcher.click();
    }

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

import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/**
 * The Quick Actions tiles on `/dashboard`, by their heading text, paired with
 * the route each one goes to.
 *
 * Distinct from `quickActions` in `app-shell.page.ts`, which are the sidebar's
 * contextual buttons — these are the three cards in the dashboard body.
 */
export const dashboardTiles = {
  chat: { name: 'Start New Chat', route: '/chat' },
  profile: { name: 'Profile Settings', route: '/profile' },
  memories: { name: 'Manage Memories', route: '/memories' },
} as const;

export type DashboardTile = keyof typeof dashboardTiles;

/**
 * Dashboard (`/dashboard`) — features/dashboard/dashboard.component.html.
 *
 * Reached from the profile page's back action and after a subscription change;
 * nothing in the sidebar links to it, so specs navigate here directly.
 */
export class DashboardPage {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.heading = this.page.getByRole('heading', { name: 'Quick Actions' });
  }

  readonly heading: Locator;

  async navigateTo(): Promise<void> {
    await this.page.goto('/dashboard');
    await this.shell.dismissAnnouncementIfPresent();
  }

  /**
   * The tile card. Each is a single `<a>` wrapping an icon, a heading and a
   * description, so the accessible name is all three joined — matched by
   * substring rather than `exact` for that reason.
   */
  tile(tile: DashboardTile): Locator {
    return this.page.getByRole('link', { name: dashboardTiles[tile].name });
  }
}

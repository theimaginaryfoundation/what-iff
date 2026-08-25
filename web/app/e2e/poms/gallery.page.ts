import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/** The page's two top-level modes, by their switch buttons' labels. */
export type GalleryMode = 'Gallery' | 'Expression Manager';

/** Source segment in the gallery toolbar. Labels carry a live count ("All 3"). */
export type GallerySource = 'All' | 'Generated' | 'Imported';

/** Sort segment in the gallery toolbar. Labels carry a direction arrow when active. */
export type GallerySort = 'Created' | 'Last used';

/**
 * Image gallery (`/gallery`) — features/gallery/gallery-page.component.html.
 *
 * NOTE: importing an image is not reachable from a hermetic run. The import
 * endpoint proxies the file to the vendor Files API through
 * `handlerutils.UploadFileAttachment` with no mock/local bypass, so under
 * `LLM_BACKEND=mock`/`local` every import fails on the provider egress guard —
 * the same constraint that keeps personality attachments untested (see
 * TEST_PLAN.md). Nothing here drives the import modal for that reason.
 */
export class GalleryPage {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    // The page region, named independently of the <h1>: that heading is the
    // active mode's name and changes as the user switches, so it cannot also
    // be the handle used to find the page.
    this.root = this.page.getByRole('region', { name: 'Image gallery' });
    // Scoped to the page: "Gallery" is also the sidebar's nav label, and the
    // mode switch repeats the same two words as the heading.
    this.heading = this.root.getByRole('heading', { level: 1 });
    this.modeSwitch = this.root.getByRole('group', { name: 'Gallery mode' });
    this.sourceGroup = this.root.getByRole('group', { name: 'Filter by source' });
    this.sortGroup = this.root.getByRole('group', { name: 'Sort images' });
    this.searchInput = this.root.getByLabel('Search images');
    this.resultsSummary = this.root.getByText(/\d+ of \d+ shown/);
    this.grid = this.page.getByRole('list', { name: 'Gallery images' });
    this.emptyMessage = this.page.getByText('No images match these filters yet.');
    this.loadingStatus = this.page.getByRole('status', { name: 'Loading gallery' });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/gallery');
    await this.shell.dismissAnnouncementIfPresent();
  }

  readonly root: Locator;

  /** `<h1>`, which is the active mode's name — "Gallery" or "Expression Manager". */
  readonly heading: Locator;

  readonly modeSwitch: Locator;

  /** The "Filter by source" segmented group. */
  readonly sourceGroup: Locator;

  /** The "Sort images" segmented group. */
  readonly sortGroup: Locator;

  /** Free-text filter over names, prompts and threads. Gallery mode only. */
  readonly searchInput: Locator;

  /** The "<n> of <m> shown" line beside the toolbar. Gallery mode only. */
  readonly resultsSummary: Locator;

  readonly grid: Locator;

  readonly emptyMessage: Locator;

  readonly loadingStatus: Locator;

  modeButton(mode: GalleryMode): Locator {
    return this.modeSwitch.getByRole('button', { name: mode, exact: true });
  }

  async setMode(mode: GalleryMode): Promise<void> {
    await this.modeButton(mode).click();
  }

  /**
   * A source segment. Matched on a leading-anchored pattern rather than an
   * exact name: each label ends in a live count that changes with the account's
   * contents, which no test can pin on a shared one.
   */
  sourceSegment(source: GallerySource): Locator {
    return this.sourceGroup.getByRole('button', { name: new RegExp(`^${source}\\b`) });
  }

  async filterBySource(source: GallerySource): Promise<void> {
    await this.sourceSegment(source).click();
  }

  /** A sort segment. Same leading-anchored match — the label gains "▼"/"▲" when active. */
  sortSegment(sort: GallerySort): Locator {
    return this.sortGroup.getByRole('button', { name: new RegExp(`^${sort}\\b`) });
  }

  async sortBy(sort: GallerySort): Promise<void> {
    await this.sortSegment(sort).click();
  }

  /**
   * Narrows the grid to images matching `query`.
   *
   * The narrowing affordance this page needs on a shared account: the grid is
   * the account's whole library, so an assertion about "the gallery" is really
   * an assertion about every other worker's images too.
   */
  async search(query: string): Promise<void> {
    await this.searchInput.fill(query);
  }
}

import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';
import { ConfirmationModal } from './confirmation.modal';

/**
 * Integrations (`/integrations`) — connectors + webhook API tokens.
 *
 * Everything below the header is gated on `hasSubscriptionAccess()`
 * (integrations.component.ts): it is true unconditionally when
 * `environment.requireBilling` is false — which is the case for the default
 * `environment.ts` the local dev server builds with — and otherwise depends on
 * `GET /billing/customer` reporting an active subscription. So the tabs are
 * reachable for a plain test user locally, but NOT against a build whose
 * environment sets `requireBilling: true`, where the `role="status"`
 * unavailable notice renders instead. Specs branch on `unavailableNotice`
 * rather than assuming either.
 */
export class IntegrationsPage {
  private readonly shell: AppShell;
  readonly confirmation: ConfirmationModal;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.confirmation = new ConfirmationModal(page);
    this.unavailableNotice = this.page.getByRole('status').filter({ hasText: 'Integrations are unavailable for this account' });
    this.connectorsTab = this.page.getByRole('button', {
      name: 'Connectors',
      exact: true,
    });
    this.webhooksTab = this.page.getByRole('button', {
      name: 'Webhooks',
      exact: true,
    });
    this.createTokenHeading = this.page.getByRole('heading', {
      name: 'Create Webhook API Token',
    });
    this.tokenNameInput = this.page.getByPlaceholder('e.g. Slack trigger');
    this.createTokenButton = this.page.getByRole('button', {
      name: /^(Create Token|Creating\.\.\.)$/,
    });
    this.newTokenBanner = this.page.getByRole('heading', {
      name: 'Copy your new API token now',
    });
    this.emptyTokensMessage = this.page.getByText('No webhook API tokens created yet.');
    this.heading = this.page.getByRole('heading', {
      name: 'Integrations',
      level: 1,
    });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/integrations');
    await this.shell.dismissAnnouncementIfPresent();
  }

  readonly heading: Locator;

  /** Shown in place of the tabs when the account has no billing access. */
  readonly unavailableNotice: Locator;

  /** Tabs are plain buttons in a <nav> — no tab/tablist roles. */
  readonly connectorsTab: Locator;

  readonly webhooksTab: Locator;

  async openConnectors(): Promise<void> {
    await this.connectorsTab.click();
  }

  async openWebhooks(): Promise<void> {
    await this.webhooksTab.click();
  }

  // --- webhooks tab --------------------------------------------------------

  readonly createTokenHeading: Locator;

  readonly tokenNameInput: Locator;

  readonly createTokenButton: Locator;

  async createToken(name: string): Promise<void> {
    await this.tokenNameInput.fill(name);
    await this.createTokenButton.click();
  }

  /** One-time banner carrying the raw token; only shown right after creation. */
  readonly newTokenBanner: Locator;

  async copyToken(): Promise<void> {
    await this.page.getByRole('button', { name: 'Copy Token' }).click();
  }

  async dismissTokenBanner(): Promise<void> {
    await this.page.getByRole('button', { name: 'Dismiss' }).click();
  }

  /**
   * A token's row in the list, located by its name heading.
   *
   * Scoped by `data-testid` rather than the `p-4` Tailwind class this used to
   * match: a padding utility is not a contract, it is shared with the
   * just-created banner and the sidebar card in the same template, and
   * retuning spacing would have broken this POM with nothing in the component
   * to warn whoever did it. The row is a structural container with no
   * accessible name of its own, so there is no aria-first locator to prefer
   * here — the heading inside it still does the identifying.
   */
  tokenRow(name: string): Locator {
    return this.page.getByTestId('webhook-token-row').filter({ has: this.page.getByRole('heading', { name, exact: true }) });
  }

  /** Revoking goes through the shared confirmation dialog ("Revoke API Token"). */
  async revokeToken(name: string): Promise<void> {
    await this.tokenRow(name).getByRole('button', { name: 'Revoke' }).click();
    await this.confirmation.confirm('Revoke');
  }

  readonly emptyTokensMessage: Locator;
}

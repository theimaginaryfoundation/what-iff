import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/**
 * Profile & Settings modal — see e2e/TEST_PLAN.md item 3. Opened via the
 * sidebar's "Open profile for <username>" button; a real modal (ui-modal),
 * not a route change.
 */
export class ProfileSettingsModal {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.identityCard = this.page.locator('.profile-settings__identity-card');
    this.emailInput = this.page.getByLabel('Email', { exact: true });
    this.firstNameInput = this.page.getByLabel('First Name');
    this.lastNameInput = this.page.getByLabel('Last Name');
    this.themeSelect = this.page.getByLabel('Theme');
    this.htmlElement = this.page.locator('html');
    this.profileSavedMessage = this.page.getByText('Profile updated.');
    this.passwordUpdatedMessage = this.page.getByText('Password updated.');
    this.logoutButton = this.page.getByRole('button', { name: 'Log Out' });
    this.heading = this.page.getByRole('heading', {
      name: 'Profile & Settings',
    });
  }

  readonly heading: Locator;

  /**
   * Identity card at the top of the Profile tab: avatar initials, display
   * name, email, and "Member since <date>" — all account-specific/dynamic,
   * so visual specs mask this whole region rather than individual fields.
   */
  readonly identityCard: Locator;

  readonly emailInput: Locator;

  readonly firstNameInput: Locator;

  readonly lastNameInput: Locator;

  readonly themeSelect: Locator;

  /**
   * `<html>` itself — `ThemeService`/`applyThemeAttribute()` set
   * `data-theme="light"|"dark"` on it, which is the one observable signal of
   * which resolved theme is actually applied (as opposed to the `<select>`'s
   * value, which only reflects the stored *mode*, including `'system'`).
   */
  readonly htmlElement: Locator;

  /** Toast shown after a successful "Save Changes". */
  readonly profileSavedMessage: Locator;

  /** Toast shown after a successful "Update Password". */
  readonly passwordUpdatedMessage: Locator;

  /**
   * The only logout affordance in the app: a "Log Out" button in the account
   * header of this modal (profile-settings-modal.component.html). There is no
   * sidebar logout entry.
   */
  readonly logoutButton: Locator;

  /** Dismisses the announcement modal first — it intercepts the click. */
  async open(): Promise<void> {
    await this.shell.dismissAnnouncementIfPresent();
    await this.shell.openMobileSidebarIfPresent();
    await this.shell.openProfileButton().click();
  }

  async fillProfile(details: { firstName: string; lastName: string; theme: string }): Promise<void> {
    await this.firstNameInput.fill(details.firstName);
    await this.lastNameInput.fill(details.lastName);
    await this.themeSelect.selectOption(details.theme);
  }

  /**
   * Chooses a theme mode on its own, independent of the rest of the profile
   * form. `onThemeModeChange()` (profile-settings-modal.component.ts) applies
   * and persists the mode as soon as the `<select>` fires `change` — it does
   * not wait for "Save Changes", which only covers name/email.
   */
  async selectTheme(mode: 'light' | 'dark' | 'system'): Promise<void> {
    await this.themeSelect.selectOption(mode);
  }

  /**
   * Gives the account a short, fixed first/last name and saves it, so the
   * identity card's name line renders "<firstName> <lastName>" instead of
   * falling back to `displayName()`'s next choice: the account's full
   * `username` (`e2e-<uuid>`, ~40 chars — see newTestUserDetails() in
   * e2e/fixtures/api.ts), which a fresh e2e user has no first/last name to
   * pre-empt. That fallback sits right at the mobile viewport's wrap
   * boundary, so whether it renders on one line or two comes down to the
   * exact glyph widths of that run's random UUID — a real source of visual-
   * diff flakiness, but not a timing race (the wrap is decided at first
   * layout, so no wait fixes it). Visual specs that screenshot the Profile
   * tab should call this before capturing.
   */
  async pinIdentityToShortName(): Promise<void> {
    await this.firstNameInput.fill('Visual');
    await this.lastNameInput.fill('Test');
    await this.saveChanges();
    await this.profileSavedMessage.waitFor({ state: 'visible' });
  }

  async saveChanges(): Promise<void> {
    await this.page.getByRole('button', { name: 'Save Changes' }).click();
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await this.page.getByPlaceholder('Current Password').fill(currentPassword);
    await this.page.getByPlaceholder('New Password', { exact: true }).fill(newPassword);
    await this.page.getByPlaceholder('Confirm New Password').fill(newPassword);
    await this.page.getByRole('button', { name: 'Update Password' }).click();
  }

  /** Closes the modal and ends the session; callers assert on the redirect. */
  async logout(): Promise<void> {
    await this.logoutButton.click();
  }

}

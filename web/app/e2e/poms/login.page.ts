import type { Locator, Page } from '@playwright/test';

export class LoginPage {
  constructor(private readonly page: Page) {
    this.identifierInput = this.page.getByPlaceholder('Enter your username or email');
    this.passwordInput = this.page.getByPlaceholder('Enter your password');
    this.showPasswordButton = this.page.getByRole('button', { name: 'Show password' });
    this.hidePasswordButton = this.page.getByRole('button', { name: 'Hide password' });
    this.submitButton = this.page.getByRole('button', { name: 'Sign in' });
    this.errorAlert = this.page.getByRole('alert');
  }

  readonly identifierInput: Locator;

  readonly passwordInput: Locator;

  /** Visible while the password field is masked. */
  readonly showPasswordButton: Locator;

  /** Visible while the password field is revealed. */
  readonly hidePasswordButton: Locator;

  readonly submitButton: Locator;

  readonly errorAlert: Locator;

  async navigateTo(): Promise<void> {
    await this.page.goto('/auth/login');
  }

  async fillCredentials(credentials: { email: string; password: string }): Promise<void> {
    await this.identifierInput.fill(credentials.email);
    await this.passwordInput.fill(credentials.password);
  }

  async submit(): Promise<void> {
    await this.submitButton.click();
  }

  /** goto + fill + submit; callers assert on the outcome. */
  async signIn(credentials: { email: string; password: string }): Promise<void> {
    await this.navigateTo();
    await this.fillCredentials(credentials);
    await this.submit();
  }

  async showPassword(): Promise<void> {
    await this.showPasswordButton.click();
  }

  async hidePassword(): Promise<void> {
    await this.hidePasswordButton.click();
  }
}

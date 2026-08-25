import type { Page } from '@playwright/test';

export interface RegistrationDetails {
  username: string;
  email: string;
  password: string;
}

export class RegisterPage {
  constructor(private readonly page: Page) {}

  async navigateTo(): Promise<void> {
    await this.page.goto('/auth/register');
  }

  async fillCredentials(details: RegistrationDetails): Promise<void> {
    await this.page.getByPlaceholder('Choose a username').fill(details.username);
    await this.page.getByPlaceholder('you@example.com').fill(details.email);
    await this.page.getByPlaceholder('Create a password').fill(details.password);
    await this.page.getByPlaceholder('Re-enter your password').fill(details.password);
  }

  async submit(): Promise<void> {
    await this.page.getByRole('button', { name: 'Sign up' }).click();
  }
}

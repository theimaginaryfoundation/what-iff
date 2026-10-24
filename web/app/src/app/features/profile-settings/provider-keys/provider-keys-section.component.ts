import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ModelProvider, PROVIDERS, ProviderInfo, ProviderKeySummary } from '../../../core/models/provider-key.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { ProviderKeysService } from '../../../core/services/provider-keys.service';
import { apiErrorMessage, hasErrorCode } from '../../../core/utils/api-error.helpers';
import { ProfileSettingsModalService } from '../profile-settings-modal.service';

interface ProviderRow {
  info: ProviderInfo;
  key: ProviderKeySummary | null;
}

/**
 * The API keys tab of Profile & Settings: every provider, whether this account
 * has a key for it, and an inline form to add or replace one. A key is
 * write-only, so a stored key shows only its last four characters.
 *
 * Self-contained so any build's settings modal can place it in a tab.
 */
@Component({
  selector: 'app-provider-keys-section',
  standalone: true,
  imports: [FormsModule],
  templateUrl: './provider-keys-section.component.html',
  styleUrl: './provider-keys-section.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProviderKeysSectionComponent implements OnInit {
  private readonly keys = inject(ProviderKeysService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly personalities = inject(PersonalityService);
  private readonly modal = inject(ProfileSettingsModalService);
  private readonly router = inject(Router);

  readonly loading = signal(true);
  readonly loadError = signal('');
  readonly stored = signal<ProviderKeySummary[]>([]);

  /** The provider whose form is open, if any. */
  readonly editing = signal<ModelProvider | null>(null);
  readonly keyInput = signal('');
  readonly passwordInput = signal('');
  readonly saving = signal(false);
  readonly formError = signal('');
  readonly notice = signal('');

  /** True while the account has no personality: saving a key points onward to creating one. */
  readonly onboarding = signal(false);

  readonly rows = computed<ProviderRow[]>(() => {
    const byProvider = new Map(this.stored().map(k => [k.provider, k]));
    return PROVIDERS.map(info => ({ info, key: byProvider.get(info.id) ?? null }));
  });

  /** Every provider marked required in PROVIDERS has a stored key. */
  readonly hasRequiredKey = computed(() => {
    const stored = new Set(this.stored().map(k => k.provider));
    return PROVIDERS.filter(p => p.required).every(p => stored.has(p.id));
  });

  async ngOnInit(): Promise<void> {
    await Promise.all([this.load(), this.checkOnboarding()]);
  }

  async load(): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    try {
      this.stored.set(await firstValueFrom(this.keys.list()));
    } catch (err) {
      this.loadError.set(apiErrorMessage(err, 'Could not load your provider keys.'));
    } finally {
      this.loading.set(false);
    }
  }

  private async checkOnboarding(): Promise<void> {
    try {
      const page = await firstValueFrom(this.personalities.listPersonalities(1, 1));
      this.onboarding.set((page.total_count ?? page.results?.length ?? 0) === 0);
    } catch {
      this.onboarding.set(false);
    }
  }

  open(provider: ModelProvider): void {
    this.editing.set(provider);
    this.keyInput.set('');
    this.passwordInput.set('');
    this.formError.set('');
    this.notice.set('');
  }

  cancel(): void {
    this.editing.set(null);
    this.formError.set('');
  }

  async save(info: ProviderInfo): Promise<void> {
    const key = this.keyInput().trim();
    if (!key) {
      this.formError.set(`Paste your ${info.name} key.`);
      return;
    }
    if (!this.passwordInput()) {
      this.formError.set('Enter your password to confirm.');
      return;
    }
    this.saving.set(true);
    this.formError.set('');
    try {
      await firstValueFrom(this.keys.set(info.id, key, this.passwordInput()));
      this.editing.set(null);
      this.notice.set(`${info.name} key saved.`);
      await Promise.all([this.load(), firstValueFrom(this.keys.refreshAccess())]);
    } catch (err) {
      this.formError.set(this.saveError(err));
    } finally {
      this.passwordInput.set('');
      this.saving.set(false);
    }
  }

  private saveError(err: unknown): string {
    if (hasErrorCode(err, 'reauthentication_required')) {
      return "That password didn't match. Try again.";
    }
    if (err instanceof HttpErrorResponse && err.status === 400) {
      return apiErrorMessage(err, "That key doesn't look right.");
    }
    return apiErrorMessage(err, "Couldn't save the key. Try again.");
  }

  async remove(info: ProviderInfo): Promise<void> {
    const confirmed = await this.confirmation.confirm({
      title: `Remove your ${info.name} key?`,
      message: info.required
        ? 'WhatIff needs an OpenAI key for memory, summaries and images. Chats stop working until you add one again.'
        : `${info.name} models stop working until you add a key again.`,
      confirmText: 'Remove key',
      type: 'danger',
    });
    if (!confirmed) {
      return;
    }
    try {
      await firstValueFrom(this.keys.remove(info.id));
      this.notice.set(`${info.name} key removed.`);
      await Promise.all([this.load(), firstValueFrom(this.keys.refreshAccess())]);
    } catch (err) {
      this.notice.set('');
      this.loadError.set(apiErrorMessage(err, "Couldn't remove the key. Try again."));
    }
  }

  /** Onboarding's next step: leave settings for the personality welcome. */
  goToPersonalities(): void {
    this.modal.close();
    void this.router.navigate(['/personality']);
  }

  formatDate(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
  }
}

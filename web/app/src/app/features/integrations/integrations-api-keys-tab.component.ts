import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';

import { ProviderKeyStatus } from '../../core/models/provider-key.model';
import { vendorLabel } from '../../core/utils/provider-vendor';
import { ProviderKeyService } from '../../core/services/provider-key.service';

/**
 * Where each provider issues keys. Every catalog provider takes a per-account
 * key now, so every one of them needs somewhere to send a user who does not
 * have one yet.
 */
const KEY_HELP_URLS: Record<string, string> = {
  openai: 'https://platform.openai.com/api-keys',
  anthropic: 'https://console.anthropic.com/settings/keys',
  google: 'https://aistudio.google.com/apikey',
  zai: 'https://z.ai/manage-apikey/apikey-list',
};

@Component({
  selector: 'app-integrations-api-keys-tab',
  standalone: true,
  imports: [CommonModule, FormsModule, RouterLink],
  templateUrl: './integrations-api-keys-tab.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class IntegrationsApiKeysTabComponent implements OnInit {
  private providerKeys = inject(ProviderKeyService);

  statuses = signal<ProviderKeyStatus[]>([]);
  isLoading = signal(false);
  /** Provider currently being saved or removed, so only its row shows a spinner. */
  busyProvider = signal<string | null>(null);
  errorMessage = signal('');
  successMessage = signal('');

  /** Draft key input per provider; cleared as soon as a save succeeds. */
  draftKeys = signal<Record<string, string>>({});

  /**
   * True when a provider the app cannot work without has no usable key. Drives
   * the banner, and it is why the setup guard sent the user here.
   */
  missingRequired = computed(() => this.statuses().some(s => s.required && !s.configured));

  ngOnInit(): void {
    this.load();
  }

  label(provider: string): string {
    return vendorLabel(provider);
  }

  helpUrl(provider: string): string | null {
    return KEY_HELP_URLS[provider] ?? null;
  }

  draftFor(provider: string): string {
    return this.draftKeys()[provider] ?? '';
  }

  onDraftChange(provider: string, value: string): void {
    this.draftKeys.set({ ...this.draftKeys(), [provider]: value });
  }

  load(): void {
    this.isLoading.set(true);
    this.errorMessage.set('');
    this.providerKeys.listStatuses().subscribe({
      next: statuses => {
        this.statuses.set(statuses);
        this.isLoading.set(false);
      },
      error: err => {
        this.errorMessage.set(this.readError(err, 'Could not load provider keys.'));
        this.isLoading.set(false);
      },
    });
  }

  save(provider: string): void {
    const key = this.draftFor(provider).trim();
    if (!key) {
      this.errorMessage.set('Enter a key first.');
      return;
    }
    this.busyProvider.set(provider);
    this.errorMessage.set('');
    this.successMessage.set('');
    this.providerKeys.setKey(provider, key).subscribe({
      next: status => {
        this.applyStatus(status);
        // Drop the plaintext from memory as soon as it is stored.
        this.onDraftChange(provider, '');
        this.successMessage.set(`${this.label(provider)} key saved.`);
        this.busyProvider.set(null);
      },
      error: err => {
        this.errorMessage.set(this.readError(err, 'Could not save that key.'));
        this.busyProvider.set(null);
      },
    });
  }

  remove(provider: string): void {
    this.busyProvider.set(provider);
    this.errorMessage.set('');
    this.successMessage.set('');
    this.providerKeys.removeKey(provider).subscribe({
      next: status => {
        this.applyStatus(status);
        this.successMessage.set(`${this.label(provider)} key removed.`);
        this.busyProvider.set(null);
      },
      error: err => {
        this.errorMessage.set(this.readError(err, 'Could not remove that key.'));
        this.busyProvider.set(null);
      },
    });
  }

  private applyStatus(updated: ProviderKeyStatus): void {
    this.statuses.set(this.statuses().map(s => (s.provider === updated.provider ? updated : s)));
  }

  /**
   * Error bodies vary by failure mode — an API error object, a network failure,
   * a non-JSON gateway page — so read defensively rather than assuming shape.
   */
  private readError(err: unknown, fallback: string): string {
    const body = (err as { error?: unknown })?.error;
    if (typeof body === 'string' && body.trim()) {
      return body;
    }
    if (body && typeof body === 'object') {
      const message = (body as { error?: string; message?: string }).error ?? (body as { message?: string }).message;
      if (message) {
        return message;
      }
    }
    return fallback;
  }
}

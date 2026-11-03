import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ProviderConnected, ProviderConnection, ProviderPreset } from '../../../core/models/provider-connection.model';
import { PROVIDERS } from '../../../core/models/provider-key.model';
import { AuthService } from '../../../core/services/auth.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { ProviderKeysService } from '../../../core/services/provider-keys.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { ProfileSettingsModalService } from '../profile-settings-modal.service';
import { ProviderFormComponent } from './provider-form.component';

interface ProviderRow {
  connection: ProviderConnection;
  /** What the connection is for, or where it points. */
  detail: string;
  required: boolean;
  local: boolean;
}

/** Remembers, per account in this browser, that the "saved keys are now connections" notice was dismissed. */
const CONVERTED_NOTICE_KEY = 'whatiff.providers.convertedNoticeDismissed';

/**
 * The Providers tab of Profile & Settings: the connections this account
 * reaches model providers through (ADR 0x027), and one form to add or edit
 * them. A key is write-only, so a stored key shows only its last four
 * characters.
 *
 * Self-contained so any build's settings modal can place it in a tab.
 */
@Component({
  selector: 'app-providers-section',
  standalone: true,
  imports: [ProviderFormComponent],
  templateUrl: './providers-section.component.html',
  styleUrl: './providers-section.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProvidersSectionComponent implements OnInit {
  private readonly connectionsService = inject(ProviderConnectionsService);
  private readonly keys = inject(ProviderKeysService);
  private readonly auth = inject(AuthService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly personalities = inject(PersonalityService);
  private readonly modal = inject(ProfileSettingsModalService);
  private readonly router = inject(Router);

  readonly loading = signal(true);
  readonly loadError = signal('');
  readonly connections = signal<ProviderConnection[]>([]);
  readonly presets = signal<ProviderPreset[]>([]);
  readonly notice = signal('');

  /** The form: closed, adding (optionally on a given provider), or editing a connection. */
  readonly formOpen = signal(false);
  readonly formConnection = signal<ProviderConnection | null>(null);
  readonly formPreset = signal<string | null>(null);

  /** True while the account has no personality: connecting OpenAI points onward to creating one. */
  readonly onboarding = signal(false);

  private readonly noticeDismissed = signal(false);

  readonly rows = computed<ProviderRow[]>(() =>
    this.connections().map(connection => {
      const preset = this.presets().find(p => p.id === connection.preset);
      const info = PROVIDERS.find(p => p.id === connection.preset);
      return {
        connection,
        detail: info?.usedFor ?? hostOf(connection.base_url || preset?.base_url || ''),
        required: info?.required ?? false,
        local: preset?.local ?? false,
      };
    }),
  );

  /** WhatIff needs an OpenAI connection for memory, summaries and images. */
  readonly needsOpenAI = computed(() => !this.loading() && !this.connections().some(c => c.preset === 'openai'));

  /** Shown once to an account whose saved keys were converted into connections. */
  readonly showConvertedNotice = computed(
    () => !this.noticeDismissed() && this.connections().some(c => c.source === 'converted'),
  );

  async ngOnInit(): Promise<void> {
    this.noticeDismissed.set(this.readDismissed());
    await Promise.all([this.load(), this.checkOnboarding()]);
  }

  async load(): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    try {
      const [connections, presets] = await Promise.all([
        firstValueFrom(this.connectionsService.list()),
        firstValueFrom(this.connectionsService.presets()),
      ]);
      this.connections.set(connections);
      this.presets.set(presets);
    } catch (err) {
      this.loadError.set(apiErrorMessage(err, 'Could not load your providers.'));
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

  add(preset: string | null = null): void {
    this.formConnection.set(null);
    this.formPreset.set(preset);
    this.notice.set('');
    this.formOpen.set(true);
  }

  edit(connection: ProviderConnection): void {
    this.formConnection.set(connection);
    this.formPreset.set(null);
    this.notice.set('');
    this.formOpen.set(true);
  }

  closeForm(): void {
    this.formOpen.set(false);
  }

  /** A connection was added or changed: show it, and refresh what the rest of the app knows. */
  async onSaved(connected: ProviderConnected): Promise<void> {
    this.notice.set(`${connected.connection.name} connected.`);
    await Promise.all([this.load(), firstValueFrom(this.keys.refreshAccess())]);
  }

  async remove(row: ProviderRow): Promise<void> {
    const { connection } = row;
    const confirmed = await this.confirmation.confirm({
      title: `Remove ${connection.name}?`,
      message: row.required
        ? 'WhatIff needs an OpenAI key for memory, summaries and images. Chats stop working until you connect it again.'
        : connection.builtin
          ? `${connection.name} models stop working until you connect it again.`
          : `WhatIff stops using ${connection.name}.`,
      confirmText: 'Remove',
      type: 'danger',
    });
    if (!confirmed) {
      return;
    }
    try {
      await firstValueFrom(this.connectionsService.remove(connection.id));
      this.notice.set(`${connection.name} removed.`);
      await Promise.all([this.load(), firstValueFrom(this.keys.refreshAccess())]);
    } catch (err) {
      this.notice.set('');
      this.loadError.set(apiErrorMessage(err, "Couldn't remove it. Try again."));
    }
  }

  dismissConvertedNotice(): void {
    this.noticeDismissed.set(true);
    const key = this.dismissedKey();
    if (!key) {
      return;
    }
    try {
      localStorage.setItem(key, '1');
    } catch {
      // Storage can be unavailable (private windows); the notice just shows again next time.
    }
  }

  private readDismissed(): boolean {
    const key = this.dismissedKey();
    if (!key) {
      return false;
    }
    try {
      return localStorage.getItem(key) === '1';
    } catch {
      return false;
    }
  }

  /**
   * The storage key for this account, or null before the account is known: a
   * shared fallback would let one account's dismissal hide the notice from
   * another in the same browser. Best effort by design; nothing depends on it.
   */
  private dismissedKey(): string | null {
    const id = this.auth.currentUser()?.id;
    return id ? `${CONVERTED_NOTICE_KEY}.${id}` : null;
  }

  /** Onboarding's next step: leave settings for the personality welcome. */
  goToPersonalities(): void {
    this.modal.close();
    void this.router.navigate(['/personality']);
  }
}

/** The host of an address, for a short description; the address itself when it does not parse. */
function hostOf(address: string): string {
  try {
    return new URL(address).host;
  } catch {
    return address;
  }
}

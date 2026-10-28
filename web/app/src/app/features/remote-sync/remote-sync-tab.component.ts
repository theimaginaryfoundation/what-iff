import { ChangeDetectionStrategy, Component, DestroyRef, OnInit, computed, inject, signal } from '@angular/core';
import { DatePipe, DecimalPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { RemoteSyncService, RemoteSyncSettings, runSummary } from './remote-sync.service';

/** Where people create the token: the hosted app's Tools page. */
export const HOSTED_TOOLS_URL = 'https://app.whatiff.chat/integrations';

/** The mullet mode guide (docs/guides/mullet-mode.md). */
export const MULLET_MODE_GUIDE_URL = 'https://github.com/theimaginaryfoundation/what-iff/blob/main/docs/guides/mullet-mode.md';

/** How often the tab re-reads the status while a run is going. */
const POLL_MS = 2000;

type Tone = 'success' | 'warning' | 'danger' | 'neutral' | 'info';

/**
 * Integrations → Remote sync: paste a token from another What Iff server (usually the hosted
 * app) and this server keeps a copy of your threads from it. Talks to /api/remote-sync
 * (internal/remotesyncplugin). See docs/adr/0x026-remote-sync.md.
 */
@Component({
  selector: 'app-remote-sync-tab',
  standalone: true,
  imports: [DatePipe, DecimalPipe, FormsModule, HelpHintComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="space-y-6" data-testid="remote-sync-tab">
      <div>
        <h2 class="inline-flex items-center gap-1 text-lg font-semibold text-gray-900 dark:text-white">
          Mullet mode
          <span class="text-sm font-normal text-gray-500 dark:text-gray-400">remote sync</span>
          <ui-help-hint label="What does mullet mode copy?" heading="What comes over">
            Every thread's messages: what you said and what the persona said, with their times. Persona prompts, memories and attachments
            are not part of the copy; import an account export from the other server once to bring those. A thread's persona is matched here
            by name.
          </ui-help-hint>
        </h2>
        <p class="mt-1 max-w-3xl text-sm text-gray-600 dark:text-gray-400">
          Business up front, party in the back: talk on whatiff.chat (or another What Iff server) while you are out, and keep a copy of
          every thread on this server. When you open this app, it fetches what is new, ready to continue here with your own models.
          <a class="text-indigo-600 underline dark:text-indigo-400" [href]="guideUrl" target="_blank" rel="noopener">Read the guide</a>
        </p>
        <p class="mt-2 max-w-3xl text-sm text-gray-600 dark:text-gray-400" data-testid="remote-sync-fork-note">
          Nothing here is ever changed or deleted by sync, and nothing is sent back. If you continue the same thread in both places, the two
          continuations become two threads: yours here stays as it is, and the other server's continues in a copy marked
          <strong>(from …)</strong>.
        </p>
      </div>

      @if (error()) {
        <div
          class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-800/60 dark:bg-red-900/20 dark:text-red-300"
          role="alert"
          data-testid="remote-sync-error"
        >
          {{ error() }}
        </div>
      }
      @if (notice()) {
        <div
          class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm text-green-800 dark:border-green-800/60 dark:bg-green-900/20 dark:text-green-300"
          role="status"
        >
          {{ notice() }}
        </div>
      }

      @if (loading()) {
        <div class="text-sm text-gray-600 dark:text-gray-400">Loading…</div>
      } @else {
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <!-- Status -->
          <section
            class="rounded-lg border border-gray-200 bg-white p-4 lg:col-span-2 dark:border-gray-700 dark:bg-gray-800"
            data-testid="remote-sync-status"
          >
            @if (!settings()?.configured) {
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Set it up</h3>
              <ol class="mt-3 list-decimal space-y-2 pl-5 text-sm text-gray-700 dark:text-gray-300">
                <li>
                  On the other server, open <strong>Tools → Webhooks</strong> (<a
                    class="text-indigo-600 underline dark:text-indigo-400"
                    [href]="hostedToolsUrl"
                    target="_blank"
                    rel="noopener"
                    >whatiff.chat</a
                  >).
                </li>
                <li>Create a token with <strong>Read threads</strong> ticked. Name it after this server.</li>
                <li>Paste it here and save. The first sync starts straight away.</li>
              </ol>
            } @else {
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div class="flex flex-wrap items-center gap-2">
                  <span
                    class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium"
                    [class]="toneClass(statusTone())"
                    data-testid="remote-sync-status-badge"
                  >
                    {{ statusLabel() }}
                  </span>
                  <span class="text-sm text-gray-700 dark:text-gray-300">{{ settings()!.base_url }}</span>
                </div>
                <button
                  class="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
                  [disabled]="busy() || settings()!.running || !settings()!.enabled"
                  (click)="runNow()"
                  data-testid="remote-sync-run-now"
                >
                  {{ settings()!.running ? 'Syncing…' : 'Sync now' }}
                </button>
              </div>

              @if (settings()!.last_error) {
                <p class="mt-3 text-sm text-red-700 dark:text-red-300" data-testid="remote-sync-last-error">{{ settings()!.last_error }}</p>
              }

              <dl class="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400">Threads</dt>
                  <dd class="font-medium text-gray-900 dark:text-white" data-testid="remote-sync-threads">
                    {{ settings()!.totals?.threads ?? 0 | number }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400">Messages</dt>
                  <dd class="font-medium text-gray-900 dark:text-white">{{ settings()!.totals?.messages ?? 0 | number }}</dd>
                </div>
                <div>
                  <dt class="text-xs text-gray-500 dark:text-gray-400">Forks</dt>
                  <dd class="font-medium text-gray-900 dark:text-white">{{ settings()!.totals?.forks ?? 0 | number }}</dd>
                </div>
              </dl>

              <div class="mt-4 space-y-1 text-xs text-gray-500 dark:text-gray-400">
                @if (settings()!.last_success_at) {
                  <p>Last synced {{ settings()!.last_success_at | date: 'medium' }}.</p>
                } @else if (!settings()!.running) {
                  <p>Not synced yet.</p>
                }
                @if (lastRunSummary()) {
                  <p data-testid="remote-sync-last-run">Last run: {{ lastRunSummary() }}.</p>
                }
                @if (settings()!.enabled) {
                  <p data-testid="remote-sync-cadence">
                    Syncs when you open this app or come back to it{{
                      settings()!.interval_seconds ? ', and every ' + intervalLabel() + ' in the background' : ''
                    }}.
                  </p>
                }
              </div>
            }
          </section>

          <!-- Settings -->
          <section class="h-fit rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Connection</h3>
            <div class="mt-3 space-y-3">
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300" for="remote-sync-url">Server</label>
                <input
                  id="remote-sync-url"
                  type="text"
                  class="w-full rounded-lg border border-gray-300 bg-gray-50 px-3 py-2 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white"
                  [placeholder]="defaultBaseUrl()"
                  [ngModel]="baseUrl()"
                  (ngModelChange)="baseUrl.set($event)"
                  data-testid="remote-sync-url"
                />
              </div>
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300" for="remote-sync-token">Webhook token</label>
                <input
                  id="remote-sync-token"
                  type="password"
                  autocomplete="off"
                  class="w-full rounded-lg border border-gray-300 bg-gray-50 px-3 py-2 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white"
                  [placeholder]="tokenPlaceholder()"
                  [ngModel]="token()"
                  (ngModelChange)="token.set($event)"
                  data-testid="remote-sync-token"
                />
              </div>
              <label class="flex items-start gap-2 text-sm text-gray-900 dark:text-white">
                <input
                  type="checkbox"
                  class="mt-0.5 rounded border-gray-300 dark:border-gray-600"
                  [ngModel]="enabled()"
                  (ngModelChange)="enabled.set($event)"
                  data-testid="remote-sync-enabled"
                />
                <span>Keep this server in sync</span>
              </label>
              <button
                class="w-full rounded-lg bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
                [disabled]="!canSave()"
                (click)="save()"
                data-testid="remote-sync-save"
              >
                {{ busy() ? 'Checking…' : 'Save' }}
              </button>
              @if (settings()?.configured) {
                <button
                  class="w-full rounded-lg border border-gray-300 px-3 py-2 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                  [disabled]="busy()"
                  (click)="remove()"
                  data-testid="remote-sync-remove"
                >
                  Stop syncing and forget the token
                </button>
              }
            </div>
          </section>
        </div>
      }
    </div>
  `,
})
export class RemoteSyncTabComponent implements OnInit {
  private readonly api = inject(RemoteSyncService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly destroyRef = inject(DestroyRef);

  readonly hostedToolsUrl = HOSTED_TOOLS_URL;
  readonly guideUrl = MULLET_MODE_GUIDE_URL;

  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  readonly notice = signal<string | null>(null);
  readonly settings = signal<RemoteSyncSettings | null>(null);

  readonly baseUrl = signal('');
  readonly token = signal('');
  readonly enabled = signal(true);

  /** How often to re-read the status while a run is going (tests shorten it). */
  pollMs = POLL_MS;
  private pollTimer: ReturnType<typeof setTimeout> | null = null;

  readonly defaultBaseUrl = computed(() => this.settings()?.default_base_url || 'https://api.whatiff.chat');
  readonly tokenPlaceholder = computed(() => {
    const hint = this.settings()?.token_hint;
    return hint ? `Saved (…${hint}); paste to replace` : 'wht_…';
  });
  readonly canSave = computed(() => !this.busy() && (!!this.settings()?.configured || this.token().trim() !== ''));
  readonly lastRunSummary = computed(() => runSummary(this.settings()?.last_run));
  readonly intervalLabel = computed(() => {
    const s = this.settings()?.interval_seconds ?? 0;
    const unit = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`;
    if (s >= 3600 && s % 3600 === 0) return s === 3600 ? 'hour' : unit(s / 3600, 'hour');
    if (s % 60 === 0) return s === 60 ? 'minute' : unit(s / 60, 'minute');
    return unit(s, 'second');
  });

  readonly statusTone = computed<Tone>(() => {
    const s = this.settings();
    if (!s?.configured) return 'neutral';
    if (s.running) return 'info';
    if (!s.enabled) return 'neutral';
    switch (s.status) {
      case 'ok':
        return 'success';
      case 'needs_attention':
        return 'danger';
      case 'error':
        return 'warning';
      default:
        return 'neutral';
    }
  });

  readonly statusLabel = computed(() => {
    const s = this.settings();
    if (!s?.configured) return 'Not set up';
    if (s.running) return 'Syncing…';
    if (!s.enabled) return 'Off';
    switch (s.status) {
      case 'ok':
        return 'In sync';
      case 'needs_attention':
        return 'Needs attention';
      case 'error':
        return 'Will retry';
      default:
        return 'Waiting for the first sync';
    }
  });

  constructor() {
    this.destroyRef.onDestroy(() => this.stopPolling());
  }

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    try {
      const s = await firstValueFrom(this.api.get());
      this.apply(s, true);
    } catch (e) {
      this.error.set(apiErrorMessage(e, 'Could not load the sync settings.'));
    } finally {
      this.loading.set(false);
    }
  }

  async save(): Promise<void> {
    if (!this.canSave()) return;
    this.busy.set(true);
    this.error.set(null);
    this.notice.set(null);
    const token = this.token().trim();
    try {
      const s = await firstValueFrom(
        this.api.save({ base_url: this.baseUrl().trim(), enabled: this.enabled(), ...(token ? { token } : {}) }),
      );
      this.token.set('');
      this.notice.set(s.enabled ? 'Saved. The token works; syncing now.' : 'Saved. Sync is off.');
      this.apply(s, true);
    } catch (e) {
      this.error.set(apiErrorMessage(e, 'Could not save the sync settings.'));
    } finally {
      this.busy.set(false);
    }
  }

  async runNow(): Promise<void> {
    this.busy.set(true);
    this.error.set(null);
    this.notice.set(null);
    try {
      this.apply(await firstValueFrom(this.api.runNow()), false);
      this.settings.update(s => (s ? { ...s, running: true } : s));
      this.schedulePoll();
    } catch (e) {
      this.error.set(apiErrorMessage(e, 'Could not start a sync.'));
    } finally {
      this.busy.set(false);
    }
  }

  async remove(): Promise<void> {
    const ok = await this.confirmation.confirm({
      title: 'Stop syncing?',
      message:
        'This server stops copying threads and forgets the token. Threads already copied stay here. Revoke the token on the other server too if you no longer need it.',
      type: 'danger',
      confirmText: 'Stop syncing',
      cancelText: 'Cancel',
    });
    if (!ok) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      await firstValueFrom(this.api.remove());
      this.stopPolling();
      this.settings.update(s => ({
        ...(s ?? { running: false }),
        configured: false,
        running: false,
        token_hint: undefined,
        totals: undefined,
      }));
      this.notice.set('Sync stopped. Copied threads are still here.');
    } catch (e) {
      this.error.set(apiErrorMessage(e, 'Could not stop syncing.'));
    } finally {
      this.busy.set(false);
    }
  }

  toneClass(tone: Tone): string {
    switch (tone) {
      case 'success':
        return 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300';
      case 'warning':
        return 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300';
      case 'danger':
        return 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300';
      case 'info':
        return 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300';
      default:
        return 'bg-gray-100 text-gray-700 dark:bg-gray-900/40 dark:text-gray-300';
    }
  }

  /** Takes a fresh settings read; resetForm also copies it into the form fields. */
  private apply(s: RemoteSyncSettings, resetForm: boolean): void {
    this.settings.set(s);
    if (resetForm) {
      this.baseUrl.set(s.base_url || s.default_base_url || '');
      this.enabled.set(s.configured ? s.enabled !== false : true);
    }
    if (s.running) this.schedulePoll();
  }

  private schedulePoll(): void {
    if (this.pollTimer) return;
    this.pollTimer = setTimeout(() => void this.poll(), this.pollMs);
  }

  private async poll(): Promise<void> {
    this.pollTimer = null;
    try {
      const s = await firstValueFrom(this.api.get());
      this.settings.set(s);
      if (s.running) this.schedulePoll();
    } catch {
      // A transient failure while polling is not worth an error banner; the next action re-reads.
    }
  }

  private stopPolling(): void {
    if (this.pollTimer) clearTimeout(this.pollTimer);
    this.pollTimer = null;
  }
}

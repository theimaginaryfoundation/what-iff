import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { Chat, isSandboxed } from '../../core/models/chat.model';
import { ChatService } from '../../core/services/chat.service';
import { needsRelayAcknowledgement } from '../discord/discord.service';
import { BUZZ_EMPTY_ALLOW_NOTE, BUZZ_UNRESTRICTED_WARNING, BuzzChannel, BuzzCommunity, BuzzService } from './buzz.service';
import { BuzzPubkeyChipsComponent } from './buzz-pubkey-chips.component';
import { BuzzTabState } from './buzz-tab.state';
import { BUZZ_UI } from './buzz-ui';

/**
 * A community's channels, to connect one to a relay thread: channels the persona is in can be
 * connected (to a new sandboxed thread, or an existing thread with the same acknowledgement as
 * Discord), public ones it is not in can be joined first, and private ones need a member to add it.
 */
@Component({
  selector: 'app-buzz-channels',
  standalone: true,
  imports: [FormsModule, BuzzPubkeyChipsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!open()) {
      <button
        type="button"
        class="text-sm font-medium text-indigo-600 hover:underline dark:text-indigo-400"
        (click)="toggle()"
        data-testid="buzz-channels-open"
      >
        + Connect a channel
      </button>
    } @else {
      <div data-testid="buzz-channels">
        <div class="flex items-center justify-between gap-2">
          <h5 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Channels</h5>
          <div class="flex gap-2">
            <button type="button" [class]="ui.secondary" [disabled]="loading()" (click)="load()">Refresh</button>
            <button type="button" [class]="ui.secondary" (click)="toggle()">Close</button>
          </div>
        </div>
        @if (loading()) {
          <p class="mt-2" [class]="ui.hint">Loading channels…</p>
        } @else if (channels().length === 0) {
          <p class="mt-2" [class]="ui.hint">No channels the persona can see yet.</p>
        }
        <ul class="mt-2 divide-y divide-gray-100 dark:divide-gray-700/60">
          @for (c of channels(); track c.id) {
            <li class="py-2" data-testid="buzz-channel">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="min-w-0">
                  <span class="text-sm font-medium text-gray-900 dark:text-white">#{{ c.name }}</span>
                  @if (!c.public) {
                    <span class="ml-1" [class]="ui.hint">private</span>
                  }
                  @if (c.archived) {
                    <span class="ml-1" [class]="ui.hint">archived</span>
                  }
                  @if (c.about) {
                    <p [class]="ui.hint">{{ c.about }}</p>
                  }
                </div>
                @if (bound(c)) {
                  <span [class]="ui.hint">Connected</span>
                } @else if (c.member) {
                  <button type="button" [class]="ui.primary" [disabled]="state.busy()" (click)="pick(c)" data-testid="buzz-channel-connect">
                    Connect
                  </button>
                } @else if (c.public) {
                  <button type="button" [class]="ui.secondary" [disabled]="state.busy()" (click)="join(c)" data-testid="buzz-channel-join">
                    Join
                  </button>
                } @else {
                  <span [class]="ui.hint">Ask a member to add the persona</span>
                }
              </div>
              @if (selected()?.id === c.id) {
                <div class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700" data-testid="buzz-connect-form">
                  <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
                    <label class="block text-xs text-gray-600 dark:text-gray-400">
                      Thread
                      <select
                        class="mt-1"
                        [class]="ui.input"
                        [ngModel]="chatId()"
                        (ngModelChange)="pickThread($event)"
                        aria-label="Thread"
                        data-testid="buzz-thread-select"
                      >
                        <option value="">A new relay thread (sandboxed, recommended)</option>
                        @for (t of threads(); track t.id) {
                          <option [value]="t.id">{{ t.name }}{{ unrestricted(t) ? ' (not sandboxed)' : ' (sandboxed)' }}</option>
                        }
                      </select>
                    </label>
                    <div class="block text-xs text-gray-600 dark:text-gray-400">
                      <span class="mb-1 block">Only these Buzz keys may mention it (optional)</span>
                      <app-buzz-pubkey-chips
                        label="Allowed Buzz public keys"
                        placeholder="Anyone, until you add a key…"
                        [keys]="allow()"
                        (keysChange)="allow.set($event)"
                      />
                    </div>
                  </div>
                  @if (!allow().length) {
                    <p class="text-xs font-medium text-amber-800 dark:text-amber-300" data-testid="buzz-connect-empty-allow-note">
                      No allow list: {{ emptyAllowNote }} They can be limited later under "Who can mention".
                    </p>
                  }
                  @if (selectedUnrestricted()) {
                    <div [class]="ui.warningBox" role="alert" data-testid="buzz-unrestricted-warning">
                      <p class="font-semibold">This thread is not sandboxed.</p>
                      <p class="mt-1">
                        {{ unrestrictedWarning }} Everything said in the channel is read and answered as you, and replies are posted where
                        other people can read them. Sandboxing the thread first is safer.
                      </p>
                      <label class="mt-2 flex items-start gap-2">
                        <input
                          type="checkbox"
                          class="mt-0.5"
                          [ngModel]="ack()"
                          (ngModelChange)="ack.set($event)"
                          data-testid="buzz-ack-unrestricted"
                        />
                        <span>I understand: {{ unrestrictedWarning }}</span>
                      </label>
                    </div>
                  }
                  <div class="flex gap-2">
                    <button
                      type="button"
                      [class]="ui.primary"
                      [disabled]="!canConnect() || state.busy()"
                      (click)="connect()"
                      data-testid="buzz-connect"
                    >
                      {{ chatId() ? 'Connect this thread' : 'Connect, with a new relay thread' }}
                    </button>
                    <button type="button" [class]="ui.secondary" (click)="selected.set(null)">Cancel</button>
                  </div>
                </div>
              }
            </li>
          }
        </ul>
      </div>
    }
  `,
})
export class BuzzChannelsComponent {
  private readonly buzz = inject(BuzzService);
  private readonly chats = inject(ChatService);
  readonly state = inject(BuzzTabState);

  readonly community = input.required<BuzzCommunity>();
  /** The identity's persona: existing threads are listed from it. */
  readonly personalityId = input.required<string>();

  readonly ui = BUZZ_UI;
  readonly unrestrictedWarning = BUZZ_UNRESTRICTED_WARNING;
  readonly emptyAllowNote = BUZZ_EMPTY_ALLOW_NOTE;
  readonly open = signal(false);
  readonly loading = signal(false);
  readonly channels = signal<BuzzChannel[]>([]);
  readonly threads = signal<Chat[]>([]);
  /** The channel being connected. */
  readonly selected = signal<BuzzChannel | null>(null);
  /** '' = a new relay thread (sandboxed); otherwise an existing thread's id. */
  readonly chatId = signal('');
  readonly ack = signal(false);
  readonly allow = signal<string[]>([]);

  readonly selectedUnrestricted = computed(() => {
    const t = this.threads().find(x => x.id === this.chatId());
    return !!t && this.unrestricted(t);
  });
  readonly canConnect = computed(() => !!this.selected() && (!this.selectedUnrestricted() || this.ack()));

  unrestricted(t: Chat): boolean {
    return needsRelayAcknowledgement(isSandboxed(t));
  }

  bound(c: BuzzChannel): boolean {
    return this.state.bindingsFor(this.community().id).some(b => b.channel_id === c.id);
  }

  toggle(): void {
    this.open.update(v => !v);
    this.selected.set(null);
    if (this.open()) void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.channels.set(await firstValueFrom(this.buzz.listChannels(this.community().id)));
    } catch (e) {
      this.state.fail(e, 'Could not list the community’s channels.');
    } finally {
      this.loading.set(false);
    }
  }

  async join(c: BuzzChannel): Promise<void> {
    const ok = await this.state.run(() => firstValueFrom(this.buzz.joinChannel(this.community().id, c.id)), `Could not join #${c.name}.`);
    if (ok) await this.load();
  }

  pick(c: BuzzChannel): void {
    this.selected.set(c);
    this.chatId.set('');
    this.ack.set(false);
    this.allow.set([]);
    if (this.threads().length === 0) {
      // Existing threads are an optional alternative; failing to list them leaves the (safe) new-thread default.
      this.chats
        .listChatsPage(1, 50, { personality_id: this.personalityId() })
        .subscribe({ next: page => this.threads.set(page.results ?? []), error: () => undefined });
    }
  }

  pickThread(chatId: string): void {
    // The acknowledgement is for one thread: choosing another starts it unticked.
    this.chatId.set(chatId);
    this.ack.set(false);
  }

  async connect(): Promise<void> {
    const channel = this.selected();
    if (!channel || !this.canConnect()) return;
    const chatId = this.chatId();
    const ok = await this.state.run(async () => {
      const binding = await firstValueFrom(
        this.buzz.addBinding({
          community_id: this.community().id,
          channel_id: channel.id,
          channel_name: channel.name,
          allow_pubkeys: this.allow(),
          ...(chatId ? { chat_id: chatId } : {}),
          ...(chatId && this.selectedUnrestricted() ? { allow_unrestricted: this.ack() } : {}),
        }),
      );
      this.state.putBinding(binding);
    }, 'Could not connect the channel.');
    if (ok) this.selected.set(null);
  }
}

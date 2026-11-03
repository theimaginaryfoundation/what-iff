import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { Chat, isSandboxed } from '../../core/models/chat.model';
import { ChatService } from '../../core/services/chat.service';
import { needsRelayAcknowledgement } from '../discord/discord.service';
import { RELAY_EMPTY_ALLOW_NOTE, RELAY_UNRESTRICTED_WARNING } from '../relay/relay-copy';
import { RelayIdChipsComponent } from '../relay/relay-id-chips.component';
import { RELAY_UI } from '../relay/relay-ui';
import { SLACK_USER_IDS, SlackApp, SlackChannel, SlackService } from './slack.service';
import { SlackTabState } from './slack-tab.state';

/**
 * A workspace's channels, to connect one to a relay thread: channels the app is in can be
 * connected (to a new sandboxed thread, or an existing thread with the same acknowledgement as
 * Discord and Buzz), public ones it is not in can be joined first, and private ones need the
 * persona invited in Slack.
 */
@Component({
  selector: 'app-slack-channels',
  standalone: true,
  imports: [FormsModule, RelayIdChipsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!open()) {
      <button
        type="button"
        class="text-sm font-medium text-indigo-600 hover:underline dark:text-indigo-400"
        (click)="toggle()"
        data-testid="slack-channels-open"
      >
        + Connect a channel
      </button>
    } @else {
      <div data-testid="slack-channels">
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
          <p class="mt-2" [class]="ui.hint">No channels yet.</p>
        }
        <ul class="mt-2 divide-y divide-gray-100 dark:divide-gray-700/60">
          @for (c of channels(); track c.id) {
            <li class="py-2" data-testid="slack-channel">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="min-w-0">
                  <span class="text-sm font-medium text-gray-900 dark:text-white">#{{ c.name }}</span>
                  @if (c.is_private) {
                    <span class="ml-1" [class]="ui.hint">private</span>
                  }
                  @if (c.num_members != null) {
                    <span class="ml-1" [class]="ui.hint">{{ c.num_members }} {{ c.num_members === 1 ? 'member' : 'members' }}</span>
                  }
                  @if (c.is_member) {
                    <span class="ml-1" [class]="ui.hint" data-testid="slack-channel-member">· &#64;{{ app().bot_name }} is in it</span>
                  }
                </div>
                @if (bound(c)) {
                  <span [class]="ui.hint">Connected</span>
                } @else if (c.is_member) {
                  <button
                    type="button"
                    [class]="ui.primary"
                    [disabled]="state.busy()"
                    (click)="pick(c)"
                    data-testid="slack-channel-connect"
                  >
                    Connect
                  </button>
                } @else if (!c.is_private) {
                  <button type="button" [class]="ui.secondary" [disabled]="state.busy()" (click)="join(c)" data-testid="slack-channel-join">
                    Join
                  </button>
                } @else {
                  <span [class]="ui.hint" data-testid="slack-channel-invite"
                    >Invite it in Slack: <code>/invite &#64;{{ app().bot_name }}</code></span
                  >
                }
              </div>
              @if (selected()?.id === c.id) {
                <div class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700" data-testid="slack-connect-form">
                  <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
                    <label class="block text-xs text-gray-600 dark:text-gray-400">
                      Thread
                      <select
                        class="mt-1"
                        [class]="ui.input"
                        [ngModel]="chatId()"
                        (ngModelChange)="pickThread($event)"
                        aria-label="Thread"
                        data-testid="slack-thread-select"
                      >
                        <option value="">A new relay thread (sandboxed, recommended)</option>
                        @for (t of threads(); track t.id) {
                          <option [value]="t.id">{{ t.name }}{{ unrestricted(t) ? ' (not sandboxed)' : ' (sandboxed)' }}</option>
                        }
                      </select>
                    </label>
                    <div class="block text-xs text-gray-600 dark:text-gray-400">
                      <span class="mb-1 block">Only these Slack members may mention it (optional)</span>
                      <app-relay-id-chips
                        [kind]="userIds"
                        label="Allowed Slack member ids"
                        placeholder="Anyone, until you add an id…"
                        [ids]="allow()"
                        (idsChange)="allow.set($event)"
                      />
                    </div>
                  </div>
                  @if (!allow().length) {
                    <p class="text-xs font-medium text-amber-800 dark:text-amber-300" data-testid="slack-connect-empty-allow-note">
                      No allow list: {{ emptyAllowNote }} They can be limited later under "Who can mention".
                    </p>
                  }
                  @if (selectedUnrestricted()) {
                    <div [class]="ui.warningBox" role="alert" data-testid="slack-unrestricted-warning">
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
                          data-testid="slack-ack-unrestricted"
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
                      data-testid="slack-connect-channel"
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
export class SlackChannelsComponent {
  private readonly slack = inject(SlackService);
  private readonly chats = inject(ChatService);
  readonly state = inject(SlackTabState);

  readonly app = input.required<SlackApp>();

  readonly ui = RELAY_UI;
  readonly userIds = SLACK_USER_IDS;
  readonly unrestrictedWarning = RELAY_UNRESTRICTED_WARNING;
  readonly emptyAllowNote = RELAY_EMPTY_ALLOW_NOTE;
  readonly open = signal(false);
  readonly loading = signal(false);
  readonly channels = signal<SlackChannel[]>([]);
  readonly threads = signal<Chat[]>([]);
  /** The channel being connected. */
  readonly selected = signal<SlackChannel | null>(null);
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

  bound(c: SlackChannel): boolean {
    return this.state.bindingsFor(this.app().id).some(b => b.channel_id === c.id);
  }

  toggle(): void {
    this.open.update(v => !v);
    this.selected.set(null);
    if (this.open()) void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.channels.set(await firstValueFrom(this.slack.listChannels(this.app().id)));
    } catch (e) {
      this.state.fail(e, 'Could not list the workspace’s channels.');
    } finally {
      this.loading.set(false);
    }
  }

  /** Adds the app to a public channel; the answer is the channel as it is now, so the list needs no reload. */
  async join(c: SlackChannel): Promise<void> {
    await this.state.run(async () => {
      const joined = await firstValueFrom(this.slack.joinChannel(this.app().id, c.id));
      this.channels.update(list => list.map(x => (x.id === c.id ? { ...x, ...joined, is_member: true } : x)));
    }, `Could not join #${c.name}.`);
  }

  pick(c: SlackChannel): void {
    this.selected.set(c);
    this.chatId.set('');
    this.ack.set(false);
    this.allow.set([]);
    if (this.threads().length === 0) {
      // Existing threads are an optional alternative; failing to list them leaves the (safe) new-thread default.
      this.chats
        .listChatsPage(1, 50, { personality_id: this.app().personality_id })
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
        this.slack.addBinding({
          app_id: this.app().id,
          channel_id: channel.id,
          channel_name: channel.name,
          is_private: channel.is_private,
          allow_user_ids: this.allow(),
          ...(chatId ? { chat_id: chatId } : {}),
          ...(chatId && this.selectedUnrestricted() ? { allow_unrestricted: this.ack() } : {}),
        }),
      );
      this.state.putBinding(binding);
    }, 'Could not connect the channel.');
    if (ok) this.selected.set(null);
  }
}

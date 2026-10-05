import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { HttpErrorResponse } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';

import { PersonalityService } from '../../core/services/personality.service';
import { Personality } from '../../core/models/personality.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ChatService } from '../../core/services/chat.service';
import { Chat } from '../../core/models/chat.model';
import {
  DiscordBinding,
  DiscordBot,
  DiscordChannel,
  DiscordGuild,
  DiscordService,
  EMPTY_ALLOW_NOTE,
  UNRESTRICTED_WARNING,
  bindingLabel,
  needsRelayAcknowledgement,
} from './discord.service';
import { DiscordIdChipsComponent } from './discord-id-chips.component';

/** Per-bot "connect a channel" form state. */
interface ConnectForm {
  guilds: DiscordGuild[];
  channels: DiscordChannel[];
  guildId: string;
  channelId: string;
  loading: boolean;
  /** The persona's existing threads, offered as an alternative to a new relay thread. */
  threads: Chat[];
  /** '' = a new relay thread (sandboxed); otherwise an existing thread's id. */
  chatId: string;
  /** Explicit confirmation, required when the chosen existing thread is not sandboxed. */
  ackUnrestricted: boolean;
  /** Optional Discord user ids that may tag the bot; empty = anyone in the channel. */
  allowIds: string[];
}

/** Per-binding editable access lists (Discord user ids). */
interface AccessDraft {
  allow: string[];
  deny: string[];
}

/**
 * Integrations → Discord: add your own Discord bot for a persona, invite it to a
 * server, and connect channels to relay threads. Talks to /api/discord
 * (internal/discordplugin).
 */
@Component({
  selector: 'app-discord-tab',
  standalone: true,
  imports: [DatePipe, FormsModule, RouterLink, DiscordIdChipsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="space-y-6" data-testid="discord-tab">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Discord</h2>
      </div>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400">
        Give a persona its own Discord bot. Tag it in a connected channel and it answers there; ask it in the app to post a reply and it
        does. Each connected channel has a relay thread here that keeps the conversation together.
      </p>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400" data-testid="discord-relay-defaults">
        A new relay thread is sandboxed: it reads only itself (none of your memories, other conversations, files or scratchpad), and what
        the persona learns there stays in that thread. The tools that act beyond the conversation (scheduled jobs, sub-agents, the
        scratchpad, web search, page fetching and image generation) start switched off. You can change both in the thread's settings. What
        the persona learns is saved as <strong>External</strong> memories, marked unverified; filter the memory list by External to review
        them.
      </p>

      @if (error()) {
        <div
          class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-800/60 dark:bg-red-900/20 dark:text-red-300"
          role="alert"
        >
          {{ error() }}
        </div>
      }

      <!-- Add a bot -->
      <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Add a bot</h3>
        <ol class="mt-3 list-decimal space-y-2 pl-5 text-sm text-gray-700 dark:text-gray-300">
          <li>
            Open the
            <a
              class="text-indigo-600 underline dark:text-indigo-400"
              href="https://discord.com/developers/applications"
              target="_blank"
              rel="noopener"
              >Discord developer portal</a
            >
            and create a <strong>New Application</strong>. Name it after your persona.
          </li>
          <li>On its <strong>Bot</strong> page, choose <strong>Reset Token</strong> and copy the token.</li>
          <li>
            Optional: on the same page, switch on <strong>Message Content Intent</strong>. It is not needed for tags and replies, only for
            future triggers.
          </li>
          <li>Choose the persona and paste the token below. We check it with Discord and store it encrypted.</li>
        </ol>
        <div class="mt-4 grid grid-cols-1 gap-3 md:grid-cols-3">
          <select
            class="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
            [ngModel]="newPersonaId()"
            (ngModelChange)="newPersonaId.set($event)"
            aria-label="Persona"
            data-testid="discord-persona-select"
          >
            <option value="">Choose a persona…</option>
            @for (p of availablePersonas(); track p.id) {
              <option [value]="p.id">{{ p.name }}</option>
            }
          </select>
          <input
            type="password"
            autocomplete="off"
            class="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
            placeholder="Bot token"
            [ngModel]="newToken()"
            (ngModelChange)="newToken.set($event)"
            aria-label="Bot token"
            data-testid="discord-token-input"
          />
          <button
            class="rounded-lg bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
            [disabled]="!newPersonaId() || !newToken().trim() || busy()"
            (click)="addBot()"
            data-testid="discord-add-bot"
          >
            Add bot
          </button>
        </div>
      </section>

      <!-- Bots -->
      @if (loading()) {
        <div class="text-sm text-gray-600 dark:text-gray-400">Loading…</div>
      } @else if (bots().length === 0) {
        <div class="text-sm text-gray-600 dark:text-gray-400">No bots yet.</div>
      }

      @for (bot of bots(); track bot.id) {
        <section class="rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800" data-testid="discord-bot">
          <div class="flex flex-wrap items-start justify-between gap-3 p-4">
            <div class="min-w-0">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ bot.bot_username || 'Discord bot' }}
                <span class="font-normal text-gray-500 dark:text-gray-400">as {{ personaName(bot.personality_id) }}</span>
              </h3>
              <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span class="inline-flex items-center rounded-full px-2 py-0.5 font-medium" [class]="statusClass(bot.status)">
                  {{ botStatusLabel(bot.status) }}
                </span>
                <span>Added {{ bot.created_at | date: 'mediumDate' }}</span>
              </div>
              @if (bot.last_error) {
                <p class="mt-1 text-xs text-red-700 dark:text-red-300">{{ bot.last_error }}</p>
              }
            </div>
            <div class="flex flex-wrap gap-2">
              <a
                class="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700"
                [href]="bot.invite_url"
                target="_blank"
                rel="noopener"
              >
                Invite to a server
              </a>
              <button
                class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                [disabled]="busy()"
                (click)="syncProfile(bot)"
              >
                Use persona's name and portrait
              </button>
              <button
                class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                [disabled]="busy()"
                (click)="replaceToken(bot)"
              >
                Replace token
              </button>
              <button
                class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
                [disabled]="busy()"
                (click)="togglePaused(bot)"
              >
                {{ bot.status === 'disabled' ? 'Resume' : 'Pause' }}
              </button>
              <button
                class="rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
                [disabled]="busy()"
                (click)="removeBot(bot)"
              >
                Remove
              </button>
            </div>
          </div>

          <!-- Channels -->
          <div class="border-t border-gray-200 dark:border-gray-700">
            @for (b of bindingsFor(bot.id); track b.id) {
              <div class="border-b border-gray-100 p-4 last:border-b-0 dark:border-gray-700/60" data-testid="discord-binding">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="text-sm font-medium text-gray-900 dark:text-white">{{ label(b) }}</div>
                    <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                      <span class="inline-flex items-center rounded-full px-2 py-0.5 font-medium" [class]="statusClass(b.status)">
                        {{ b.status === 'active' ? 'Connected' : 'Broken' }}
                      </span>
                      @if (b.chat_sandboxed === true) {
                        <span
                          class="inline-flex items-center rounded-full bg-green-100 px-2 py-0.5 font-medium text-green-800 dark:bg-green-900/30 dark:text-green-300"
                          data-testid="discord-binding-sandboxed"
                          >Sandboxed thread</span
                        >
                      } @else if (b.chat_sandboxed === false) {
                        <span
                          class="inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-900 dark:bg-amber-900/30 dark:text-amber-200"
                          data-testid="discord-binding-unsandboxed"
                        >
                          {{ b.allow_unrestricted ? 'Not sandboxed (acknowledged)' : 'Not sandboxed: paused' }}
                        </span>
                      }
                      @if (!b.allow_user_ids.length) {
                        <span
                          class="inline-flex items-center rounded-full bg-gray-100 px-2 py-0.5 font-medium text-gray-700 dark:bg-gray-900/40 dark:text-gray-300"
                          data-testid="discord-binding-open-to-anyone"
                          >Open to anyone in the channel</span
                        >
                      }
                      <a class="text-indigo-600 underline dark:text-indigo-400" [routerLink]="['/chat', b.chat_id]">Open relay thread</a>
                      @if (b.last_activity_at) {
                        <span>Last activity {{ b.last_activity_at | date: 'short' }}</span>
                      }
                    </div>
                    @if (b.last_error) {
                      <p class="mt-1 text-xs text-red-700 dark:text-red-300">{{ b.last_error }}</p>
                    }
                    @if (b.chat_sandboxed === false) {
                      <label class="mt-2 flex max-w-xl items-start gap-2 text-xs text-amber-900 dark:text-amber-200">
                        <input
                          type="checkbox"
                          class="mt-0.5"
                          [checked]="b.allow_unrestricted"
                          (change)="setAcknowledged(b, $any($event.target).checked)"
                          data-testid="discord-binding-ack"
                        />
                        <span>
                          This thread is not sandboxed. {{ unrestrictedWarning }}
                          Until this is ticked, tags here are not answered. Or sandbox the thread again.
                        </span>
                      </label>
                    }
                  </div>
                  <div class="flex flex-wrap items-center gap-2">
                    <label class="inline-flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
                      <input type="checkbox" [checked]="b.inbound_enabled" (change)="setInbound(b, $any($event.target).checked)" />
                      Answer when tagged
                    </label>
                    @if (b.status === 'broken') {
                      <button
                        class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium dark:border-gray-600 dark:text-gray-200"
                        (click)="reactivate(b)"
                      >
                        Try again
                      </button>
                    }
                    <button
                      class="rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
                      (click)="removeBinding(b)"
                    >
                      Disconnect
                    </button>
                  </div>
                </div>
                <details class="mt-3 text-xs text-gray-700 dark:text-gray-300">
                  <summary class="cursor-pointer select-none">Who can tag {{ bot.bot_username || 'the bot' }} here</summary>
                  <p class="mt-2 text-gray-500 dark:text-gray-400">
                    Discord user ids (right-click a user, Copy User ID, with Developer Mode on). Press Enter, comma or Space after each id,
                    or paste a whole list. An empty allow list means anyone in the channel, and deny always wins. Each answer is a turn on
                    your account, and it can draw on this persona's memories and the relay thread's tools, in a channel other people can
                    read.
                  </p>
                  @if (!draft(b).allow.length) {
                    <p class="mt-2 font-medium text-amber-800 dark:text-amber-300" data-testid="discord-empty-allow-note">
                      Allow list is empty: {{ emptyAllowNote }}
                    </p>
                  }
                  <div class="mt-2 grid grid-cols-1 gap-3 md:grid-cols-2">
                    <div>
                      <span class="mb-1 block text-gray-600 dark:text-gray-400">Allow</span>
                      <app-discord-id-chips
                        label="Allowed Discord user ids"
                        placeholder="Anyone, until you add an id…"
                        [ids]="draft(b).allow"
                        (idsChange)="setDraft(b, 'allow', $event)"
                      />
                    </div>
                    <div>
                      <span class="mb-1 block text-gray-600 dark:text-gray-400">Deny</span>
                      <app-discord-id-chips
                        label="Denied Discord user ids"
                        placeholder="Nobody blocked…"
                        [ids]="draft(b).deny"
                        (idsChange)="setDraft(b, 'deny', $event)"
                      />
                    </div>
                  </div>
                  <button
                    class="mt-2 rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700"
                    (click)="saveAccess(b)"
                  >
                    Save
                  </button>
                </details>
              </div>
            }

            <!-- Connect a channel -->
            <div class="p-4">
              @if (connectFor(bot.id); as form) {
                <div class="grid grid-cols-1 gap-2 md:grid-cols-3">
                  <select
                    class="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
                    [ngModel]="form.guildId"
                    (ngModelChange)="pickGuild(bot, $event)"
                    aria-label="Server"
                  >
                    <option value="">{{ form.loading && !form.guilds.length ? 'Loading servers…' : 'Choose a server…' }}</option>
                    @for (g of form.guilds; track g.id) {
                      <option [value]="g.id">{{ g.name }}</option>
                    }
                  </select>
                  <select
                    class="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
                    [ngModel]="form.channelId"
                    (ngModelChange)="pickChannel(bot, $event)"
                    [disabled]="!form.guildId"
                    aria-label="Channel"
                  >
                    <option value="">Choose a channel…</option>
                    @for (c of form.channels; track c.id) {
                      <option [value]="c.id">#{{ c.name }}</option>
                    }
                  </select>
                  <button
                    class="rounded-lg bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                    [disabled]="!canConnect(form) || busy()"
                    (click)="connect(bot)"
                    data-testid="discord-connect"
                  >
                    {{ form.chatId ? 'Connect this thread' : 'Connect, with a new relay thread' }}
                  </button>
                </div>
                <div class="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
                  <label class="block text-xs text-gray-600 dark:text-gray-400">
                    Thread
                    <select
                      class="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
                      [ngModel]="form.chatId"
                      (ngModelChange)="pickThread(bot, $event)"
                      aria-label="Thread"
                      data-testid="discord-thread-select"
                    >
                      <option value="">A new relay thread (sandboxed, recommended)</option>
                      @for (t of form.threads; track t.id) {
                        <option [value]="t.id">{{ t.name }}{{ threadUnrestricted(t) ? ' (not sandboxed)' : ' (sandboxed)' }}</option>
                      }
                    </select>
                  </label>
                  <div class="block text-xs text-gray-600 dark:text-gray-400">
                    <span class="mb-1 block">Only these Discord user ids may tag it (optional)</span>
                    <app-discord-id-chips
                      label="Allowed Discord user ids"
                      placeholder="Anyone, until you add an id…"
                      [ids]="form.allowIds"
                      (idsChange)="patchForm(bot.id, { allowIds: $event })"
                      data-testid="discord-connect-allow"
                    />
                  </div>
                </div>
                @if (!form.allowIds.length) {
                  <p class="mt-2 text-xs font-medium text-amber-800 dark:text-amber-300" data-testid="discord-empty-allow-note">
                    No allow list: {{ emptyAllowNote }} They can be limited later under "Who can tag".
                  </p>
                }
                @if (selectedThreadUnrestricted(form)) {
                  <div
                    class="mt-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-700/60 dark:bg-amber-900/20 dark:text-amber-200"
                    role="alert"
                    data-testid="discord-unrestricted-warning"
                  >
                    <p class="font-semibold">This thread is not sandboxed.</p>
                    <p class="mt-1">
                      {{ unrestrictedWarning }} Everything said in the Discord channel is read and answered as you, and replies are posted
                      where other people can read them. Sandboxing the thread first is safer.
                    </p>
                    <label class="mt-2 flex items-start gap-2">
                      <input
                        type="checkbox"
                        class="mt-0.5"
                        [ngModel]="form.ackUnrestricted"
                        (ngModelChange)="patchForm(bot.id, { ackUnrestricted: $event })"
                        data-testid="discord-ack-unrestricted"
                      />
                      <span>I understand: {{ unrestrictedWarning }}</span>
                    </label>
                  </div>
                }
                @if (!form.loading && form.guilds.length === 0) {
                  <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                    The bot is not in any server yet. Use <strong>Invite to a server</strong>, then try again.
                  </p>
                }
              } @else {
                <button class="text-sm font-medium text-indigo-600 hover:underline dark:text-indigo-400" (click)="openConnect(bot)">
                  + Connect a channel
                </button>
              }
            </div>
          </div>
        </section>
      }
    </div>
  `,
})
export class DiscordTabComponent implements OnInit {
  private readonly discord = inject(DiscordService);
  private readonly personalities = inject(PersonalityService);
  private readonly confirm = inject(ConfirmationService);
  private readonly chats = inject(ChatService);

  readonly unrestrictedWarning = UNRESTRICTED_WARNING;
  readonly emptyAllowNote = EMPTY_ALLOW_NOTE;

  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  readonly bots = signal<DiscordBot[]>([]);
  readonly bindings = signal<DiscordBinding[]>([]);
  readonly personas = signal<Personality[]>([]);
  readonly newPersonaId = signal('');
  readonly newToken = signal('');
  private readonly forms = signal<Record<string, ConnectForm>>({});
  private readonly drafts = signal<Record<string, AccessDraft>>({});

  /** Personas without a bot (one bot per persona). */
  readonly availablePersonas = computed(() => {
    const taken = new Set(this.bots().map(b => b.personality_id));
    return this.personas().filter(p => !taken.has(p.id));
  });

  readonly label = bindingLabel;

  ngOnInit(): void {
    void this.reload();
    this.personalities.listPersonalities(1, 100).subscribe({
      next: page => this.personas.set(page.results ?? []),
      error: () => this.personas.set([]),
    });
  }

  async reload(): Promise<void> {
    this.loading.set(true);
    try {
      const [bots, bindings] = await Promise.all([firstValueFrom(this.discord.listBots()), firstValueFrom(this.discord.listBindings())]);
      this.bots.set(bots);
      this.bindings.set(bindings);
    } catch (e) {
      this.fail(e, 'Could not load your Discord bots.');
    } finally {
      this.loading.set(false);
    }
  }

  personaName(id: string): string {
    return this.personas().find(p => p.id === id)?.name ?? 'a persona';
  }

  bindingsFor(botId: string): DiscordBinding[] {
    return this.bindings().filter(b => b.bot_id === botId);
  }

  botStatusLabel(status: DiscordBot['status']): string {
    return status === 'active' ? 'Active' : status === 'disabled' ? 'Paused' : 'Token rejected';
  }

  statusClass(status: string): string {
    return status === 'active'
      ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300'
      : status === 'disabled'
        ? 'bg-gray-100 text-gray-700 dark:bg-gray-900/40 dark:text-gray-300'
        : 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300';
  }

  async addBot(): Promise<void> {
    await this.run(async () => {
      const bot = await firstValueFrom(this.discord.addBot(this.newPersonaId(), this.newToken().trim()));
      this.bots.update(list => [...list, bot]);
      this.newToken.set('');
      this.newPersonaId.set('');
    }, 'Could not add the bot.');
  }

  async syncProfile(bot: DiscordBot): Promise<void> {
    await this.run(async () => {
      const res = await firstValueFrom(this.discord.syncProfile(bot.id));
      this.replaceBot(res.bot);
      if (!res.avatar_synced) this.error.set('Name updated. The persona has no portrait to use, so the avatar was left as it is.');
    }, 'Could not update the bot.');
  }

  async replaceToken(bot: DiscordBot): Promise<void> {
    const token = window.prompt('Paste the new bot token');
    if (!token?.trim()) return;
    await this.run(
      async () => this.replaceBot(await firstValueFrom(this.discord.updateBot(bot.id, { token: token.trim() }))),
      'Could not replace the token.',
    );
  }

  async togglePaused(bot: DiscordBot): Promise<void> {
    await this.run(
      async () => this.replaceBot(await firstValueFrom(this.discord.updateBot(bot.id, { enabled: bot.status === 'disabled' }))),
      'Could not update the bot.',
    );
  }

  async removeBot(bot: DiscordBot): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Remove bot',
      message:
        'This disconnects all of its channels. The relay threads stay in your thread list. The Discord application itself is not deleted.',
      confirmText: 'Remove',
      type: 'danger',
    });
    if (!ok) return;
    await this.run(async () => {
      await firstValueFrom(this.discord.removeBot(bot.id));
      this.bots.update(list => list.filter(b => b.id !== bot.id));
      this.bindings.update(list => list.filter(b => b.bot_id !== bot.id));
    }, 'Could not remove the bot.');
  }

  connectFor(botId: string): ConnectForm | null {
    return this.forms()[botId] ?? null;
  }

  async openConnect(bot: DiscordBot): Promise<void> {
    this.patchForm(bot.id, { guilds: [], channels: [], guildId: '', channelId: '', loading: true });
    // The persona's existing threads are an optional alternative to a new relay thread; a
    // failure to list them leaves the (safe) new-thread default.
    this.chats
      .listChatsPage(1, 50, { personality_id: bot.personality_id })
      .subscribe({ next: page => this.patchForm(bot.id, { threads: page.results ?? [] }), error: () => undefined });
    try {
      const guilds = await firstValueFrom(this.discord.listGuilds(bot.id));
      this.patchForm(bot.id, { guilds, loading: false });
    } catch (e) {
      this.patchForm(bot.id, { loading: false });
      this.fail(e, 'Could not list the bot’s servers.');
    }
  }

  threadUnrestricted(t: Chat): boolean {
    return needsRelayAcknowledgement(t.sandboxed);
  }

  selectedThreadUnrestricted(form: ConnectForm): boolean {
    const t = form.threads.find(x => x.id === form.chatId);
    return !!t && this.threadUnrestricted(t);
  }

  /** A channel and (for an existing thread that is not sandboxed) the explicit acknowledgement are needed. */
  canConnect(form: ConnectForm): boolean {
    return !!form.channelId && (!this.selectedThreadUnrestricted(form) || form.ackUnrestricted);
  }

  pickThread(bot: DiscordBot, chatId: string): void {
    // The acknowledgement is for one thread: choosing another starts it unticked.
    this.patchForm(bot.id, { chatId, ackUnrestricted: false });
  }

  async pickGuild(bot: DiscordBot, guildId: string): Promise<void> {
    this.patchForm(bot.id, { guildId, channelId: '', channels: [] });
    if (!guildId) return;
    try {
      this.patchForm(bot.id, { channels: await firstValueFrom(this.discord.listChannels(bot.id, guildId)) });
    } catch (e) {
      this.fail(e, 'Could not list the channels.');
    }
  }

  pickChannel(bot: DiscordBot, channelId: string): void {
    this.patchForm(bot.id, { channelId });
  }

  async connect(bot: DiscordBot): Promise<void> {
    const form = this.connectFor(bot.id);
    if (!form) return;
    const guild = form.guilds.find(g => g.id === form.guildId);
    const channel = form.channels.find(c => c.id === form.channelId);
    if (!guild || !channel) return;
    await this.run(async () => {
      const binding = await firstValueFrom(
        this.discord.addBinding({
          bot_id: bot.id,
          guild_id: guild.id,
          guild_name: guild.name,
          channel_id: channel.id,
          channel_name: channel.name,
          allow_user_ids: form.allowIds,
          ...(form.chatId ? { chat_id: form.chatId } : {}),
          ...(form.chatId && this.selectedThreadUnrestricted(form) ? { allow_unrestricted: form.ackUnrestricted } : {}),
        }),
      );
      this.bindings.update(list => [...list, binding]);
      this.forms.update(f => {
        const next = { ...f };
        delete next[bot.id];
        return next;
      });
    }, 'Could not connect the channel.');
  }

  async setInbound(b: DiscordBinding, on: boolean): Promise<void> {
    await this.run(
      async () => this.replaceBinding(await firstValueFrom(this.discord.updateBinding(b.id, { inbound_enabled: on }))),
      'Could not update the channel.',
    );
  }

  async setAcknowledged(b: DiscordBinding, on: boolean): Promise<void> {
    await this.run(
      async () => this.replaceBinding(await firstValueFrom(this.discord.updateBinding(b.id, { allow_unrestricted: on }))),
      'Could not update the channel.',
    );
  }

  async reactivate(b: DiscordBinding): Promise<void> {
    await this.run(
      async () => this.replaceBinding(await firstValueFrom(this.discord.updateBinding(b.id, { reactivate: true }))),
      'Could not update the channel.',
    );
  }

  draft(b: DiscordBinding): AccessDraft {
    return this.drafts()[b.id] ?? { allow: b.allow_user_ids, deny: b.deny_user_ids };
  }

  setDraft(b: DiscordBinding, key: keyof AccessDraft, value: string[]): void {
    this.drafts.update(d => ({ ...d, [b.id]: { ...this.draft(b), [key]: value } }));
  }

  async saveAccess(b: DiscordBinding): Promise<void> {
    const d = this.draft(b);
    await this.run(async () => {
      const updated = await firstValueFrom(this.discord.updateBinding(b.id, { allow_user_ids: d.allow, deny_user_ids: d.deny }));
      this.replaceBinding(updated);
      this.drafts.update(all => {
        const next = { ...all };
        delete next[b.id];
        return next;
      });
    }, 'Could not save who can tag the bot.');
  }

  async removeBinding(b: DiscordBinding): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Disconnect channel',
      message: `Stop relaying ${bindingLabel(b)}? The relay thread stays in your thread list.`,
      confirmText: 'Disconnect',
      type: 'danger',
    });
    if (!ok) return;
    await this.run(async () => {
      await firstValueFrom(this.discord.removeBinding(b.id));
      this.bindings.update(list => list.filter(x => x.id !== b.id));
    }, 'Could not disconnect the channel.');
  }

  private replaceBot(bot: DiscordBot): void {
    this.bots.update(list => list.map(b => (b.id === bot.id ? bot : b)));
  }

  private replaceBinding(binding: DiscordBinding): void {
    this.bindings.update(list => list.map(b => (b.id === binding.id ? binding : b)));
  }

  patchForm(botId: string, patch: Partial<ConnectForm>): void {
    this.forms.update(f => ({
      ...f,
      [botId]: {
        ...(f[botId] ?? {
          guilds: [],
          channels: [],
          guildId: '',
          channelId: '',
          loading: false,
          threads: [],
          chatId: '',
          ackUnrestricted: false,
          allowIds: [],
        }),
        ...patch,
      },
    }));
  }

  private async run(fn: () => Promise<void>, fallback: string): Promise<void> {
    this.busy.set(true);
    this.error.set(null);
    try {
      await fn();
    } catch (e) {
      this.fail(e, fallback);
    } finally {
      this.busy.set(false);
    }
  }

  private fail(e: unknown, fallback: string): void {
    const msg = e instanceof HttpErrorResponse ? (e.error?.message ?? e.error?.error) : null;
    this.error.set(typeof msg === 'string' && msg ? msg : fallback);
  }
}

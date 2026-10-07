import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { Chat, isSandboxed } from '../../core/models/chat.model';
import { ChatService } from '../../core/services/chat.service';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import {
  DiscordBinding,
  DiscordService,
  UNRESTRICTED_WARNING,
  UpdateDiscordBindingRequest,
  needsRelayAcknowledgement,
} from './discord.service';

/**
 * Moves a channel binding to another relay thread: a new sandboxed thread (the default, and the
 * way back into a sandbox once a thread has left one, since a thread cannot be sandboxed later),
 * or one of the persona's existing threads, with the acknowledgement a thread that is not
 * sandboxed needs. The binding's thread is the only setting the Discord tab cannot otherwise
 * change after connecting.
 */
@Component({
  selector: 'app-discord-binding-thread',
  standalone: true,
  imports: [FormsModule],
  template: `
    @if (!open()) {
      <button type="button" class="text-indigo-600 underline dark:text-indigo-400" (click)="toggle()" data-testid="discord-binding-move">
        Move to another thread
      </button>
    } @else {
      <div class="mt-2 max-w-xl rounded-lg border border-gray-200 p-3 text-xs dark:border-gray-700" data-testid="discord-binding-move-form">
        <label class="block text-gray-600 dark:text-gray-400">
          Relay thread
          <select
            class="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
            [ngModel]="chatId()"
            (ngModelChange)="pick($event)"
            [disabled]="loading() || saving()"
            aria-label="Relay thread"
            data-testid="discord-binding-move-select"
          >
            <option value="">A new sandboxed thread (recommended)</option>
            @for (t of threads(); track t.id) {
              <option [value]="t.id">{{ t.name }}{{ isSandboxed(t) ? ' (sandboxed)' : ' (not sandboxed)' }}</option>
            }
          </select>
        </label>
        @if (selectedUnsandboxed()) {
          <div
            class="mt-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-700/60 dark:bg-amber-900/20 dark:text-amber-200"
            role="alert"
            data-testid="discord-binding-move-warning"
          >
            <p class="font-semibold">That thread is not sandboxed.</p>
            <p class="mt-1">{{ unrestrictedWarning }}</p>
            <label class="mt-2 flex items-start gap-2">
              <input
                type="checkbox"
                class="mt-0.5"
                [ngModel]="ack()"
                (ngModelChange)="ack.set($event)"
                data-testid="discord-binding-move-ack"
              />
              <span>I understand: {{ unrestrictedWarning }}</span>
            </label>
          </div>
        }
        <p class="mt-2 text-gray-500 dark:text-gray-400">
          The channel's conversation continues in the thread you pick; the old thread stays in your threads, unconnected.
        </p>
        <div class="mt-2 flex gap-2">
          <button
            type="button"
            class="rounded-lg bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
            [disabled]="!canMove() || saving() || loading()"
            (click)="move()"
            data-testid="discord-binding-move-confirm"
          >
            {{ saving() ? 'Moving…' : 'Move' }}
          </button>
          <button
            type="button"
            class="rounded-lg border border-gray-300 px-3 py-1.5 font-medium dark:border-gray-600 dark:text-gray-200"
            [disabled]="saving()"
            (click)="toggle()"
          >
            Cancel
          </button>
        </div>
        @if (error(); as message) {
          <p class="mt-2 text-red-700 dark:text-red-300" role="alert">{{ message }}</p>
        }
      </div>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DiscordBindingThreadComponent {
  private readonly discord = inject(DiscordService);
  private readonly chatService = inject(ChatService);

  readonly binding = input.required<DiscordBinding>();
  /** The bot's persona: the new thread is created for it, and existing threads are listed from it. */
  readonly personalityId = input.required<string>();
  /** The binding as the server returned it after the move. */
  readonly moved = output<DiscordBinding>();

  readonly isSandboxed = isSandboxed;
  readonly unrestrictedWarning = UNRESTRICTED_WARNING;
  readonly open = signal(false);
  readonly loading = signal(false);
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);
  readonly threads = signal<Chat[]>([]);
  /** '' = a new sandboxed thread; otherwise an existing thread's id. */
  readonly chatId = signal('');
  readonly ack = signal(false);

  readonly selectedUnsandboxed = computed(() => {
    const t = this.threads().find(x => x.id === this.chatId());
    return !!t && needsRelayAcknowledgement(isSandboxed(t));
  });
  readonly canMove = computed(() => !this.selectedUnsandboxed() || this.ack());

  toggle(): void {
    this.open.update(v => !v);
    this.error.set(null);
    if (this.open() && this.threads().length === 0) void this.loadThreads();
  }

  pick(chatId: string): void {
    // The acknowledgement is for one thread: choosing another starts it unticked.
    this.chatId.set(chatId);
    this.ack.set(false);
  }

  private async loadThreads(): Promise<void> {
    this.loading.set(true);
    try {
      const page = await firstValueFrom(this.chatService.listChatsPage(1, 50, { personality_id: this.personalityId() }));
      this.threads.set((page.results ?? []).filter(c => c.id !== this.binding().chat_id));
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Could not list this persona’s threads.'));
    } finally {
      this.loading.set(false);
    }
  }

  async move(): Promise<void> {
    if (!this.canMove() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      let body: UpdateDiscordBindingRequest;
      if (this.chatId()) {
        body = { chat_id: this.chatId(), ...(this.selectedUnsandboxed() ? { allow_unrestricted: this.ack() } : {}) };
      } else {
        // The same thread the server would create for a new binding: sandboxed, for the bot's
        // persona, named after the channel (the app's create route gives it the sandbox defaults).
        const created = await firstValueFrom(
          this.chatService.createChat({
            name: `Discord · #${this.binding().channel_name || this.binding().channel_id}`,
            personality_id: this.personalityId(),
            context_scope: 'sandbox',
          }),
        );
        body = { chat_id: created.id };
      }
      const updated = await firstValueFrom(this.discord.updateBinding(this.binding().id, body));
      this.moved.emit(updated);
      this.open.set(false);
      this.chatId.set('');
      this.ack.set(false);
      this.threads.set([]);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Could not move the channel to that thread.'));
    } finally {
      this.saving.set(false);
    }
  }
}

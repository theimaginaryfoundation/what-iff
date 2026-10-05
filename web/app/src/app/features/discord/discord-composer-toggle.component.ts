import { ChangeDetectionStrategy, Component, effect, inject, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { ContextPanelService } from '../chat/services/context-panel.service';
import { DiscordBinding, DiscordService, bindingLabel } from './discord.service';

/**
 * Above the composer in a relay thread: "Post the next reply to #channel". Off by
 * default and not sticky; the server consumes it with the next reply (or it
 * expires). Renders nothing outside relay threads, or when the server does not run
 * the relay. Rendered by chat-page next to the composer.
 */
@Component({
  selector: 'app-discord-composer-toggle',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (bindings().length > 0) {
      <div class="toggle" data-testid="discord-composer-toggle">
        @for (b of bindings(); track b.id) {
          <label class="toggle__option" [class.toggle__option--on]="pending().has(b.id)" [class.toggle__option--disabled]="saving()">
            <input
              type="checkbox"
              class="toggle__box"
              [checked]="pending().has(b.id)"
              [disabled]="saving()"
              (change)="toggle(b, $any($event.target).checked)"
            />
            <span class="toggle__text"
              >Post the next reply to <strong>{{ label(b) }}</strong></span
            >
          </label>
        }
      </div>
    }
  `,
  styles: [
    `
      .toggle {
        display: flex;
        flex-wrap: wrap;
        gap: 0.5rem;
        margin-bottom: 0.5rem;
      }

      .toggle__option {
        align-items: center;
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 999px;
        color: var(--color-text-muted);
        cursor: pointer;
        display: inline-flex;
        font-size: 0.75rem;
        gap: 0.5rem;
        padding: 0.3rem 0.75rem 0.3rem 0.5rem;
        transition:
          background 120ms ease,
          border-color 120ms ease,
          color 120ms ease;
      }

      .toggle__option:hover {
        border-color: color-mix(in srgb, var(--color-accent) 55%, var(--color-border-base));
      }

      .toggle__option--on {
        background: color-mix(in srgb, var(--color-accent) 12%, transparent);
        border-color: var(--color-accent);
        color: var(--color-text-primary);
      }

      .toggle__option--disabled {
        cursor: progress;
        opacity: 0.6;
      }

      .toggle__text strong {
        font-weight: 600;
      }

      .toggle__box {
        appearance: none;
        background: var(--color-surface-input);
        border: 1.5px solid color-mix(in srgb, var(--color-accent) 45%, var(--color-border-base));
        border-radius: 0.3rem;
        cursor: inherit;
        display: grid;
        flex: none;
        height: 1rem;
        margin: 0;
        place-content: center;
        transition:
          background 120ms ease,
          border-color 120ms ease;
        width: 1rem;
      }

      .toggle__box::before {
        box-shadow: inset 1rem 1rem #fff;
        clip-path: polygon(14% 44%, 0 65%, 50% 100%, 100% 16%, 80% 0, 43% 62%);
        content: '';
        height: 0.45rem;
        transform: scale(0);
        transition: transform 100ms ease;
        width: 0.45rem;
      }

      .toggle__box:checked {
        background: var(--color-accent);
        border-color: var(--color-accent);
      }

      .toggle__box:checked::before {
        transform: scale(1);
      }

      .toggle__box:focus-visible {
        outline: 2px solid color-mix(in srgb, var(--color-accent) 55%, transparent);
        outline-offset: 2px;
      }
    `,
  ],
})
export class DiscordComposerToggleComponent {
  private readonly discord = inject(DiscordService);
  private readonly contextPanel = inject(ContextPanelService);

  readonly bindings = signal<DiscordBinding[]>([]);
  readonly pending = signal<ReadonlySet<string>>(new Set());
  readonly saving = signal(false);
  readonly label = bindingLabel;

  constructor() {
    // Reload when the thread changes, and when a new reply lands (the server
    // consumes the pending post with that reply, so the toggle resets).
    effect(() => {
      const chatId = this.contextPanel.activeChatId();
      this.contextPanel.latestBreakdown();
      untracked(() => void this.load(chatId));
    });
  }

  private async load(chatId: string | null): Promise<void> {
    if (!chatId || !(await firstValueFrom(this.discord.available()))) {
      this.bindings.set([]);
      return;
    }
    try {
      const state = await firstValueFrom(this.discord.chatState(chatId));
      if (this.contextPanel.activeChatId() !== chatId) return;
      this.bindings.set(state.bindings.filter(b => b.status === 'active'));
      this.pending.set(new Set(state.pending_binding_ids));
    } catch {
      this.bindings.set([]);
    }
  }

  async toggle(b: DiscordBinding, on: boolean): Promise<void> {
    const chatId = this.contextPanel.activeChatId();
    if (!chatId) return;
    this.saving.set(true);
    try {
      await firstValueFrom(this.discord.setPostNextReply(chatId, b.id, on));
      const next = new Set(this.pending());
      if (on) next.add(b.id);
      else next.delete(b.id);
      this.pending.set(next);
    } catch {
      // Leave the box as the server has it.
      await this.load(chatId);
    } finally {
      this.saving.set(false);
    }
  }
}

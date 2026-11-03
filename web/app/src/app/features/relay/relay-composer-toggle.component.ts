import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Observable, firstValueFrom } from 'rxjs';

import { isSandboxed } from '../../core/models/chat.model';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import { saveCheckbox } from '../../core/utils/checkbox.helpers';
import { ContextPanelService } from '../chat/services/context-panel.service';

/** The part of a relay binding the composer toggle reads. */
export interface RelayComposerBinding {
  id: string;
  status: string;
  /** The owner's acknowledgement that the bound thread is not sandboxed. */
  allow_unrestricted: boolean;
}

/** One relay's API for a thread: the Discord, Buzz and Slack services each fit it. */
export interface RelayChatApi<B extends RelayComposerBinding> {
  available(): Observable<boolean>;
  chatState(chatId: string): Observable<{ bindings: B[]; pending_binding_ids: string[] }>;
  setPostNextReply(chatId: string, bindingId: string, on: boolean): Observable<void>;
}

/** What a relay's composer toggle says and where it reads from. */
export interface RelayComposerSource<B extends RelayComposerBinding> {
  api: RelayChatApi<B>;
  /** The relay's name, as the Integrations tab is labelled ("Discord"). */
  platform: string;
  /** The Integrations tab id ("discord"); also the prefix of the data-testids. */
  tab: string;
  /** What starts a turn from the relay, for the paused note: "tags" or "mentions". */
  triggers: string;
  /** "#general · Home" for a binding. */
  label(binding: B): string;
  /** After "Post the next reply to #channel": " on Buzz", say. */
  toggleSuffix?: string;
  /** Another way out of the paused state, after "until you acknowledge it": ", or move the channel…,". */
  pausedRemedy?: string;
}

/**
 * Above the composer in a relay thread: "Post the next reply to #channel". Off by default and not
 * sticky; the server consumes it with the next reply (or it expires). Renders nothing outside the
 * relay's threads, or when the relay is off for the user. Also says when the relay's mentions are
 * paused because the thread left the sandbox without the owner's acknowledgement. Each relay wraps
 * it with its own source (app-discord-composer-toggle, app-buzz-composer-toggle,
 * app-slack-composer-toggle), and chat-page renders those next to the composer.
 */
@Component({
  selector: 'app-relay-composer-toggle',
  standalone: true,
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let s = source();
    @if (pausedBindings().length > 0) {
      <p class="paused" role="status" [attr.data-testid]="s.tab + '-paused-note'">
        This thread is no longer sandboxed, so {{ s.platform }} {{ s.triggers }} in
        @for (b of pausedBindings(); track b.id) {
          <strong>{{ s.label(b) }}</strong
          >{{ $last ? '' : ', ' }}
        }
        are paused until you acknowledge it{{ s.pausedRemedy ?? '' }} in
        <a [routerLink]="['/integrations']" [queryParams]="{ tab: s.tab }">Integrations → {{ s.platform }}</a
        >.
      </p>
    }
    @if (bindings().length > 0) {
      <div class="toggle" [attr.data-testid]="s.tab + '-composer-toggle'">
        @for (b of bindings(); track b.id) {
          <label class="toggle__option" [class.toggle__option--on]="pending().has(b.id)" [class.toggle__option--disabled]="saving()">
            <input
              type="checkbox"
              class="toggle__box"
              [checked]="pending().has(b.id)"
              [disabled]="saving()"
              (change)="toggle(b, $any($event.target))"
            />
            <span class="toggle__text"
              >Post the next reply to <strong>{{ s.label(b) }}</strong
              >{{ s.toggleSuffix ?? '' }}</span
            >
          </label>
        }
      </div>
    }
    @if (error(); as message) {
      <p class="error" role="alert" [attr.data-testid]="s.tab + '-composer-toggle-error'">{{ message }}</p>
    }
  `,
  styles: [
    `
      .paused {
        background: color-mix(in srgb, var(--color-warning) 10%, transparent);
        border: 1px solid color-mix(in srgb, var(--color-warning) 40%, var(--color-border-base));
        border-radius: 0.5rem;
        color: var(--color-text-primary);
        font-size: 0.75rem;
        margin: 0 0 0.5rem;
        padding: 0.5rem 0.75rem;
      }
      .paused a {
        color: var(--color-accent, var(--color-text-primary));
        text-decoration: underline;
      }
      .error {
        color: var(--color-danger);
        font-size: 0.75rem;
        margin: 0 0 0.5rem;
      }
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
export class RelayComposerToggleComponent<B extends RelayComposerBinding> {
  private readonly contextPanel = inject(ContextPanelService);

  readonly source = input.required<RelayComposerSource<B>>();

  readonly bindings = signal<B[]>([]);
  /**
   * Bindings the relay will not answer right now: the thread is not sandboxed (read live from the
   * panel's chat, so leaving the sandbox shows this at once) and the owner has not acknowledged
   * the binding. Nothing is posted to the relay about it, so this is where the owner learns.
   */
  readonly pausedBindings = computed(() => {
    const chat = this.contextPanel.activeChat();
    if (!chat || isSandboxed(chat)) return [];
    return this.bindings().filter(b => !b.allow_unrestricted);
  });
  readonly pending = signal<ReadonlySet<string>>(new Set());
  readonly saving = signal(false);
  /** Why the last toggle did not save; cleared by the next toggle or a reload of the thread. */
  readonly error = signal<string | null>(null);

  constructor() {
    // Reload when the thread changes, and when a new reply lands (the server consumes the
    // pending post with that reply, so the toggle resets).
    effect(() => {
      const chatId = this.contextPanel.activeChatId();
      this.contextPanel.latestBreakdown();
      untracked(() => {
        this.error.set(null);
        void this.load(chatId);
      });
    });
  }

  private async load(chatId: string | null): Promise<void> {
    const api = this.source().api;
    if (!chatId || !(await firstValueFrom(api.available()))) {
      this.bindings.set([]);
      return;
    }
    try {
      const state = await firstValueFrom(api.chatState(chatId));
      if (this.contextPanel.activeChatId() !== chatId) return;
      this.bindings.set(state.bindings.filter(b => b.status === 'active'));
      this.pending.set(new Set(state.pending_binding_ids));
    } catch {
      this.bindings.set([]);
    }
  }

  async toggle(b: B, box: HTMLInputElement): Promise<void> {
    const chatId = this.contextPanel.activeChatId();
    if (!chatId) return;
    const platform = this.source().platform;
    this.saving.set(true);
    this.error.set(null);
    try {
      const saved = await saveCheckbox(box, async on => {
        try {
          await firstValueFrom(this.source().api.setPostNextReply(chatId, b.id, on));
        } catch (e) {
          // Say so: an untick that did not save means the next reply is still posted.
          this.error.set(
            apiErrorMessage(
              e,
              on ? `Could not set the next reply to post on ${platform}.` : `Could not stop the next reply from posting on ${platform}.`,
            ),
          );
          return false;
        }
        const next = new Set(this.pending());
        if (on) next.add(b.id);
        else next.delete(b.id);
        this.pending.set(next);
        return true;
      });
      // The box is back where it was; reload in case the server did act on it after all.
      if (!saved) await this.load(chatId);
    } finally {
      this.saving.set(false);
    }
  }
}

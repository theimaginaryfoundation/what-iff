import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { isSandboxed } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { apiErrorMessage } from '../../../../core/utils/api-error.helpers';
import { ButtonComponent } from '../../../../shared/ui/button/button.component';
import { HelpHintComponent } from '../../../../shared/ui/help-hint/help-hint.component';
import { ContextPanelService } from '../../services/context-panel.service';

/** What a sandboxed thread can and cannot do. Shared by the help hint and the on-state note. */
export const SANDBOX_COPY =
  'A sandboxed thread reads only itself: no memories made elsewhere, no other conversations, no scratchpad, no account-wide files, jobs, skills or personalities, and only files uploaded to it. Memories it creates stay in this thread. It starts with no connectors and with web search, page fetching, image generation and sub-agents switched off; turn any of those on in the Tools tab, or attach a connector, if this thread should have them. It is never given your name and never picks moods automatically.';

/** Why the sandbox cannot be turned on later: shown in the off state. */
export const SANDBOX_CREATE_ONLY_NOTE =
  'Off: this thread can use your memories, other conversations and account files. A thread can only be sandboxed when it is created, since this one has already read your account.';

/**
 * "Sandbox" setting for the active thread. A thread is sandboxed when it is created, so this
 * panel offers a new sandboxed thread (same personality and model) while the thread is not one,
 * and a confirmed way out while it is. Leaving is saved with the chat PATCH; the updated chat is
 * published so the thread header and this panel stay in sync.
 */
@Component({
  selector: 'app-thread-sandbox',
  standalone: true,
  imports: [ButtonComponent, HelpHintComponent],
  template: `
    @if (chat(); as activeChat) {
      <div class="sandbox">
        <div class="sandbox__row">
          <span class="sandbox__label">{{ sandboxed() ? 'Sandboxed thread' : 'Sandbox' }}</span>
          <ui-help-hint label="What is a sandbox?" heading="Sandbox" align="end">
            {{ copy }}
          </ui-help-hint>
        </div>
        @if (sandboxed()) {
          <p class="sandbox__note">{{ copy }}</p>
          <ui-button size="sm" variant="secondary" [disabled]="saving()" (activate)="leaveSandbox()" data-testid="sandbox-leave">
            Turn off sandbox
          </ui-button>
        } @else {
          <p class="sandbox__note">{{ createOnlyNote }}</p>
          <ui-button
            size="sm"
            variant="secondary"
            [disabled]="saving()"
            (activate)="startSandboxedThread()"
            data-testid="sandbox-new-thread"
          >
            New sandboxed thread
          </ui-button>
        }
        @if (error(); as message) {
          <p class="sandbox__error" role="alert">{{ message }}</p>
        }
      </div>
    }
  `,
  styles: [
    `
      :host {
        display: block;
        flex-shrink: 0;
        margin-bottom: 0.625rem;
      }

      .sandbox {
        background: var(--color-surface-elevated);
        border: 1px solid var(--color-border-base);
        border-radius: 0.375rem;
        display: grid;
        gap: 0.375rem;
        justify-items: start;
        min-width: 0;
        padding: 0.5rem 0.625rem;
      }

      .sandbox__row {
        align-items: center;
        display: flex;
        gap: 0.25rem;
        justify-content: space-between;
        justify-self: stretch;
      }

      .sandbox__label {
        color: var(--color-text-primary);
        font-size: 0.8125rem;
        font-weight: 600;
      }

      .sandbox__note,
      .sandbox__error {
        font-size: 0.75rem;
        margin: 0;
      }

      .sandbox__note {
        color: var(--color-text-secondary);
      }

      .sandbox__error {
        color: var(--color-danger);
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadSandboxComponent {
  private readonly context = inject(ContextPanelService);
  private readonly chatService = inject(ChatService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly router = inject(Router);

  readonly copy = SANDBOX_COPY;
  readonly createOnlyNote = SANDBOX_CREATE_ONLY_NOTE;

  readonly chat = this.context.activeChat;
  readonly sandboxed = computed(() => isSandboxed(this.chat()));
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  /** Leaves the sandbox for good (a thread cannot re-enter one), after the owner confirms. */
  async leaveSandbox(): Promise<void> {
    const chat = this.chat();
    if (!chat || !this.sandboxed() || this.saving()) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Turn off sandbox?',
      message:
        "This thread will be able to read your memories, other conversations, scratchpad and account-wide files, jobs, skills and personalities, and will use the full tool set. Everything said in it so far, by anyone who could talk to it, becomes part of what your personality learns from. This can't be undone: a thread can only be sandboxed when it is created.",
      confirmText: 'Turn off sandbox',
      type: 'warning',
    });
    if (!confirmed) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const updated = await firstValueFrom(this.chatService.patchChat(chat.id, { context_scope: 'account' }));
      this.context.publishThreadUpdate(updated);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to turn off the sandbox'));
    } finally {
      this.saving.set(false);
    }
  }

  /** Starts a new sandboxed thread with this thread's personality and model, and opens it. */
  async startSandboxedThread(): Promise<void> {
    const chat = this.chat();
    if (!chat || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const created = await firstValueFrom(
        this.chatService.createChat({
          name: 'New Chat',
          personality_id: chat.personality_id,
          model_id: chat.model_id,
          context_scope: 'sandbox',
        }),
      );
      this.chatService.setLastChatId(created.id);
      await this.router.navigate(['/chat', created.id]);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to start a sandboxed thread'));
    } finally {
      this.saving.set(false);
    }
  }
}

import { ChangeDetectionStrategy, Component, ElementRef, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { apiErrorMessage } from '../../../../core/utils/api-error.helpers';
import { HelpHintComponent } from '../../../../shared/ui/help-hint/help-hint.component';
import { ContextPanelService } from '../../services/context-panel.service';

/** What a sandboxed thread can and cannot see. Shared by the help hint and the on-state note. */
export const SANDBOX_COPY =
  "A sandboxed thread can't read anything outside itself: only memories created in this thread, no other conversations, no scratchpad, no account-wide files, jobs, skills or personalities. It can only use files uploaded to it, and has a locked-down set of tools. Memories it creates are visible only in this thread. It is never given your name and never picks moods automatically.";

/**
 * "Sandbox" setting for the active thread. Turning it on takes effect right away; turning it off
 * widens what the thread can read, so it asks first. Saved with the chat PATCH; the updated chat is
 * published so the thread header and this panel stay in sync.
 */
@Component({
  selector: 'app-thread-sandbox',
  standalone: true,
  imports: [HelpHintComponent],
  template: `
    @if (chat(); as activeChat) {
      <div class="sandbox">
        <div class="sandbox__row">
          <label class="sandbox__toggle">
            <input
              type="checkbox"
              role="switch"
              [checked]="sandboxed()"
              [disabled]="saving()"
              (change)="toggle($any($event.target).checked)"
            />
            <span class="sandbox__label">Sandbox</span>
          </label>
          <ui-help-hint label="What is a sandbox?" heading="Sandbox" align="end">
            {{ copy }}
          </ui-help-hint>
        </div>
        @if (sandboxed()) {
          <p class="sandbox__note">{{ copy }}</p>
        } @else {
          <p class="sandbox__note">Off: this thread can use your memories, other conversations and account files. Turn it on to keep it self-contained.</p>
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
        min-width: 0;
        padding: 0.5rem 0.625rem;
      }

      .sandbox__row {
        align-items: center;
        display: flex;
        gap: 0.25rem;
        justify-content: space-between;
      }

      .sandbox__toggle {
        align-items: center;
        cursor: pointer;
        display: inline-flex;
        gap: 0.4rem;
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
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly copy = SANDBOX_COPY;

  readonly chat = this.context.activeChat;
  readonly sandboxed = computed(() => this.chat()?.sandboxed === true);
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  async toggle(next: boolean): Promise<void> {
    const chat = this.chat();
    if (!chat || next === this.sandboxed() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      if (!next) {
        const confirmed = await this.confirmation.confirm({
          title: 'Turn off sandbox?',
          message:
            'This thread will be able to read your memories, other conversations, scratchpad and account-wide files, jobs, skills and personalities, and will use the full tool set.',
          confirmText: 'Turn off sandbox',
          type: 'warning',
        });
        if (!confirmed) {
          this.resyncSwitch();
          return;
        }
      }
      const updated = await firstValueFrom(this.chatService.patchChat(chat.id, { sandboxed: next }));
      this.context.publishThreadUpdate(updated);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to update sandbox'));
      this.resyncSwitch();
    } finally {
      this.saving.set(false);
    }
  }

  /** The bound value didn't change, so Angular won't revert the checkbox the user just clicked. */
  private resyncSwitch(): void {
    const input = this.host.nativeElement.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (input) input.checked = this.sandboxed();
  }
}

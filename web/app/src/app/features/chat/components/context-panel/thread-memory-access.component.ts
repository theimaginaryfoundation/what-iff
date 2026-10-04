import { ChangeDetectionStrategy, Component, ElementRef, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { MemorySensitivity } from '../../../../core/models/memory.model';
import { ChatService } from '../../../../core/services/chat.service';
import { apiErrorMessage } from '../../../../core/utils/api-error.helpers';
import { HelpHintComponent } from '../../../../shared/ui/help-hint/help-hint.component';
import {
  isMemoryRestricted,
  MEMORY_ACCESS_OPTIONS,
  memoryAccessOf,
  RESTRICTED_WITHHELD_COPY,
} from '../../helpers/memory-access.helpers';
import { ContextPanelService } from '../../services/context-panel.service';

/**
 * "Memory access" setting for the active thread: caps which memories (by sensitivity) it may use.
 * Anything below Everything also sandboxes the thread, which the copy spells out. Saved with the
 * chat PATCH; the updated chat is published so the thread header and this panel stay in sync.
 */
@Component({
  selector: 'app-thread-memory-access',
  standalone: true,
  imports: [HelpHintComponent],
  template: `
    @if (chat(); as activeChat) {
      <div class="access" role="radiogroup" aria-labelledby="thread-memory-access-label">
        <div class="access__legend">
          <span id="thread-memory-access-label">Memory access</span>
          <ui-help-hint label="What is memory access?" heading="Memory access" align="end">
            Limits which memories this thread can use, by how sensitive they are. Set each memory's sensitivity in the
            memory manager. {{ withheldCopy }}
          </ui-help-hint>
        </div>
        @for (option of options; track option.value) {
          <label class="access__option">
            <input
              type="radio"
              name="memory-access"
              [value]="option.value"
              [checked]="limit() === option.value"
              [disabled]="saving()"
              (change)="select(option.value)"
            />
            <span class="access__label">{{ option.label }}</span>
            <small>{{ option.description }}</small>
          </label>
        }
        @if (restricted()) {
          <p class="access__note">{{ withheldCopy }}</p>
        }
        @if (error(); as message) {
          <p class="access__error" role="alert">{{ message }}</p>
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

      .access {
        background: var(--color-surface-elevated);
        border: 1px solid var(--color-border-base);
        border-radius: 0.375rem;
        display: grid;
        gap: 0.375rem;
        margin: 0;
        min-width: 0;
        padding: 0.5rem 0.625rem;
      }

      .access__legend {
        align-items: center;
        color: var(--color-text-muted);
        display: flex;
        font-size: 0.625rem;
        font-weight: 700;
        gap: 0.25rem;
        letter-spacing: 0.06em;
        text-transform: uppercase;
      }

      .access__option {
        align-items: baseline;
        column-gap: 0.4rem;
        cursor: pointer;
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        row-gap: 0.1rem;
      }

      .access__option input {
        grid-row: span 2;
      }

      .access__label {
        color: var(--color-text-primary);
        font-size: 0.8125rem;
        font-weight: 600;
      }

      .access__option small {
        color: var(--color-text-muted);
        font-size: 0.75rem;
      }

      .access__note,
      .access__error {
        font-size: 0.75rem;
        margin: 0;
      }

      .access__note {
        color: var(--color-text-secondary);
      }

      .access__error {
        color: var(--color-danger);
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadMemoryAccessComponent {
  private readonly context = inject(ContextPanelService);
  private readonly chatService = inject(ChatService);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly options = MEMORY_ACCESS_OPTIONS;
  readonly withheldCopy = RESTRICTED_WITHHELD_COPY;

  readonly chat = this.context.activeChat;
  readonly limit = computed(() => memoryAccessOf(this.chat()));
  readonly restricted = computed(() => isMemoryRestricted(this.limit()));
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  async select(limit: MemorySensitivity): Promise<void> {
    const chat = this.chat();
    if (!chat || limit === this.limit() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const updated = await firstValueFrom(this.chatService.patchChat(chat.id, { memory_sensitivity_limit: limit }));
      this.context.publishThreadUpdate(updated);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to update memory access'));
      this.resyncRadios();
    } finally {
      this.saving.set(false);
    }
  }

  /** The bound limit didn't change, so Angular won't un-tick the radio the user just clicked. */
  private resyncRadios(): void {
    const current = this.limit();
    this.host.nativeElement.querySelectorAll<HTMLInputElement>('input[type="radio"]').forEach(radio => {
      radio.checked = radio.value === current;
    });
  }
}

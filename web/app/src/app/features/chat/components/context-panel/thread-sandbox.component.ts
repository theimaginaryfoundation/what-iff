import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { isSandboxed } from '../../../../core/models/chat.model';
import { ButtonComponent } from '../../../../shared/ui/button/button.component';
import { HelpHintComponent } from '../../../../shared/ui/help-hint/help-hint.component';
import { ContextPanelService } from '../../services/context-panel.service';
import { ThreadSandboxService } from '../../services/thread-sandbox.service';

/** What a sandboxed thread can and cannot do. Shown in the help hint. */
export const SANDBOX_COPY =
  'A sandboxed thread reads only itself: no memories made elsewhere, no other conversations, no scratchpad, no account-wide files, jobs, skills or personalities, and only files uploaded to it. Memories it creates stay in this thread. It starts with no connectors and with web search, page fetching, image generation and sub-agents switched off; turn any of those on in the Tools tab, or attach a connector, if this thread should have them. It is never given your name and never picks moods automatically.';

/**
 * The sandbox box for the active thread, shown only while the thread is sandboxed. A thread can leave
 * a sandbox but never enter one (docs/ARCHITECTURE_SUMMARY.md), so an ordinary thread shows nothing
 * here. Leaving asks for confirmation through ThreadSandboxService, the same as the header chip.
 */
@Component({
  selector: 'app-thread-sandbox',
  standalone: true,
  imports: [ButtonComponent, HelpHintComponent],
  template: `
    @if (chat() && sandboxed()) {
      <div class="sandbox">
        <div class="sandbox__row">
          <span class="sandbox__label">Sandboxed thread</span>
          <ui-help-hint label="What is a sandbox?" heading="Sandbox" align="end">
            {{ copy }}
          </ui-help-hint>
        </div>
        <dl class="sandbox__sees" aria-label="What this thread can see" data-testid="sandbox-sees">
          @for (row of sees(); track row.label) {
            <div class="sandbox__sees-row">
              <dt>{{ row.label }}</dt>
              <dd [class.sandbox__sees-value--closed]="row.closed">{{ row.value }}</dd>
            </div>
          }
        </dl>
        <ui-button size="sm" variant="secondary" [disabled]="saving()" (activate)="leaveSandbox()" data-testid="sandbox-leave">
          Remove from sandbox
        </ui-button>
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

      .sandbox__sees {
        display: grid;
        gap: 0.125rem;
        justify-self: stretch;
        margin: 0;
      }

      .sandbox__sees-row {
        display: flex;
        font-size: 0.75rem;
        gap: 0.5rem;
        justify-content: space-between;
      }

      .sandbox__sees-row dt {
        color: var(--color-text-secondary);
      }

      .sandbox__sees-row dd {
        color: var(--color-text-primary);
        font-weight: 600;
        margin: 0;
      }

      .sandbox__sees-value--closed {
        color: var(--color-warning) !important;
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
  private readonly sandbox = inject(ThreadSandboxService);

  readonly copy = SANDBOX_COPY;

  readonly chat = this.context.activeChat;
  readonly sandboxed = computed(() => isSandboxed(this.chat()));
  /** What the thread can see, in a few words. `closed` rows are the ones the sandbox shuts. */
  readonly sees = computed<readonly { label: string; value: string; closed: boolean }[]>(() => {
    const sandboxed = this.sandboxed();
    return [
      { label: 'Your memories', value: sandboxed ? 'This thread only' : 'All', closed: sandboxed },
      { label: 'Other conversations', value: sandboxed ? 'Hidden' : 'Visible', closed: sandboxed },
      { label: 'Account files', value: sandboxed ? 'Hidden' : 'Visible', closed: sandboxed },
      { label: 'Tools', value: sandboxed ? 'Limited' : 'All', closed: sandboxed },
      { label: 'Connectors', value: sandboxed ? 'None to start' : 'Your defaults', closed: sandboxed },
    ];
  });
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  /** Leaves the sandbox for good (a thread cannot re-enter one), after the owner confirms. */
  async leaveSandbox(): Promise<void> {
    if (this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const result = await this.sandbox.leave(this.chat());
      if (result.status === 'failed') this.error.set(result.message);
    } finally {
      this.saving.set(false);
    }
  }
}

import { ChangeDetectionStrategy, Component, computed, effect, inject, input } from '@angular/core';

import { ContextPanelService } from '../../../services/context-panel.service';
import { ScratchpadService } from '../../../services/scratchpad.service';
import { ButtonComponent } from '../../../../../shared/ui/button/button.component';

@Component({
  selector: 'app-context-scratchpad-tab',
  standalone: true,
  imports: [ButtonComponent],
  template: `
    <section class="tab-body" aria-label="Thread scratchpad">
      @if (!chatId()) {
        <p class="state">Open a thread to view context.</p>
      } @else if (scratchpad.loading()) {
        <p class="state">Loading scratchpad…</p>
      } @else {
        <label for="scratchpad-input" class="label">{{ scratchpadHeading() }}</label>
        <textarea
          id="scratchpad-input"
          [value]="scratchpad.value()"
          [disabled]="!canSave()"
          (input)="scratchpad.updateDraft($any($event.target).value)"
          aria-describedby="scratchpad-help"
          placeholder="Capture thread-specific notes"
        ></textarea>
        <p id="scratchpad-help" class="hint">
          @if (canSave()) { Autosaves changes for this conversation. } @else { Attach a personality to enable scratchpad saving. }
        </p>
        <div class="actions">
          <ui-button size="sm" variant="secondary" (activate)="copyToComposer()">Copy to composer</ui-button>
          @if (canSave()) {
            <ui-button size="sm" variant="primary" [loading]="scratchpad.status() === 'saving'" (activate)="saveNow()">
              Save now
            </ui-button>
          }
        </div>
        @if (scratchpad.error(); as error) {
          <p class="error" role="alert">{{ error }}</p>
        } @else if (scratchpad.status() === 'saved') {
          <p class="saved">Saved</p>
        }
        <details>
          <summary>Summary</summary>
          <p class="summary">{{ scratchpad.summary() || 'No summary available yet.' }}</p>
        </details>
      }
    </section>
  `,
  styles: [`
    .tab-body {
      display: grid;
      gap: 0.6rem;
    }

    .label {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      font-weight: 700;
      text-transform: uppercase;
    }

    textarea {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.75rem;
      color: var(--color-text-primary);
      min-height: 12rem;
      padding: 0.75rem;
      resize: vertical;
      width: 100%;
    }

    .actions {
      display: flex;
      gap: 0.5rem;
    }

    .hint,
    .saved,
    .state,
    .summary {
      color: var(--color-text-muted);
      font-size: 0.8rem;
      margin: 0;
    }

    .error {
      color: var(--color-danger);
      margin: 0;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextScratchpadTabComponent {
  readonly chatId = input<string | null>(null);
  readonly canSave = input(false);
  readonly personalityName = input<string | null>(null);

  readonly scratchpad = inject(ScratchpadService);
  private readonly contextPanel = inject(ContextPanelService);
  private readonly loadTrigger = computed(() => ({ chatId: this.chatId(), canSave: this.canSave() }));

  constructor() {
    effect(() => {
      const state = this.loadTrigger();
      void this.scratchpad.load(state.chatId, state.canSave);
    });
  }

  async saveNow(): Promise<void> {
    await this.scratchpad.saveNow();
  }

  copyToComposer(): void {
    this.contextPanel.requestComposerInsert(this.scratchpad.value());
  }

  scratchpadHeading(): string {
    const trimmedName = this.personalityName()?.trim();
    if (!trimmedName) return 'Scratchpad';
    const suffix = trimmedName.endsWith('s') ? '\'' : '\'s';
    return `${trimmedName}${suffix} Scratchpad`;
  }
}

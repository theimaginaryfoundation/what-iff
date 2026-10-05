import { DatePipe, UpperCasePipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
} from '@angular/core';

import { PersonalityPromptChange } from '../../../core/models/personality.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { HelpHintComponent } from '../../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/**
 * Append-only history of one personality's system prompt, shown under the
 * prompt editor on the personality detail page. Collapsed by default (#76);
 * the list is fetched on first expand and refetched whenever the persisted
 * prompt changes, so a save made in the editor above shows up here.
 *
 * "Restore previous" writes a new `revert` entry server-side and emits
 * `restored` so the host can reload the prompt it displays.
 */
@Component({
  selector: 'app-personality-prompt-history',
  standalone: true,
  imports: [DatePipe, UpperCasePipe, HelpHintComponent, TooltipDirective],
  template: `
    <section
      class="flex flex-col gap-3 rounded-2xl border border-(--color-border-default) bg-(--color-surface-card) p-4"
      aria-labelledby="prompt-change-heading"
    >
      <header class="flex items-center justify-between gap-2">
        <h2 id="prompt-change-heading" class="inline-flex items-center gap-1 text-base font-semibold text-(--color-text-primary)">
          Prompt changes
          <ui-help-hint label="What are prompt changes?" heading="Prompt changes" guide="context">
            Every edit to this personality's system prompt, with before and after. Restoring an earlier prompt is
            recorded as a new change, so nothing here is ever rewritten.
          </ui-help-hint>
        </h2>
        <button
          type="button"
          class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
          data-testid="prompt-change-toggle"
          [attr.aria-expanded]="expanded()"
          (click)="toggle()"
        >{{ expanded() ? 'Hide history' : 'Show history' }}</button>
      </header>

      @if (expanded()) {
        <div class="flex flex-col gap-3">
          @if (loading()) {
            <p class="text-sm text-(--color-text-secondary)" role="status">Loading prompt changes…</p>
          } @else if (error()) {
            <p class="text-sm text-red-500" role="alert">{{ error() }}</p>
          } @else if (changes().length === 0) {
            <p class="text-sm text-(--color-text-secondary)">No prompt changes logged yet.</p>
          } @else {
            @if (notice()) {
              <p class="text-sm text-(--color-text-secondary)" role="status">{{ notice() }}</p>
            }
            <ul class="prompt-history-list" role="list" aria-label="Personality prompt changes">
              @for (change of changes(); track change.id) {
                <li class="prompt-change-card">
                  <div class="prompt-change-card__head">
                    <span class="prompt-change-card__action">{{ change.action === 'revert' ? 'Restored' : 'Edited' }}</span>
                    <div class="prompt-change-card__meta">
                      <span>{{ change.created_at | date:'MMM d, y' | uppercase }}</span>
                      <span>{{ change.created_at | date:'h:mm a' }}</span>
                    </div>
                  </div>
                  <div class="prompt-change-card__body">
                    <section class="diff-block">
                      <div class="diff-block__title-row">
                        <h3 class="diff-block__title">System prompt</h3>
                        <button
                          type="button"
                          class="diff-pane__revert"
                          [disabled]="revertingId() === change.id"
                          uiTooltip="Put the Before prompt back; saved as a new change"
                          (click)="restore(change)"
                        >{{ revertingId() === change.id ? 'Restoring…' : 'Restore previous' }}</button>
                      </div>
                      <div class="diff-grid">
                        <div class="diff-pane diff-pane--old">
                          <div class="diff-pane__head"><span class="diff-pane__label">Before</span></div>
                          <pre class="diff-pane__content">{{ change.old_prompt || '(empty)' }}</pre>
                        </div>
                        <div class="diff-pane diff-pane--new">
                          <div class="diff-pane__head"><span class="diff-pane__label">After</span></div>
                          <pre class="diff-pane__content">{{ change.new_prompt || '(empty)' }}</pre>
                        </div>
                      </div>
                    </section>
                  </div>
                </li>
              }
            </ul>
          }
        </div>
      }
    </section>
  `,
  styleUrl: './personality-prompt-history.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityPromptHistoryComponent {
  private readonly personalityService = inject(PersonalityService);
  private readonly confirmation = inject(ConfirmationService);

  readonly personalityId = input.required<string>();
  readonly personalityName = input.required<string>();
  /** The persisted system prompt; a change means a new entry may exist. */
  readonly currentPrompt = input<string>('');

  /** Emitted after a restore succeeds, so the host can reload the prompt. */
  readonly restored = output<void>();

  readonly expanded = signal(false);
  readonly changes = signal<PersonalityPromptChange[]>([]);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly notice = signal<string | null>(null);
  readonly revertingId = signal<string | null>(null);

  constructor() {
    effect(() => {
      this.personalityId();
      this.currentPrompt();
      if (untracked(this.expanded)) untracked(() => this.load());
    });
  }

  toggle(): void {
    const next = !this.expanded();
    this.expanded.set(next);
    if (next) this.load();
  }

  load(): void {
    this.loading.set(true);
    this.error.set(null);
    this.personalityService.listPromptChanges(this.personalityId()).subscribe({
      next: changes => {
        this.changes.set(changes);
        this.loading.set(false);
      },
      error: err => {
        this.error.set(err instanceof Error ? err.message : 'Failed to load prompt changes');
        this.loading.set(false);
      },
    });
  }

  async restore(change: PersonalityPromptChange): Promise<void> {
    if (this.revertingId()) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Restore personality prompt?',
      message: `Restore ${this.personalityName()} to the prompt from before this change? The restore will be recorded as a new change.`,
      type: 'warning',
      confirmText: 'Restore',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.revertingId.set(change.id);
    this.notice.set(null);
    this.personalityService.revertPromptChange(change.personality_id, change.id).subscribe({
      next: () => {
        this.revertingId.set(null);
        this.notice.set('Previous prompt restored.');
        this.load();
        this.restored.emit();
      },
      error: err => {
        this.revertingId.set(null);
        this.error.set(err instanceof Error ? err.message : 'Failed to restore personality prompt');
      },
    });
  }
}

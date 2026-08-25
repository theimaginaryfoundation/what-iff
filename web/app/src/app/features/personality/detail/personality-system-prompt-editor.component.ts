import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  input,
  output,
  signal,
} from '@angular/core';

import {
  TEXT_LIMIT_HARD_MAX,
  TEXT_LIMIT_WARNING_THRESHOLD,
} from '../../../core/constants/text-limits.constants';

export interface SystemPromptValue {
  name: string;
  systemPrompt: string;
}

/**
 * Inline editor for a personality's name + system prompt. Handles
 * save/discard and exposes both the soft warning (≥20k chars) and hard
 * limit (>25k chars). Cancellation reverts to the latest input value.
 */
@Component({
  selector: 'app-personality-system-prompt-editor',
  standalone: true,
  template: `
    <section
      class="system-prompt-editor flex flex-col gap-3 rounded-2xl border border-(--color-border-default) bg-(--color-surface-card) p-4"
      aria-label="System prompt editor"
    >
      <header class="flex items-center justify-between gap-2">
        <h2 class="text-base font-semibold text-(--color-text-primary)">System prompt</h2>
        @if (!isEditing()) {
          <button
            type="button"
            class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
            (click)="startEdit()"
          >Edit</button>
        }
      </header>

      @if (isEditing()) {
        <label class="flex flex-col gap-1 text-sm">
          <span class="font-medium text-(--color-text-primary)">Name</span>
          <input
            type="text"
            class="rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
            [value]="draft().name"
            (input)="setName($any($event.target).value)"
          />
        </label>

        <label class="flex flex-col gap-1 text-sm">
          <span class="font-medium text-(--color-text-primary)">Prompt</span>
          <textarea
            rows="10"
            class="rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 font-mono text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
            [value]="draft().systemPrompt"
            (input)="setSystemPrompt($any($event.target).value)"
          ></textarea>
          <div class="flex items-center justify-between text-xs">
            <span
              class="text-(--color-text-secondary)"
              [class.text-amber-500]="isNearLimit()"
              [class.text-red-500]="isOverLimit()"
            >{{ characterCountLabel() }} / {{ hardLimitLabel }} characters</span>
            @if (errorMessage()) {
              <span class="text-red-500" role="alert">{{ errorMessage() }}</span>
            }
          </div>
        </label>

        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated) disabled:opacity-60"
            [disabled]="isSaving()"
            (click)="cancelEdit()"
          >Cancel</button>
          <button
            type="button"
            class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"
            [disabled]="!canSave() || isSaving()"
            (click)="onSave()"
          >{{ isSaving() ? 'Saving…' : 'Save' }}</button>
        </div>
      } @else {
        <div class="flex flex-col gap-2">
          <p class="text-sm font-medium text-(--color-text-primary)">{{ value().name }}</p>
          <pre
            class="max-h-72 overflow-y-auto whitespace-pre-wrap rounded-lg bg-(--color-surface-elevated) p-3 font-mono text-xs text-(--color-text-secondary)"
          >{{ value().systemPrompt || 'No system prompt set.' }}</pre>
        </div>
      }
    </section>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [],
})
export class PersonalitySystemPromptEditorComponent {
  readonly value = input.required<SystemPromptValue>();
  readonly isSaving = input<boolean>(false);

  readonly save = output<SystemPromptValue>();
  readonly cancel = output<void>();

  readonly hardLimit = TEXT_LIMIT_HARD_MAX;
  readonly hardLimitLabel = TEXT_LIMIT_HARD_MAX.toLocaleString();

  readonly isEditing = signal(false);
  readonly draft = signal<SystemPromptValue>({ name: '', systemPrompt: '' });
  readonly errorMessage = signal<string | null>(null);

  readonly characterCount = computed(() => this.draft().systemPrompt.length);
  readonly characterCountLabel = computed(() => this.characterCount().toLocaleString());
  readonly isNearLimit = computed(() => this.characterCount() >= TEXT_LIMIT_WARNING_THRESHOLD);
  readonly isOverLimit = computed(() => this.characterCount() > TEXT_LIMIT_HARD_MAX);
  readonly canSave = computed(() => {
    const draft = this.draft();
    if (!draft.name.trim() || !draft.systemPrompt.trim()) return false;
    if (this.isOverLimit()) return false;
    return true;
  });

  constructor() {
    effect(() => {
      if (!this.isEditing()) {
        this.draft.set(this.value());
      }
    });
  }

  startEdit(): void {
    this.draft.set(this.value());
    this.errorMessage.set(null);
    this.isEditing.set(true);
  }

  cancelEdit(): void {
    this.draft.set(this.value());
    this.errorMessage.set(null);
    this.isEditing.set(false);
    this.cancel.emit();
  }

  setName(value: string): void {
    this.draft.update(draft => ({ ...draft, name: value }));
  }

  setSystemPrompt(value: string): void {
    this.draft.update(draft => ({ ...draft, systemPrompt: value }));
  }

  onSave(): void {
    const draft = this.draft();
    if (!draft.name.trim() || !draft.systemPrompt.trim()) {
      this.errorMessage.set('Name and prompt are required.');
      return;
    }
    if (this.isOverLimit()) {
      this.errorMessage.set(`Prompt cannot exceed ${this.hardLimit.toLocaleString()} characters.`);
      return;
    }
    this.errorMessage.set(null);
    this.save.emit({ name: draft.name.trim(), systemPrompt: draft.systemPrompt.trim() });
    this.isEditing.set(false);
  }
}

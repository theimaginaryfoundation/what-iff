import { ChangeDetectionStrategy, Component, input, linkedSignal, output, signal } from '@angular/core';

/**
 * A list of short strings edited as chips: type and press Enter (or comma) to add, × to remove,
 * Backspace in an empty box removes the last one. Used for `list` parameter values and for select
 * options in the parameter builder (ADR 0x044).
 */
@Component({
  selector: 'app-chip-list-input',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div
      class="flex min-h-10 flex-wrap items-center gap-1.5 rounded-xl border bg-surface-base px-2 py-1.5"
      [class.border-border-base]="!invalid()"
      [class.border-amber-500]="invalid()">
      @for (item of current(); track $index) {
        <span class="inline-flex max-w-full items-center gap-1 rounded-full bg-accent-primary/15 px-2 py-0.5 text-xs text-accent-primary">
          <span class="truncate">{{ item }}</span>
          <button
            type="button"
            class="shrink-0 text-accent-primary"
            [attr.aria-label]="'Remove ' + item"
            [disabled]="disabled()"
            (click)="remove($index)">
            ×
          </button>
        </span>
      }
      <input
        class="min-w-32 flex-1 border-0 bg-transparent px-1 py-0.5 text-sm text-text-primary focus:ring-0"
        [attr.id]="inputId()"
        [attr.aria-label]="ariaLabel()"
        [attr.aria-describedby]="describedBy()"
        [placeholder]="placeholder()"
        [disabled]="disabled() || atLimit()"
        [value]="draft()"
        (input)="draft.set($any($event.target).value)"
        (keydown)="onKeydown($event)"
        (blur)="commit()" />
    </div>
  `,
})
export class ChipListInputComponent {
  readonly items = input<string[]>([]);
  readonly placeholder = input('Type and press Enter');
  readonly ariaLabel = input<string | null>(null);
  readonly inputId = input<string | null>(null);
  readonly describedBy = input<string | null>(null);
  readonly disabled = input(false);
  readonly invalid = input(false);
  readonly max = input(50);

  readonly itemsChange = output<string[]>();

  readonly draft = signal('');
  /**
   * The items as last edited here, reset whenever the parent passes new ones. Edits build on this
   * rather than on the input, which only catches up at the next change detection, so two quick
   * additions don't overwrite each other.
   */
  readonly current = linkedSignal(() => this.items());

  atLimit(): boolean {
    return this.current().length >= this.max();
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      this.commit();
      return;
    }
    if (event.key === 'Backspace' && this.draft() === '' && this.current().length > 0) {
      event.preventDefault();
      this.remove(this.current().length - 1);
    }
  }

  /** Adds the typed text as a chip, unless it is blank or already listed. */
  commit(): void {
    const value = this.draft().trim();
    this.draft.set('');
    if (!value || this.current().includes(value) || this.atLimit()) return;
    this.emit([...this.current(), value]);
  }

  remove(index: number): void {
    this.emit(this.current().filter((_, i) => i !== index));
  }

  private emit(items: string[]): void {
    this.current.set(items);
    this.itemsChange.emit(items);
  }
}

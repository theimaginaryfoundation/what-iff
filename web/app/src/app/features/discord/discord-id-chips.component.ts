import { ChangeDetectionStrategy, Component, input, output, signal } from '@angular/core';

/**
 * A list of Discord user ids shown as removable chips, like the Edit tags field in
 * the thread manager. Ids are added with Enter, a comma or a space, or by pasting
 * a list; whatever is typed when the field loses focus is added too.
 */
@Component({
  selector: 'app-discord-id-chips',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="chips__field" [class.chips__field--error]="error()" (click)="entry.focus()">
      @for (id of ids(); track id) {
        <span class="chips__chip">
          {{ id }}
          <button type="button" class="chips__remove" [attr.aria-label]="'Remove ' + id" (click)="remove(id); $event.stopPropagation()">
            ×
          </button>
        </span>
      }
      <input
        #entry
        type="text"
        inputmode="numeric"
        autocomplete="off"
        class="chips__input"
        [attr.placeholder]="ids().length === 0 ? placeholder() : 'Add another…'"
        [attr.aria-label]="label()"
        (input)="onInput(entry)"
        (keydown)="onKeydown($event, entry)"
        (paste)="onPaste($event, entry)"
        (blur)="commit(entry)"
      />
    </div>
    @if (error(); as message) {
      <p class="chips__error" role="alert">{{ message }}</p>
    }
  `,
  styles: [
    `
      :host {
        display: block;
      }

      .chips__field {
        align-items: center;
        background: var(--color-surface-muted);
        border: 1px solid var(--color-border-base);
        border-radius: 0.5rem;
        cursor: text;
        display: flex;
        flex-wrap: wrap;
        gap: 0.375rem;
        min-height: 2.5rem;
        padding: 0.375rem 0.5rem;
      }

      .chips__field:focus-within {
        border-color: var(--color-accent);
      }

      .chips__field--error {
        border-color: var(--color-danger);
      }

      .chips__chip {
        align-items: center;
        background: color-mix(in srgb, var(--color-accent) 14%, transparent);
        border: 1px solid var(--color-accent);
        border-radius: 999px;
        color: var(--color-accent);
        display: inline-flex;
        font-size: 0.75rem;
        font-variant-numeric: tabular-nums;
        font-weight: 600;
        gap: 0.125rem;
        max-width: 100%;
        padding: 0.125rem 0.25rem 0.125rem 0.5rem;
      }

      .chips__remove {
        background: transparent;
        border: 0;
        color: inherit;
        cursor: pointer;
        font-size: 0.875rem;
        line-height: 1;
        opacity: 0.75;
        padding: 0 0.125rem;
      }

      .chips__remove:hover {
        opacity: 1;
      }

      .chips__input {
        background: transparent;
        border: 0;
        color: var(--color-text-primary);
        flex: 1;
        font-size: 0.8125rem;
        min-width: 8rem;
        outline: 0;
        padding: 0.25rem 0.125rem;
      }

      .chips__error {
        color: var(--color-danger);
        font-size: 0.75rem;
        margin: 0.25rem 0 0;
      }
    `,
  ],
})
export class DiscordIdChipsComponent {
  readonly ids = input<readonly string[]>([]);
  readonly label = input('Discord user ids');
  readonly placeholder = input('Paste or type a user id…');
  readonly idsChange = output<string[]>();

  readonly error = signal<string | null>(null);

  onInput(entry: HTMLInputElement): void {
    // A separator in the text (typed, or inside a pasted list) ends an id.
    if (/[\s,]/.test(entry.value)) this.commit(entry);
    else this.error.set(null);
  }

  /**
   * Browsers flatten new lines when pasting into a single-line field, which would
   * glue a one-id-per-line list together, so read the clipboard text ourselves.
   */
  onPaste(event: ClipboardEvent, entry: HTMLInputElement): void {
    const text = event.clipboardData?.getData('text');
    if (!text) return;
    event.preventDefault();
    this.commitText(`${entry.value} ${text}`, entry);
  }

  onKeydown(event: KeyboardEvent, entry: HTMLInputElement): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      this.commit(entry);
    } else if (event.key === 'Backspace' && entry.value === '' && this.ids().length > 0) {
      this.idsChange.emit(this.ids().slice(0, -1));
    }
  }

  /** Adds every valid id in the field; anything that is not an id stays there with a note. */
  commit(entry: HTMLInputElement): void {
    this.commitText(entry.value, entry);
  }

  private commitText(text: string, entry: HTMLInputElement): void {
    const parts = text.split(/[\s,]+/).filter(Boolean);
    if (parts.length === 0) {
      entry.value = '';
      this.error.set(null);
      return;
    }
    const next = [...this.ids()];
    const rejected: string[] = [];
    for (const part of parts) {
      if (!/^\d+$/.test(part)) rejected.push(part);
      else if (!next.includes(part)) next.push(part);
    }
    if (next.length !== this.ids().length) this.idsChange.emit(next);
    entry.value = rejected.join(' ');
    this.error.set(rejected.length ? 'Discord user ids are numbers only (Copy User ID, with Developer Mode on).' : null);
  }

  remove(id: string): void {
    this.idsChange.emit(this.ids().filter(existing => existing !== id));
  }
}

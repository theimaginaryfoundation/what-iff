import { ChangeDetectionStrategy, Component, input, output, signal } from '@angular/core';

import { fullNpub, normalizePubkey, shortNpub } from './npub';

/**
 * A list of Buzz public keys shown as removable chips (shortened npubs, the full one on hover),
 * the Buzz twin of the Discord user-id chips. Keys are typed or pasted as npub or hex and kept as
 * hex, as the API stores them; they are added with Enter, a comma or a space, by pasting a list,
 * or when the field loses focus.
 */
@Component({
  selector: 'app-buzz-pubkey-chips',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="chips__field" [class.chips__field--error]="error()" (click)="entry.focus()">
      @for (key of keys(); track key) {
        <span class="chips__chip" [attr.title]="npub(key)">
          {{ short(key) }}
          <button
            type="button"
            class="chips__remove"
            [attr.aria-label]="'Remove ' + short(key)"
            (click)="remove(key); $event.stopPropagation()"
          >
            ×
          </button>
        </span>
      }
      <input
        #entry
        type="text"
        autocomplete="off"
        spellcheck="false"
        class="chips__input"
        [attr.placeholder]="keys().length === 0 ? placeholder() : 'Add another…'"
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
        font-family: ui-monospace, monospace;
        font-size: 0.75rem;
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
        min-width: 10rem;
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
export class BuzzPubkeyChipsComponent {
  /** Hex pubkeys. */
  readonly keys = input<readonly string[]>([]);
  readonly label = input('Buzz public keys');
  readonly placeholder = input('Paste an npub…');
  readonly keysChange = output<string[]>();

  readonly error = signal<string | null>(null);
  readonly short = shortNpub;
  readonly npub = fullNpub;

  onInput(entry: HTMLInputElement): void {
    if (/[\s,]/.test(entry.value)) this.commit(entry);
    else this.error.set(null);
  }

  /** Reads the clipboard text ourselves, so a one-key-per-line list is not glued together. */
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
    } else if (event.key === 'Backspace' && entry.value === '' && this.keys().length > 0) {
      this.keysChange.emit(this.keys().slice(0, -1));
    }
  }

  /** Adds every valid key in the field; anything that is not a key stays there with a note. */
  commit(entry: HTMLInputElement): void {
    this.commitText(entry.value, entry);
  }

  private commitText(text: string, entry: HTMLInputElement): void {
    const parts = text.split(/[\s,]+/).filter(Boolean);
    const next = [...this.keys()];
    const rejected: string[] = [];
    for (const part of parts) {
      const key = normalizePubkey(part);
      if (!key) rejected.push(part);
      else if (!next.includes(key)) next.push(key);
    }
    if (next.length !== this.keys().length) this.keysChange.emit(next);
    entry.value = rejected.join(' ');
    this.error.set(rejected.length ? 'Buzz public keys are an npub1… or 64 hex characters.' : null);
  }

  remove(key: string): void {
    this.keysChange.emit(this.keys().filter(existing => existing !== key));
  }
}

import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal } from '@angular/core';

import { EmojiSuggestion } from '../../helpers/emoji-shortcode.helpers';

/**
 * Slack-style emoji autocomplete popup. Renders the suggestions the composer
 * found for an in-progress `:shortcode` and lets the user pick one with the
 * mouse or keyboard. It never mutates the draft itself — the parent composer
 * owns insertion — so this stays a thin, presentational sibling of
 * {@link SlashMenuComponent}.
 */
@Component({
  selector: 'app-emoji-autocomplete-menu',
  standalone: true,
  imports: [],
  template: `
    @if (suggestions().length) {
      <div
        class="emoji-menu"
        role="listbox"
        [attr.aria-activedescendant]="activeId()"
        aria-label="Emoji suggestions"
      >
        @for (suggestion of suggestions(); track suggestion.id; let index = $index) {
          <button
            type="button"
            role="option"
            class="emoji-menu__row"
            [id]="rowId(suggestion)"
            [class.emoji-menu__row--active]="index === selectedIndex()"
            [attr.aria-selected]="index === selectedIndex()"
            (mouseenter)="selectedIndex.set(index)"
            (mousedown)="$event.preventDefault()"
            (click)="select(suggestion)"
          >
            <span class="emoji-menu__glyph" aria-hidden="true">{{ suggestion.native }}</span>
            <span class="emoji-menu__colons">{{ suggestion.colons }}</span>
          </button>
        }
      </div>
    }
  `,
  styles: [`
    .emoji-menu {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.75rem;
      box-shadow: 0 18px 45px rgb(0 0 0 / 0.18);
      display: grid;
      gap: 0.125rem;
      max-height: 16rem;
      overflow-y: auto;
      padding: 0.375rem;
    }

    .emoji-menu__row {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.5rem;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      gap: 0.5rem;
      padding: 0.375rem 0.5rem;
      text-align: left;
      width: 100%;
    }

    .emoji-menu__row--active,
    .emoji-menu__row:hover {
      background: var(--color-surface-muted);
    }

    .emoji-menu__glyph {
      font-size: 1.125rem;
      line-height: 1;
      width: 1.5rem;
      text-align: center;
    }

    .emoji-menu__colons {
      color: var(--color-text-secondary);
      font-size: 0.8125rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class EmojiAutocompleteMenuComponent {
  readonly suggestions = input.required<readonly EmojiSuggestion[]>();
  readonly selected = output<EmojiSuggestion>();
  readonly closed = output<void>();
  readonly selectedIndex = signal(0);
  readonly activeId = computed(() => {
    const current = this.suggestions()[this.selectedIndex()];
    return current ? this.rowId(current) : null;
  });

  constructor() {
    // Reset the highlight whenever the suggestion set changes (new query).
    effect(() => {
      this.suggestions();
      this.selectedIndex.set(0);
    });
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      this.closed.emit();
      return;
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.move(event.key === 'ArrowDown' ? 1 : -1);
    }
  }

  select(suggestion: EmojiSuggestion): void {
    this.selected.emit(suggestion);
  }

  /** Accept the highlighted row (invoked from the parent textarea's keydown). */
  selectHighlighted(): void {
    const suggestion = this.suggestions()[this.selectedIndex()];
    if (suggestion) {
      this.select(suggestion);
    }
  }

  rowId(suggestion: EmojiSuggestion): string {
    return `emoji-suggestion-${suggestion.id}`;
  }

  private move(delta: number): void {
    const count = this.suggestions().length;
    if (!count) {
      return;
    }
    this.selectedIndex.set((this.selectedIndex() + delta + count) % count);
  }
}

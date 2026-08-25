import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
  viewChildren,
} from '@angular/core';
import { AsyncPipe } from '@angular/common';
import { Router } from '@angular/router';

import { Personality } from '../../../core/models/personality.model';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { AvatarComponent } from '../../../shared/ui/avatar/avatar.component';
import { personalityCoverUrl } from '../helpers/cover-image.helpers';
import { shortName } from '../helpers/short-name.helpers';
import { usageBadge } from '../helpers/personality-vm.helpers';
import { PersonaCoverComponent } from './persona-cover.component';
import { PersonaAccentScopeComponent } from './persona-accent-scope.component';

interface PersonaPickerRow {
  id: string;
  name: string;
  shortName: string;
  usage: string;
  coverUrl: string | null;
  source: Personality;
}

/**
 * Reusable persona picker shell. Implements WAI-ARIA combobox + listbox semantics
 * with full keyboard navigation. Designed to be embedded inside `ui-modal` (desktop)
 * or `ui-sheet` (mobile) — the component itself is layout-agnostic.
 */
@Component({
  selector: 'persona-picker',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe, AvatarComponent, PersonaCoverComponent, PersonaAccentScopeComponent],
  template: `
    <div class="persona-picker flex flex-col gap-3 p-4" role="dialog" aria-modal="true" [attr.aria-label]="ariaLabel()">
      <label class="flex flex-col gap-1">
        <span class="persona-picker__search-label text-xs font-medium text-(--color-text-secondary)" [attr.id]="searchLabelId">Search personalities</span>
        @if (appearance() === 'filter-list') {
          <div class="app-sidebar__personality-picker">
            <input
              #searchInput
              type="text"
              class="app-sidebar__personality-search"
              role="combobox"
              [attr.aria-expanded]="dropdownOpen()"
              aria-autocomplete="list"
              [attr.aria-controls]="listId"
              [attr.aria-activedescendant]="activeId()"
              [attr.aria-labelledby]="searchLabelId"
              autocomplete="off"
              placeholder="Filter by personality…"
              [value]="query()"
              (input)="onQueryInput($any($event.target).value)"
              (focus)="openDropdown()"
              (blur)="closeDropdown()"
              (keydown)="onKeydown($event)"
            />
            @if (dropdownOpen()) {
              <div
                class="app-sidebar__personality-dropdown"
                role="listbox"
                [attr.id]="listId"
                [attr.aria-activedescendant]="activeId()"
              >
                @for (row of rows(); track row.id; let i = $index) {
                  <persona-accent-scope [personality]="row.source">
                    <button
                      #row
                      type="button"
                      role="option"
                      class="app-sidebar__personality-option"
                      [class.app-sidebar__personality-option--active]="i === highlightedIndex()"
                      [attr.id]="optionId(i)"
                      [attr.aria-selected]="i === highlightedIndex()"
                      (mousemove)="setHighlight(i)"
                      (mousedown)="selectFromDropdown(row, $event)"
                    >
                      <ui-avatar
                        class="app-sidebar__personality-option-avatar"
                        [name]="row.name"
                        [size]="'xxs'"
                        [imageUrl]="row.coverUrl ? ((row.coverUrl | authImage | async) ?? null) : null"
                        [accentColor]="'var(--persona-accent)'"
                        [thumbnailCircle]="row.source.thumbnail_circle"
                      />
                      <span class="app-sidebar__personality-option-label">{{ row.name }}</span>
                    </button>
                  </persona-accent-scope>
                } @empty {
                  <p class="app-sidebar__personality-empty" role="status">No personalities found</p>
                }
              </div>
            }
          </div>
        } @else {
          <input
            #searchInput
            type="text"
            class="persona-picker__search rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
            role="combobox"
            aria-expanded="true"
            aria-autocomplete="list"
            [attr.aria-controls]="listId"
            [attr.aria-activedescendant]="activeId()"
            [attr.aria-labelledby]="searchLabelId"
            autocomplete="off"
            placeholder="Filter by personality…"
            [value]="query()"
            (input)="onQueryInput($any($event.target).value)"
            (keydown)="onKeydown($event)"
          />
        }
      </label>

      @if (appearance() !== 'filter-list') {
        <ul
          #list
          class="persona-picker__list flex max-h-[60vh] flex-col gap-1 overflow-y-auto"
          role="listbox"
          [attr.id]="listId"
          [attr.aria-activedescendant]="activeId()"
        >
          @for (row of rows(); track row.id; let i = $index) {
            <li
              #row
              role="option"
              [attr.id]="optionId(i)"
              [attr.aria-selected]="i === highlightedIndex()"
              class="persona-picker__row flex cursor-pointer items-center gap-3 rounded-lg p-2 outline-none transition-colors hover:bg-(--color-surface-elevated) focus:bg-(--color-surface-elevated)"
              [class.persona-picker__row--active]="i === highlightedIndex()"
              (mousemove)="setHighlight(i)"
              (click)="emitSelect(row)"
            >
              <persona-accent-scope [personality]="row.source">
                <persona-cover
                  [name]="row.name"
                  [imageUrl]="row.coverUrl"
                  [thumbnailCircle]="row.source.thumbnail_circle"
                  size="sm"
                />
              </persona-accent-scope>
              <div class="flex min-w-0 flex-1 flex-col">
                <span class="truncate text-sm font-medium text-(--color-text-primary)">{{ row.name }}</span>
                <span class="truncate text-xs text-(--color-text-secondary)">{{ row.shortName }} · {{ row.usage }}</span>
              </div>
            </li>
          } @empty {
            <li class="persona-picker__empty rounded-lg p-4 text-center text-sm text-(--color-text-secondary)" role="status">
              No personalities match
              @if (query()) { "{{ query() }}" }
              yet.
            </li>
          }
        </ul>
      }

      @if (appearance() !== 'filter-list') {
        <div class="persona-picker__footer flex items-center justify-between gap-2 border-t border-(--color-border-default) pt-3">
          <button
            type="button"
            class="text-sm font-medium text-(--color-accent) hover:underline focus:outline-none focus:ring-2 focus:ring-(--color-accent)"
            (click)="onCreateNew()"
          >
            + Create new personality
          </button>
          @if (showCancel()) {
            <button
              type="button"
              class="text-sm text-(--color-text-secondary) hover:text-(--color-text-primary) focus:outline-none focus:ring-2 focus:ring-(--color-accent)"
              (click)="cancel.emit()"
            >Cancel</button>
          }
        </div>
      }
    </div>
  `,
  styles: [
    `
      .persona-picker__row--active {
        background: var(--color-surface-elevated, var(--color-surface-base));
      }

      .persona-picker:has(.app-sidebar__personality-picker) {
        padding: 12px;
      }

      .persona-picker:has(.app-sidebar__personality-picker) .persona-picker__search-label {
        display: none;
      }

      .app-sidebar__personality-picker {
        position: relative;
      }

      .app-sidebar__personality-search {
        width: 100%;
        padding: 5px 8px;
        border: 1px solid var(--border);
        border-radius: 5px;
        background: var(--bg-input);
        color: var(--text-primary);
        font-size: 11px;
        line-height: 17px;
        box-sizing: border-box;
        outline: none;
      }

      .app-sidebar__personality-search:focus {
        border-color: var(--accent-mode, var(--accent));
      }

      .app-sidebar__personality-dropdown {
        position: absolute;
        top: calc(100% + 2px);
        left: 0;
        right: 0;
        z-index: 300;
        border: 1px solid var(--border);
        border-radius: 6px;
        background: var(--bg-elevated);
        box-shadow: 0 6px 20px rgba(0, 0, 0, 0.18);
        max-height: min(60vh, 280px);
        overflow: auto;
      }

      .app-sidebar__personality-option {
        width: 100%;
        display: flex;
        align-items: center;
        gap: 6px;
        border: 0;
        border-bottom: 1px solid var(--border);
        background: transparent;
        padding: 5px 8px;
        color: var(--persona-accent, var(--text-primary));
        font-size: 11px;
        min-height: 27px;
        text-align: left;
        cursor: pointer;
      }

      .app-sidebar__personality-option-avatar {
        flex: 0 0 auto;
      }

      .app-sidebar__personality-option-label {
        flex: 1;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-weight: 600;
      }

      .app-sidebar__personality-option:last-child {
        border-bottom: 0;
      }

      .app-sidebar__personality-option:hover,
      .app-sidebar__personality-option--active {
        background: color-mix(in srgb, var(--persona-accent, var(--accent-mode, var(--accent))) 14%, transparent);
        color: var(--persona-accent, var(--accent-mode, var(--accent)));
      }

      .app-sidebar__personality-empty {
        margin: 0;
        color: var(--text-muted);
        font-size: 11px;
        padding: 8px 10px;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonaPickerComponent {
  private readonly router = inject(Router);
  private readonly imageGallery = inject(ImageGalleryService);

  readonly personalities = input.required<readonly Personality[]>();
  readonly ariaLabel = input<string>('Pick a personality');
  readonly showCancel = input<boolean>(true);
  readonly appearance = input<'rich' | 'filter-list'>('rich');

  readonly select = output<Personality>();
  readonly cancel = output<void>();
  readonly create = output<void>();

  readonly searchLabelId = `persona-picker-search-${randomId()}`;
  readonly listId = `persona-picker-list-${randomId()}`;
  private readonly idPrefix = `persona-picker-row-${randomId()}-`;

  readonly query = signal('');
  readonly highlightedIndex = signal(0);
  readonly dropdownOpen = signal(false);

  readonly searchInput = viewChild<ElementRef<HTMLInputElement>>('searchInput');
  readonly rowEls = viewChildren<ElementRef<HTMLLIElement>>('row');

  readonly rows = computed<PersonaPickerRow[]>(() => {
    const q = this.query().trim().toLowerCase();
    const all = this.personalities();
    const mapped = all.map(p => ({
      id: p.id,
      name: p.name,
      shortName: shortName(p.name),
      usage: usageBadge(p.stats),
      coverUrl: personalityCoverUrl(
        p,
        [],
        this.imageGallery.getImageUrl.bind(this.imageGallery),
      ),
      source: p,
    }));
    if (!q) return mapped;
    return mapped.filter(row => row.name.toLowerCase().includes(q));
  });

  readonly activeId = computed(() => {
    const rows = this.rows();
    if (!rows.length) return null;
    const idx = Math.min(Math.max(this.highlightedIndex(), 0), rows.length - 1);
    return this.optionId(idx);
  });

  constructor() {
    effect(() => {
      const len = this.rows().length;
      if (len === 0) {
        this.highlightedIndex.set(0);
        return;
      }
      const current = this.highlightedIndex();
      if (current >= len) this.highlightedIndex.set(len - 1);
      if (current < 0) this.highlightedIndex.set(0);
    });

    effect(() => {
      const input = this.searchInput()?.nativeElement;
      if (input) setTimeout(() => input.focus(), 0);
    });
  }

  optionId(index: number): string {
    return `${this.idPrefix}${index}`;
  }

  onQueryInput(value: string): void {
    this.query.set(value);
    this.highlightedIndex.set(0);
    if (this.appearance() === 'filter-list') {
      this.dropdownOpen.set(true);
    }
  }

  setHighlight(index: number): void {
    if (index !== this.highlightedIndex()) this.highlightedIndex.set(index);
  }

  onKeydown(event: KeyboardEvent): void {
    const len = this.rows().length;
    switch (event.key) {
      case 'ArrowDown': {
        event.preventDefault();
        if (!len) return;
        this.highlightedIndex.update(i => Math.min(i + 1, len - 1));
        this.scrollHighlightedIntoView();
        return;
      }
      case 'ArrowUp': {
        event.preventDefault();
        if (!len) return;
        this.highlightedIndex.update(i => Math.max(i - 1, 0));
        this.scrollHighlightedIntoView();
        return;
      }
      case 'Home': {
        event.preventDefault();
        this.highlightedIndex.set(0);
        this.scrollHighlightedIntoView();
        return;
      }
      case 'End': {
        event.preventDefault();
        if (!len) return;
        this.highlightedIndex.set(len - 1);
        this.scrollHighlightedIntoView();
        return;
      }
      case 'Enter': {
        event.preventDefault();
        const rows = this.rows();
        const target = rows[this.highlightedIndex()];
        if (target) this.emitSelect(target);
        return;
      }
      case 'Escape': {
        event.preventDefault();
        this.cancel.emit();
        return;
      }
    }
  }

  emitSelect(row: PersonaPickerRow): void {
    this.select.emit(row.source);
  }

  openDropdown(): void {
    this.dropdownOpen.set(true);
  }

  closeDropdown(): void {
    setTimeout(() => this.dropdownOpen.set(false), 100);
  }

  selectFromDropdown(row: PersonaPickerRow, event: MouseEvent): void {
    event.preventDefault();
    this.emitSelect(row);
  }

  onCreateNew(): void {
    this.create.emit();
    this.router.navigateByUrl('/personality?create=1');
  }

  private scrollHighlightedIntoView(): void {
    const rows = this.rowEls();
    const target = rows[this.highlightedIndex()]?.nativeElement;
    if (target?.scrollIntoView) {
      target.scrollIntoView({ block: 'nearest' });
    }
  }
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}

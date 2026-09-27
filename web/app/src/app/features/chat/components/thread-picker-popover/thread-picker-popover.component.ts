import { AsyncPipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  OnInit,
  afterNextRender,
  computed,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { Personality } from '../../../../core/models/personality.model';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { thumbnailCircleToImageStyle } from '../../../../shared/ui/avatar/avatar-thumbnail.helpers';
import { personalityCoverUrl } from '../../../personality/helpers/cover-image.helpers';
import { personalityAccent } from '../../../personality/helpers/personality-vm.helpers';
import { avatarInitials, threadAgeLabel } from '../../helpers/thread-picker.helpers';

/** Rows rendered at once; search still covers every loaded thread. */
export const THREAD_PICKER_ROW_LIMIT = 50;

/**
 * Multi-select popover for attaching other threads to the next message.
 *
 * The selection itself is owned by the caller (`selected` in, `toggled` out). The popover
 * snapshots `selected` when it is created (i.e. when it opens), so Cancel/Escape can hand the
 * caller the original selection back via `cancelled`. Focus moves to the search field on open;
 * the caller decides where focus goes on close.
 */
@Component({
  selector: 'app-thread-picker-popover',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe],
  host: {
    '(keydown.escape)': 'onEscape($event)',
  },
  template: `
    <div
      class="composer__thread-popover"
      role="dialog"
      aria-label="Choose threads"
      aria-describedby="thread-picker-help"
    >
      <div class="composer__thread-header">
        <span class="composer__thread-count" aria-live="polite">{{ selected().length }} selected</span>
        <button type="button" class="composer__thread-done" (click)="done.emit()">Done</button>
        <button
          type="button"
          class="composer__thread-cancel"
          aria-label="Cancel and undo changes"
          (click)="cancel()"
        >✕</button>
      </div>
      <p id="thread-picker-help" class="composer__thread-help">
        Your personality can read attached threads, e.g. to recap what you decided there
      </p>
      <div class="composer__thread-toolbar">
        <label class="sr-only" for="composer-thread-filter">Search threads</label>
        <input
          #searchInput
          id="composer-thread-filter"
          type="search"
          class="composer__thread-search"
          placeholder="Search…"
          [value]="filter()"
          [disabled]="disabled()"
          (input)="onFilterInput($event)"
        />
        <button
          type="button"
          class="composer__thread-starred-filter"
          [class.composer__thread-starred-filter--on]="starredOnly()"
          [attr.aria-pressed]="starredOnly()"
          (click)="starredOnly.set(!starredOnly())"
        >★ Starred Only</button>
      </div>
      @if (rows().length === 0) {
        <p class="composer__thread-empty">No threads match.</p>
      } @else {
        <ul class="composer__thread-list" aria-label="Available threads">
          @for (row of rows(); track row.thread.id) {
            <li>
              <button
                type="button"
                class="composer__thread-row"
                [class.composer__thread-row--selected]="row.selected"
                [disabled]="disabled()"
                [attr.aria-pressed]="row.selected"
                (click)="pick(row.thread)"
              >
                <span
                  class="composer__thread-check"
                  [class.composer__thread-check--on]="row.selected"
                  aria-hidden="true"
                >{{ row.selected ? '✓' : '' }}</span>
                <span class="composer__thread-avatar" [style.background]="row.color" aria-hidden="true">
                  @if (row.coverUrl; as coverUrl) {
                    @if (coverUrl | authImage | async; as avatarSrc) {
                      <img
                        [src]="avatarSrc"
                        alt=""
                        [style.object-position]="row.thumbnailStyle?.objectPosition"
                        [style.transform]="row.thumbnailStyle?.transform"
                      />
                    } @else {
                      {{ row.initials }}
                    }
                  } @else {
                    {{ row.initials }}
                  }
                </span>
                <span class="composer__thread-name">{{ row.thread.name }}</span>
                @if (row.age) {
                  <span class="composer__thread-age">{{ row.age }}</span>
                }
                @if (row.thread.is_favorite) {
                  <span class="composer__thread-star" role="img" aria-label="Starred">★</span>
                }
              </button>
            </li>
          }
        </ul>
        @if (matchCount() > rows().length) {
          <p class="composer__thread-limit">
            Showing {{ rows().length }} of {{ matchCount() }} — search to find more
          </p>
        }
      }
    </div>
  `,
  styles: [`
    :host {
      display: contents;
    }

    .composer__thread-popover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.75rem;
      bottom: calc(100% + 0.5rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      display: flex;
      flex-direction: column;
      left: 0;
      max-height: min(24rem, 55vh);
      overflow: hidden;
      position: absolute;
      right: 0;
      z-index: 58;
    }

    .composer__thread-header {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 14%, var(--color-surface-base));
      border-bottom: 1px solid var(--color-border-base);
      display: flex;
      gap: 0.5rem;
      padding: 0.625rem 0.75rem;
    }

    .composer__thread-count {
      color: var(--color-accent);
      font-size: 0.875rem;
      font-weight: 700;
      margin-right: auto;
    }

    .composer__thread-done,
    .composer__thread-cancel {
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      cursor: pointer;
      font-size: 0.8125rem;
      min-height: 2rem;
      padding: 0.25rem 0.75rem;
    }

    .composer__thread-done {
      background: var(--color-accent);
      border-color: var(--color-accent);
      color: white;
      font-weight: 600;
    }

    .composer__thread-cancel {
      background: transparent;
      color: var(--color-text-secondary);
      padding-inline: 0.5rem;
    }

    .composer__thread-help {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
      padding: 0.375rem 0.75rem 0;
    }

    .composer__thread-toolbar {
      border-bottom: 1px solid var(--color-border-base);
      display: flex;
      gap: 0.5rem;
      padding: 0.5rem 0.75rem;
    }

    .composer__thread-search {
      background: var(--color-surface-input, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      color: var(--color-text-primary);
      flex: 1 1 auto;
      font-size: 0.8125rem;
      min-width: 0;
      padding: 0.5rem 0.75rem;
    }

    .composer__thread-starred-filter {
      background: transparent;
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      color: var(--color-text-secondary);
      cursor: pointer;
      flex: 0 0 auto;
      font-size: 0.8125rem;
      padding: 0.5rem 0.75rem;
      white-space: nowrap;
    }

    .composer__thread-starred-filter--on {
      background: var(--color-accent);
      border-color: var(--color-accent);
      color: white;
      font-weight: 700;
    }

    .composer__thread-list {
      list-style: none;
      margin: 0;
      overflow-y: auto;
      padding: 0.25rem 0;
    }

    .composer__thread-empty,
    .composer__thread-limit {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
      padding: 0.75rem;
    }

    .composer__thread-limit {
      border-top: 1px solid var(--color-border-base);
      padding-block: 0.5rem;
    }

    .composer__thread-row {
      align-items: center;
      background: transparent;
      border: 0;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      font-size: 0.875rem;
      gap: 0.75rem;
      padding: 0.5rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .composer__thread-row:hover:not(:disabled),
    .composer__thread-row--selected {
      background: color-mix(in srgb, var(--color-accent) 10%, transparent);
    }

    .composer__thread-check {
      align-items: center;
      border: 1px solid var(--color-border-base);
      border-radius: 50%;
      color: white;
      display: inline-flex;
      flex: 0 0 auto;
      font-size: 0.75rem;
      height: 1.5rem;
      justify-content: center;
      width: 1.5rem;
    }

    .composer__thread-check--on {
      background: var(--color-accent);
      border-color: var(--color-accent);
    }

    .composer__thread-avatar {
      align-items: center;
      border-radius: 50%;
      color: white;
      display: inline-flex;
      flex: 0 0 auto;
      font-size: 0.6875rem;
      font-weight: 700;
      height: 1.75rem;
      justify-content: center;
      overflow: hidden;
      width: 1.75rem;
    }

    .composer__thread-avatar img {
      height: 100%;
      object-fit: cover;
      width: 100%;
    }

    .composer__thread-name {
      font-weight: 600;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .composer__thread-age {
      color: var(--color-text-muted);
      flex: 0 0 auto;
      font-size: 0.8125rem;
    }

    .composer__thread-star {
      color: var(--color-accent);
      flex: 0 0 auto;
      margin-left: auto;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadPickerPopoverComponent implements OnInit {
  private readonly imageGallery = inject(ImageGalleryService);

  /** Threads the user could attach; the active thread and archived threads are left out here. */
  readonly threads = input<readonly Chat[]>([]);
  /** Threads currently attached (owned by the caller). */
  readonly selected = input<readonly Chat[]>([]);
  /** The open thread, which can never be attached to itself. */
  readonly activeChatId = input<string | null>(null);
  /** Personality catalog for row avatars. */
  readonly personalities = input<readonly Personality[]>([]);
  readonly disabled = input(false);

  /** A row was clicked: attach it, or detach it if already attached. */
  readonly toggled = output<Chat>();
  /** Done: keep the current selection. */
  readonly done = output<void>();
  /** Cancel / Escape: restore this selection (a snapshot taken when the picker opened). */
  readonly cancelled = output<readonly Chat[]>();

  readonly filter = signal('');
  readonly starredOnly = signal(false);
  private readonly searchInput = viewChild<ElementRef<HTMLInputElement>>('searchInput');
  private snapshot: readonly Chat[] = [];

  /** Every thread matching the search/starred filters, most recently active first. */
  private readonly matches = computed(() => {
    const q = this.filter().trim().toLowerCase();
    const activeId = this.activeChatId();
    const starredOnly = this.starredOnly();
    return this.threads()
      .filter(thread =>
        !!thread.id &&
        thread.id !== activeId &&
        !thread.archived &&
        (!starredOnly || !!thread.is_favorite) &&
        (!q || thread.name.toLowerCase().includes(q)),
      )
      .sort((a, b) => lastActivity(b) - lastActivity(a));
  });

  readonly matchCount = computed(() => this.matches().length);

  /** Rendered rows (capped): each thread with its personality avatar and last-activity label. */
  readonly rows = computed(() => {
    const byId = new Map(this.personalities().map(personality => [personality.id, personality] as const));
    const selectedIds = new Set(this.selected().map(thread => thread.id));
    const getImageUrl = this.imageGallery.getImageUrl.bind(this.imageGallery);
    const now = Date.now();
    return this.matches().slice(0, THREAD_PICKER_ROW_LIMIT).map(thread => {
      const personality = thread.personality_id ? byId.get(thread.personality_id) ?? null : null;
      const label = personality?.name ?? thread.personality_name ?? thread.name;
      return {
        thread,
        selected: selectedIds.has(thread.id),
        initials: avatarInitials(label),
        color: personalityAccent(
          personality ?? { id: thread.personality_id ?? '', name: label, accent_color: null },
        ),
        coverUrl: personalityCoverUrl(personality, [], getImageUrl),
        thumbnailStyle: thumbnailCircleToImageStyle(personality?.thumbnail_circle),
        age: threadAgeLabel(thread.last_message_time ?? thread.updated_at, now),
      };
    });
  });

  constructor() {
    afterNextRender(() => this.searchInput()?.nativeElement.focus());
  }

  ngOnInit(): void {
    this.snapshot = [...this.selected()];
  }

  onFilterInput(event: Event): void {
    this.filter.set((event.target as HTMLInputElement).value);
  }

  pick(thread: Chat): void {
    if (this.disabled()) return;
    this.toggled.emit(thread);
  }

  cancel(): void {
    this.cancelled.emit(this.snapshot);
  }

  onEscape(event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    this.cancel();
  }
}

function lastActivity(thread: Chat): number {
  const parsed = Date.parse(thread.last_message_time ?? thread.updated_at ?? '');
  return Number.isNaN(parsed) ? 0 : parsed;
}

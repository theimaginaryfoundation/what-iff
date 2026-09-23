import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, input, output, signal, viewChild, viewChildren } from '@angular/core';

import { MessageBookmark } from '../../../../core/models/message.model';
import { StarIconComponent } from '../../../../shared/ui/icons/icons';

/**
 * Thread bookmark navigator: a small toolbar button (shown only when the thread has bookmarks)
 * that opens a dropdown list of every bookmark. Selecting one emits `jump` so the page can
 * load the owning page if needed and scroll to it. Purely presentational otherwise.
 */
@Component({
  selector: 'app-thread-bookmarks',
  standalone: true,
  imports: [DatePipe, StarIconComponent],
  template: `
    @if (bookmarks().length) {
      <div class="tb">
        <button
          type="button"
          class="tb__btn"
          [class.tb__btn--open]="open()"
          [attr.aria-expanded]="open()"
          aria-label="Jump to a bookmark"
          aria-haspopup="menu"
          aria-controls="thread-bookmarks-menu"
          (click)="toggle()"
          title="Jump to a bookmark"
          #trigger
        >
          <ui-star-icon [size]="12" [filled]="true" />
          <span>{{ bookmarks().length }}</span>
        </button>

        @if (open()) {
          <div class="tb__backdrop" (click)="close(true)"></div>
          <div id="thread-bookmarks-menu" class="tb__panel" role="menu" aria-label="Bookmarks">
            <div class="tb__head">Bookmarks · {{ bookmarks().length }}</div>
            <ul class="tb__list">
              @for (b of bookmarks(); track b.id; let index = $index) {
                <li class="tb__row">
                  <button
                    type="button"
                    class="tb__item"
                    [class.tb__item--pending]="isPending(b.id)"
                    role="menuitem"
                    [attr.tabindex]="activeIndex() === index ? 0 : -1"
                    (click)="select(b)"
                    (keydown)="onMenuKeydown($event, index)"
                    #menuItem
                  >
                    <span class="tb__badge" [class.tb__badge--assistant]="b.origin === 'Assistant'">
                      {{ b.origin === 'User' ? 'You' : 'AI' }}
                    </span>
                    <span class="tb__snippet">{{ b.snippet || '(no text)' }}</span>
                    <time class="tb__time" [attr.datetime]="b.sent_at">{{ b.sent_at | date: 'MMM d' }}</time>
                  </button>
                  <button
                    type="button"
                    class="tb__remove"
                    [class.tb__remove--pending]="isPending(b.id)"
                    tabindex="-1"
                    [attr.aria-pressed]="isPending(b.id)"
                    [attr.aria-label]="(isPending(b.id) ? 'Restore bookmark: ' : 'Remove bookmark: ') + (b.snippet || 'this message')"
                    [title]="isPending(b.id) ? 'Restore bookmark' : 'Remove bookmark'"
                    (click)="toggleRemoval(b, $event)"
                  >
                    <ui-star-icon [size]="13" [filled]="!isPending(b.id)" />
                  </button>
                </li>
              }
            </ul>
            @if (pendingRemovals().size) {
              <p class="tb__hint">Unstarred bookmarks are removed when you close this menu.</p>
            }
          </div>
        }
      </div>
    }
  `,
  styles: [`
    :host { --bookmark-gold: #e0a53a; }
    .tb { position: relative; display: inline-flex; }

    .tb__btn {
      align-items: center;
      background: transparent;
      border: 1px solid color-mix(in srgb, var(--bookmark-gold) 45%, var(--color-border-base));
      border-radius: 0.5rem;
      color: var(--bookmark-gold);
      cursor: pointer;
      display: inline-flex;
      font-size: 0.7rem;
      font-weight: 700;
      gap: 0.25rem;
      padding: 0.25rem 0.45rem;
    }
    .tb__btn--open,
    .tb__btn:hover { background: color-mix(in srgb, var(--bookmark-gold) 14%, transparent); }

    .tb__backdrop { inset: 0; position: fixed; z-index: 40; }

    .tb__panel {
      background: var(--color-surface-raised, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.6rem;
      box-shadow: 0 12px 32px rgba(0, 0, 0, 0.28);
      max-height: min(60vh, 24rem);
      overflow-y: auto;
      position: absolute;
      right: 0;
      top: calc(100% + 0.35rem);
      width: min(20rem, 80vw);
      z-index: 41;
    }

    .tb__head {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      font-weight: 700;
      letter-spacing: 0.06em;
      padding: 0.55rem 0.75rem 0.35rem;
      text-transform: uppercase;
    }

    .tb__list { display: grid; list-style: none; margin: 0; padding: 0 0.35rem 0.35rem; }

    .tb__row { align-items: center; display: flex; gap: 0.15rem; }

    .tb__item {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.45rem;
      cursor: pointer;
      display: grid;
      flex: 1 1 auto;
      gap: 0.5rem;
      grid-template-columns: auto minmax(0, 1fr) auto;
      min-width: 0;
      padding: 0.4rem 0.4rem;
      text-align: left;
    }
    .tb__item:hover { background: color-mix(in srgb, var(--color-accent) 12%, transparent); }
    .tb__item--pending .tb__snippet { color: var(--color-text-muted); text-decoration: line-through; }
    .tb__item--pending { opacity: 0.6; }

    .tb__remove {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.45rem;
      color: var(--bookmark-gold);
      cursor: pointer;
      display: inline-flex;
      flex: 0 0 auto;
      justify-content: center;
      opacity: 0.7;
      padding: 0.35rem;
    }
    .tb__remove:hover,
    .tb__remove:focus-visible {
      background: color-mix(in srgb, var(--bookmark-gold) 16%, transparent);
      opacity: 1;
    }
    /* Marked for removal: hollow star, muted, so it reads as "will be removed" but is still here. */
    .tb__remove--pending { color: var(--color-text-muted); opacity: 1; }

    .tb__hint {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      line-height: 1.3;
      margin: 0;
      padding: 0.35rem 0.75rem 0.15rem;
    }

    .tb__badge {
      background: color-mix(in srgb, var(--color-accent) 55%, var(--color-surface-base));
      border-radius: 999px;
      color: #fff;
      font-size: 0.55rem;
      font-weight: 700;
      letter-spacing: 0.03em;
      padding: 0.05rem 0.35rem;
      text-transform: uppercase;
    }
    .tb__badge--assistant { background: color-mix(in srgb, var(--bookmark-gold) 78%, var(--color-surface-base)); }

    .tb__snippet {
      color: var(--color-text-primary);
      font-size: 0.78rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .tb__time { color: var(--color-text-muted); font-size: 0.65rem; white-space: nowrap; }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadBookmarksComponent {
  readonly bookmarks = input<MessageBookmark[]>([]);
  readonly jump = output<MessageBookmark>();
  /** Emitted once, on menu close, with the ids the user left un-starred (empty emits are skipped). */
  readonly commitRemovals = output<string[]>();

  readonly open = signal(false);
  readonly activeIndex = signal(0);
  /** Ids the user has un-starred but not yet committed — removed only when the menu closes. */
  readonly pendingRemovals = signal<Set<string>>(new Set());

  private readonly trigger = viewChild<ElementRef<HTMLButtonElement>>('trigger');
  private readonly menuItems = viewChildren<ElementRef<HTMLButtonElement>>('menuItem');

  toggle(): void {
    if (this.open()) {
      this.close(true);
    } else {
      this.openMenu(0);
    }
  }

  close(returnFocus = false): void {
    if (this.open()) {
      this.flushPendingRemovals();
    }
    this.open.set(false);
    if (returnFocus) {
      queueMicrotask(() => this.trigger()?.nativeElement.focus());
    }
  }

  select(bookmark: MessageBookmark): void {
    this.jump.emit(bookmark);
    this.close(true);
  }

  isPending(id: string): boolean {
    return this.pendingRemovals().has(id);
  }

  /**
   * Toggle a bookmark's "remove" state without saving yet — the star hollows and the row dims but
   * stays in the list. Nothing is persisted until the menu closes, so a mis-click 100s of messages
   * back is freely undoable (click again to restore) instead of an instant, obnoxious auto-save.
   * `stopPropagation` keeps the row's jump handler from also firing.
   */
  toggleRemoval(bookmark: MessageBookmark, event: Event): void {
    event.stopPropagation();
    this.pendingRemovals.update(current => {
      const next = new Set(current);
      if (next.has(bookmark.id)) {
        next.delete(bookmark.id);
      } else {
        next.add(bookmark.id);
      }
      return next;
    });
  }

  /** Commit queued removals to the parent (once) and reset, e.g. when the menu closes. */
  private flushPendingRemovals(): void {
    const pending = this.pendingRemovals();
    if (pending.size === 0) return;
    // Only commit ids still present in the current list, in case it changed underneath us.
    const present = new Set(this.bookmarks().map(b => b.id));
    const ids = [...pending].filter(id => present.has(id));
    this.pendingRemovals.set(new Set());
    if (ids.length > 0) {
      this.commitRemovals.emit(ids);
    }
  }

  onMenuKeydown(event: KeyboardEvent, currentIndex: number): void {
    let nextIndex: number | undefined;
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = currentIndex + 1;
        break;
      case 'ArrowUp':
        nextIndex = currentIndex - 1;
        break;
      case 'Home':
        nextIndex = 0;
        break;
      case 'End':
        nextIndex = this.bookmarks().length - 1;
        break;
      case 'Escape':
        event.preventDefault();
        this.close(true);
        return;
      default:
        return;
    }
    event.preventDefault();
    this.focusItem(nextIndex);
  }

  private openMenu(index: number): void {
    this.open.set(true);
    queueMicrotask(() => this.focusItem(index));
  }

  private focusItem(index: number): void {
    const items = this.menuItems();
    if (!items.length) return;
    const normalizedIndex = (index + items.length) % items.length;
    this.activeIndex.set(normalizedIndex);
    items[normalizedIndex].nativeElement.focus();
  }
}

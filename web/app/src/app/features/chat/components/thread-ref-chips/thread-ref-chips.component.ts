import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { threadReferenceCountLabel } from '../../helpers/thread-reference.helpers';

/** Chips shown before collapsing the rest into "+N" (narrow / wide layouts). */
const VISIBLE_CHIPS_MOBILE = 3;
const VISIBLE_CHIPS_DESKTOP = 5;

/**
 * Chips above the composer for the threads attached to the next message. Purely
 * presentational: emits `removed` (thread id) for a chip's × and `cleared` for Clear.
 * Renders nothing when `threads` is empty.
 */
@Component({
  selector: 'app-thread-ref-chips',
  standalone: true,
  template: `
    @if (threads().length) {
      <div class="composer__thread-refs" role="group" aria-label="Threads attached to this message">
        <span class="composer__thread-refs-label">{{ label() }}</span>
        @for (thread of threads(); track thread.id; let i = $index) {
          <span
            class="composer__thread-chip"
            [class.composer__thread-chip--hide-mobile]="i >= mobileLimit"
            [class.composer__thread-chip--hide-desktop]="i >= desktopLimit"
          >
            <span class="composer__thread-chip-name">{{ thread.name }}</span>
            <button
              type="button"
              class="composer__thread-chip-remove"
              [attr.aria-label]="'Remove thread ' + thread.name"
              (click)="removed.emit(thread.id)"
            >×</button>
          </span>
        }
        @if (threads().length > mobileLimit) {
          <span class="composer__thread-more composer__thread-more--mobile">+{{ threads().length - mobileLimit }}</span>
        }
        @if (threads().length > desktopLimit) {
          <span class="composer__thread-more composer__thread-more--desktop">+{{ threads().length - desktopLimit }}</span>
        }
        <button type="button" class="composer__thread-refs-clear" (click)="cleared.emit()">Clear</button>
      </div>
    }
  `,
  styles: [`
    :host {
      display: contents;
    }

    .composer__thread-refs {
      align-items: center;
      display: flex;
      flex-wrap: wrap;
      gap: 0.375rem;
      width: 100%;
    }

    .composer__thread-refs-label {
      color: var(--color-text-muted);
      font-size: 0.6875rem;
      font-weight: 600;
    }

    /* Same look as the composer's pending-skill chips. */
    .composer__thread-chip {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 12%, var(--color-surface-base));
      border: 1px solid color-mix(in srgb, var(--color-accent) 35%, var(--color-border-base));
      border-radius: 999px;
      color: var(--color-text-secondary);
      display: inline-flex;
      font-size: 0.6875rem;
      font-weight: 600;
      gap: 0.25rem;
      max-width: 100%;
      padding: 0.125rem 0.25rem 0.125rem 0.5rem;
    }

    .composer__thread-chip-name {
      max-width: 12rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .composer__thread-chip-remove {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      font-size: 0.875rem;
      line-height: 1;
      padding: 0 0.125rem;
    }

    .composer__thread-chip-remove:hover {
      color: var(--color-danger);
    }

    .composer__thread-more {
      color: var(--color-text-muted);
      font-size: 0.6875rem;
      font-weight: 700;
    }

    .composer__thread-more--mobile {
      display: none;
    }

    .composer__thread-refs-clear {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      font-size: 0.6875rem;
      margin-left: auto;
      padding: 0.125rem 0.25rem;
    }

    .composer__thread-refs-clear:hover {
      color: var(--color-danger);
    }

    @media (max-width: 640px) {
      .composer__thread-chip--hide-mobile {
        display: none;
      }

      .composer__thread-more--mobile {
        display: inline;
      }

      .composer__thread-more--desktop {
        display: none;
      }
    }

    @media (min-width: 641px) {
      .composer__thread-chip--hide-desktop {
        display: none;
      }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadRefChipsComponent {
  /** Attached threads, in the order they were added. */
  readonly threads = input<readonly Chat[]>([]);
  /** A chip's × was clicked (thread id). */
  readonly removed = output<string>();
  /** Clear was clicked. */
  readonly cleared = output<void>();

  protected readonly mobileLimit = VISIBLE_CHIPS_MOBILE;
  protected readonly desktopLimit = VISIBLE_CHIPS_DESKTOP;
  readonly label = computed(() => threadReferenceCountLabel(this.threads().length));
}

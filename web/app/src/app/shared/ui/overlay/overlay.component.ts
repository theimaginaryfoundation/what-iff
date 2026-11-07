import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, effect, inject, input, output, viewChild } from '@angular/core';
import { lockBodyScroll, releaseBodyScroll, type BodyScrollLockHandle } from '../helpers/body-scroll-lock.helpers';
import { createFocusTrap, releaseFocusTrap, type FocusTrapHandle } from '../helpers/focus-trap.helpers';
import { isEscapeKey } from '../helpers/keyboard.helpers';

/**
 * A dialog that fills the viewport (ADR 0x043 §1): the file workspace over the chat on a tablet,
 * expanded on a desktop, or on a phone. Like ui-modal it traps focus, locks the page behind it and
 * closes on Escape; unlike it there is no backdrop to click and the content owns its own header.
 * `inset` leaves a margin around the panel (the expanded desktop case); `fill` has none.
 */
@Component({
  selector: 'ui-overlay',
  standalone: true,
  template: `
    @if (open()) {
      <div class="ui-overlay" [class.ui-overlay--inset]="variant() === 'inset'" role="presentation" (keydown)="onKeydown($event)">
        <section
          #dialog
          class="ui-overlay__panel"
          role="dialog"
          aria-modal="true"
          [attr.aria-labelledby]="labelledBy()"
          [attr.aria-describedby]="describedBy()"
        >
          <ng-content />
        </section>
      </div>
    }
  `,
  styles: [
    `
      .ui-overlay {
        background: rgb(0 0 0 / 0.6);
        display: flex;
        inset: 0;
        position: fixed;
        z-index: 50;
      }
      .ui-overlay--inset {
        padding: 1.5rem;
      }
      .ui-overlay__panel {
        background: var(--bg-base);
        color: var(--text-primary);
        display: flex;
        flex: 1 1 auto;
        flex-direction: column;
        min-height: 0;
        min-width: 0;
        outline: none;
      }
      .ui-overlay--inset .ui-overlay__panel {
        border: 1px solid var(--border);
        border-radius: 0.75rem;
        box-shadow: 0 20px 60px rgb(0 0 0 / 0.35);
        overflow: hidden;
      }
      @media (max-width: 767px) {
        .ui-overlay--inset {
          padding: 0;
        }
        .ui-overlay--inset .ui-overlay__panel {
          border: 0;
          border-radius: 0;
        }
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class OverlayComponent implements OnDestroy {
  readonly open = input(false);
  readonly labelledBy = input<string | null>(null);
  readonly describedBy = input<string | null>(null);
  readonly dismissible = input(true);
  readonly variant = input<'fill' | 'inset'>('fill');
  readonly dismiss = output<void>();

  readonly dialog = viewChild<ElementRef<HTMLElement>>('dialog');
  private readonly document = inject(DOCUMENT);
  private focusTrap: FocusTrapHandle | null = null;
  private bodyLock: BodyScrollLockHandle | null = null;

  constructor() {
    effect(() => {
      if (this.open()) {
        this.bodyLock ??= lockBodyScroll(this.document.body);
        setTimeout(() => this.activateFocusTrap(), 0);
      } else {
        this.release();
      }
    });
  }

  ngOnDestroy(): void {
    this.release();
  }

  onKeydown(event: KeyboardEvent): void {
    if (isEscapeKey(event) && this.dismissible()) {
      event.preventDefault();
      this.dismiss.emit();
    }
  }

  private activateFocusTrap(): void {
    const dialog = this.dialog()?.nativeElement;
    if (this.open() && dialog && !this.focusTrap) this.focusTrap = createFocusTrap(dialog);
  }

  private release(): void {
    releaseFocusTrap(this.focusTrap);
    releaseBodyScroll(this.bodyLock);
    this.focusTrap = null;
    this.bodyLock = null;
  }
}

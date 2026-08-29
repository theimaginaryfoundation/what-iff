import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, effect, inject, input, output, viewChild } from '@angular/core';
import { lockBodyScroll, releaseBodyScroll, type BodyScrollLockHandle } from '../helpers/body-scroll-lock.helpers';
import { createFocusTrap, releaseFocusTrap, type FocusTrapHandle } from '../helpers/focus-trap.helpers';
import { isEscapeKey } from '../helpers/keyboard.helpers';
import { XIconComponent } from '../icons';

export type ModalSize = 'sm' | 'md' | 'lg' | 'xl';

/**
 * How a dismiss was triggered. Consumers can use this to guard "soft" dismissals
 * (backdrop click / Escape) behind an unsaved-changes confirmation while letting
 * the explicit header close button dismiss immediately.
 */
export type ModalDismissReason = 'backdrop' | 'escape' | 'close-button';

const SIZE_CLASSES: Record<ModalSize, string> = {
  sm: 'max-w-sm',
  md: 'max-w-md',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
};

@Component({
  selector: 'ui-modal',
  standalone: true,
  imports: [XIconComponent],
  templateUrl: './modal.component.html',
  styleUrl: './modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ModalComponent implements OnDestroy {
  readonly open = input(false);
  readonly labelledBy = input<string | null>(null);
  readonly describedBy = input<string | null>(null);
  readonly dismissible = input(true);
  readonly size = input<ModalSize>('md');
  readonly fullscreen = input(false);
  readonly dismiss = output<ModalDismissReason>();

  readonly dialog = viewChild<ElementRef<HTMLElement>>('dialog');
  private readonly document = inject(DOCUMENT);
  private focusTrap: FocusTrapHandle | null = null;
  private bodyLock: BodyScrollLockHandle | null = null;

  constructor() {
    effect(() => {
      if (this.open()) {
        this.lockBody();
        setTimeout(() => this.activateFocusTrap(), 0);
      } else {
        this.releaseFocusTrap();
        this.releaseBody();
      }
    });
  }

  ngOnDestroy(): void {
    this.releaseFocusTrap();
    this.releaseBody();
  }

  panelClass(): string {
    if (this.fullscreen()) {
      return 'ui-modal__panel ui-modal__panel--fullscreen w-full';
    }
    return `ui-modal__panel w-full ${SIZE_CLASSES[this.size()]}`;
  }

  backdropClass(): string {
    return this.fullscreen()
      ? 'ui-modal fixed inset-0 z-50 flex bg-black/60'
      : 'ui-modal fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4';
  }

  onBackdropClick(): void {
    this.requestDismiss('backdrop');
  }

  onDialogClick(event: Event): void {
    event.stopPropagation();
  }

  onKeydown(event: KeyboardEvent): void {
    if (isEscapeKey(event) && this.dismissible()) {
      event.preventDefault();
      this.dismiss.emit('escape');
    }
  }

  requestDismiss(reason: ModalDismissReason = 'close-button'): void {
    if (this.dismissible()) {
      this.dismiss.emit(reason);
    }
  }

  private lockBody(): void {
    if (!this.bodyLock) {
      this.bodyLock = lockBodyScroll(this.document.body);
    }
  }

  private releaseBody(): void {
    releaseBodyScroll(this.bodyLock);
    this.bodyLock = null;
  }

  private activateFocusTrap(): void {
    if (!this.open() || this.focusTrap) {
      return;
    }

    const dialog = this.dialog()?.nativeElement;
    if (dialog) {
      this.focusTrap = createFocusTrap(dialog);
    }
  }

  private releaseFocusTrap(): void {
    releaseFocusTrap(this.focusTrap);
    this.focusTrap = null;
  }
}

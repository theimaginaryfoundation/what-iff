import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, effect, inject, input, output, viewChild } from '@angular/core';
import { lockBodyScroll, releaseBodyScroll, type BodyScrollLockHandle } from '../helpers/body-scroll-lock.helpers';
import { createFocusTrap, releaseFocusTrap, type FocusTrapHandle } from '../helpers/focus-trap.helpers';
import { isEscapeKey } from '../helpers/keyboard.helpers';
import { XIconComponent } from '../icons';

@Component({
  selector: 'ui-sheet',
  standalone: true,
  imports: [XIconComponent],
  templateUrl: './sheet.component.html',
  styleUrl: './sheet.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetComponent implements OnDestroy {
  readonly open = input(false);
  readonly labelledBy = input<string | null>(null);
  readonly describedBy = input<string | null>(null);
  readonly dismissible = input(true);
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

  ngOnDestroy(): void { this.release(); }
  requestDismiss(): void { if (this.dismissible()) this.dismiss.emit(); }
  onDialogClick(event: Event): void { event.stopPropagation(); }
  onKeydown(event: KeyboardEvent): void { if (isEscapeKey(event) && this.dismissible()) { event.preventDefault(); this.dismiss.emit(); } }

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

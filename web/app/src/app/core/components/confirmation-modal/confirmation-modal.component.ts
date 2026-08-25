import { Component, inject, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ConfirmationService } from '../../services/confirmation.service';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';

type ConfirmationType = 'danger' | 'warning' | 'info' | 'success';

// Typed constant map for icon paths
const ICON_PATHS: Record<ConfirmationType, string> = {
  danger: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L3.732 16.5c-.77.833.192 2.5 1.732 2.5z',
  warning: 'M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z',
  success: 'M10 .5a9.5 9.5 0 1 0 9.5 9.5A9.51 9.51 0 0 0 10 .5Zm3.707 8.207-4 4a1 1 0 0 1-1.414 0l-2-2a1 1 0 0 1 1.414-1.414L9 10.586l3.293-3.293a1 1 0 0 1 1.414 1.414Z',
  info: 'M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z'
};

// Typed constant maps for icon styling
const ICON_COLOR_CLASSES: Record<ConfirmationType, string> = {
  danger: 'text-red-600 dark:text-red-400',
  warning: 'text-yellow-600 dark:text-yellow-400',
  success: 'text-green-600 dark:text-green-400',
  info: 'text-indigo-600 dark:text-indigo-400'
};

const ICON_BG_CLASSES: Record<ConfirmationType, string> = {
  danger: 'bg-red-100 dark:bg-red-900',
  warning: 'bg-yellow-100 dark:bg-yellow-900',
  success: 'bg-green-100 dark:bg-green-900',
  info: 'bg-indigo-100 dark:bg-indigo-900'
};

// Typed constant map for button classes
const BUTTON_CLASSES: Record<ConfirmationType, string> = {
  danger: 'bg-red-600 hover:bg-red-700 dark:bg-red-600 dark:hover:bg-red-700',
  warning: 'bg-yellow-600 hover:bg-yellow-700 dark:bg-yellow-600 dark:hover:bg-yellow-700',
  success: 'bg-green-600 hover:bg-green-700 dark:bg-green-600 dark:hover:bg-green-700',
  info: 'bg-indigo-600 hover:bg-indigo-700 dark:bg-indigo-600 dark:hover:bg-indigo-700'
};

@Component({
  selector: 'app-confirmation-modal',
  standalone: true,
  imports: [CommonModule, ModalComponent],
  templateUrl: './confirmation-modal.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./confirmation-modal.component.scss']
})
export class ConfirmationModalComponent {
  public confirmationService = inject(ConfirmationService);

  onConfirm(): void {
    // For async operations, components should handle confirmation themselves
    // and use setLoading() to show loading state
    this.confirmationService.onConfirm();
  }

  onCancel(): void {
    this.confirmationService.onCancel();
  }

  getIconPath(): string {
    return ICON_PATHS[this.confirmationService.type()];
  }

  getIconColorClass(): string {
    return ICON_COLOR_CLASSES[this.confirmationService.type()];
  }

  getIconBgClass(): string {
    return ICON_BG_CLASSES[this.confirmationService.type()];
  }

  getConfirmButtonClass(): string {
    // Use custom class if provided, otherwise use type-based default
    const customClass = this.confirmationService.confirmButtonClass();
    return customClass || BUTTON_CLASSES[this.confirmationService.type()];
  }
}


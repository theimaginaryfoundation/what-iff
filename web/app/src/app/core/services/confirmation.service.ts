import { Injectable, signal } from '@angular/core';

export interface ConfirmationOptions {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  confirmButtonClass?: string;
  type?: 'danger' | 'warning' | 'info' | 'success';
  keepOpen?: boolean; // If true, modal stays open after confirmation (for async operations)
}

@Injectable({
  providedIn: 'root'
})
export class ConfirmationService {
  // Modal state
  isOpen = signal(false);
  title = signal<string>('Confirm');
  message = signal<string>('');
  confirmText = signal<string>('Confirm');
  cancelText = signal<string>('Cancel');
  confirmButtonClass = signal<string>('');
  type = signal<'danger' | 'warning' | 'info' | 'success'>('info');
  isLoading = signal(false);
  loadingText = signal<string>('');
  keepOpen = signal(false);

  private currentResolve?: (value: boolean) => void;

  /**
   * Show a confirmation dialog
   * @param options Configuration options for the confirmation dialog
   * @returns Promise that resolves to true if confirmed, false if cancelled
   * @returns Promise that resolves to false if called while another dialog is already open (maintains backward compatibility)
   */
  confirm(options: ConfirmationOptions): Promise<boolean> {
    // Guard against concurrent calls - return false (cancelled) if dialog is already open
    // This maintains backward compatibility while preventing the bug
    if (this.isOpen()) {
      console.warn(
        'ConfirmationService.confirm() called while another confirmation dialog is already open. ' +
        'Returning false (cancelled) to maintain backward compatibility. ' +
        'Please wait for the current dialog to be resolved before opening a new one.'
      );
      return Promise.resolve(false);
    }

    return new Promise<boolean>((resolve) => {
      // Resolve any existing pending promise (shouldn't happen due to guard, but safety check)
      this.resolvePending(false);

      this.currentResolve = resolve;

      // Set modal content
      this.title.set(options.title || 'Confirm');
      this.message.set(options.message);
      this.confirmText.set(options.confirmText || 'Confirm');
      this.cancelText.set(options.cancelText || 'Cancel');
      this.type.set(options.type || 'info');
      this.keepOpen.set(options.keepOpen || false);

      // Store custom button class if provided (component will use type-based default if empty)
      this.confirmButtonClass.set(options.confirmButtonClass || '');

      // Reset loading state
      this.isLoading.set(false);
      this.loadingText.set('');

      // Open modal (DOM manipulation handled by component)
      this.isOpen.set(true);
    });
  }

  /**
   * Show an alert dialog (confirmation with only OK button)
   * @param options Configuration options for the alert dialog
   * @returns Promise that resolves when the alert is closed
   * @returns Promise that resolves immediately if called while another dialog is already open (maintains backward compatibility)
   */
  alert(options: Omit<ConfirmationOptions, 'confirmText' | 'cancelText'>): Promise<void> {
    // Guard against concurrent calls - return resolved promise if dialog is already open
    // This maintains backward compatibility while preventing the bug
    if (this.isOpen()) {
      console.warn(
        'ConfirmationService.alert() called while another confirmation dialog is already open. ' +
        'Returning resolved promise to maintain backward compatibility. ' +
        'Please wait for the current dialog to be resolved before opening a new one.'
      );
      return Promise.resolve();
    }

    return new Promise<void>((resolve) => {
      // Resolve any existing pending promise (shouldn't happen due to guard, but safety check)
      this.resolvePending(false);

      // For alerts, wrap the void resolve to match the boolean signature
      // The boolean parameter is ignored since alerts don't return a value
      this.currentResolve = () => resolve();

      // Set modal content (logical state only)
      this.title.set(options.title || 'Alert');
      this.message.set(options.message);
      this.confirmText.set('OK');
      this.cancelText.set(''); // Hide cancel button
      this.type.set(options.type || 'info');

      // Store custom button class if provided (component will use type-based default if empty)
      this.confirmButtonClass.set(options.confirmButtonClass || '');

      // Reset loading state
      this.isLoading.set(false);
      this.loadingText.set('');

      // Open modal (DOM manipulation handled by component)
      this.isOpen.set(true);
    });
  }

  /**
   * Standard "you have unsaved changes" guard shown before discarding an edit.
   * Resolves true when the user chooses to discard, false to keep editing.
   */
  confirmDiscardChanges(): Promise<boolean> {
    return this.confirm({
      title: 'Discard changes?',
      message: "You have unsaved changes. They'll be lost if you close.",
      confirmText: 'Discard',
      cancelText: 'Keep editing',
      type: 'warning',
    });
  }

  /**
   * Set loading state for async operations
   */
  setLoading(loading: boolean, text?: string): void {
    this.isLoading.set(loading);
    if (text !== undefined) {
      this.loadingText.set(text);
    }
  }

  /**
   * Handle confirmation (user clicked confirm)
   */
  onConfirm(): void {
    // Don't allow confirmation if already loading
    if (this.isLoading()) {
      return;
    }
    // Resolve with true (confirmed)
    this.resolvePending(true);
    // Only close if not set to keep open (for async operations)
    if (!this.keepOpen()) {
      this.close();
    }
  }

  /**
   * Handle cancellation (user clicked cancel or closed)
   */
  onCancel(): void {
    // Don't allow cancellation if loading
    if (this.isLoading()) {
      return;
    }
    // Resolve with false (cancelled)
    this.resolvePending(false);
    this.close();
  }

  /**
   * Resolve any pending promise (helper method)
   * @param value The value to resolve with (true for confirm, false for cancel)
   */
  private resolvePending(value: boolean): void {
    if (this.currentResolve) {
      this.currentResolve(value);
      this.currentResolve = undefined;
    }
  }

  /**
   * Close the modal
   * If called directly (not through onConfirm/onCancel), resolves any pending promise as cancelled (false)
   * Only resolves if the modal is actually open
   * DOM manipulation (body overflow) is handled by the component
   */
  close(): void {
    if (this.isOpen() && this.currentResolve) {
      this.resolvePending(false);
    }

    // Reset state
    this.isOpen.set(false);
    this.isLoading.set(false);
    this.loadingText.set('');
    this.keepOpen.set(false);
    // DOM manipulation (body overflow) handled by component via effect
  }
}


import { Injectable, signal } from '@angular/core';

/**
 * Whether some open editor holds changes that are not saved. Core code that ends the session,
 * such as signing out, asks this; the editor that owns the changes sets it.
 */
@Injectable({ providedIn: 'root' })
export class UnsavedChangesService {
  private readonly _pending = signal(false);
  readonly pending = this._pending.asReadonly();

  set(pending: boolean): void {
    this._pending.set(pending);
  }
}

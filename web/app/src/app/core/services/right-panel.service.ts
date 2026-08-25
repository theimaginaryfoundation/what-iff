import { Injectable, Signal, signal } from '@angular/core';

/**
 * Stub for the right context panel introduced in Phase 03 as an empty slot.
 * Phase 7 (Conversation Context) replaces this with the real content driver
 * (chat scratchpad / summary / pinned threads etc.). Keeping the signal here
 * means feature components can already opt in / out of the slot without
 * waiting for the Phase 7 service surface to land.
 */
@Injectable({ providedIn: 'root' })
export class RightPanelService {
  private readonly _visible = signal(false);

  /** Whether the right context panel should be rendered. Defaults to false. */
  readonly visible: Signal<boolean> = this._visible.asReadonly();

  /** Sets the right panel visibility. */
  setVisible(value: boolean): void {
    this._visible.set(value);
  }

  /** Convenience toggle. */
  toggle(): void {
    this._visible.set(!this._visible());
  }
}

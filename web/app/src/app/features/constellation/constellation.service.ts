import { Injectable, signal } from '@angular/core';

/** Small shell entry point; the map and curriculum load only on demand. */
@Injectable({ providedIn: 'root' })
export class ConstellationService {
  readonly visible = signal(false);
  open(): void {
    this.visible.set(true);
  }
  close(): void {
    this.visible.set(false);
  }
}

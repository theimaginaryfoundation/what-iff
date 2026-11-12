import { Injectable, signal } from '@angular/core';

/**
 * Whether the comments panel is open (ADR 0x045), kept across the editors a file opens in, so
 * switching between viewing and editing leaves it as it was. Null until the user opens or closes
 * it: the panel then opens by itself when a file has open threads.
 */
@Injectable({ providedIn: 'root' })
export class CommentsPanelState {
  readonly open = signal<boolean | null>(null);
  /**
   * A thread to show, from a link to a comment (ADR 0x046): the panel selects it once it has it,
   * which makes it active in the file, and clears this.
   */
  readonly focus = signal<string | null>(null);

  /** Opens the panel at a thread. */
  focusThread(threadId: string): void {
    this.open.set(true);
    this.focus.set(threadId);
  }
}

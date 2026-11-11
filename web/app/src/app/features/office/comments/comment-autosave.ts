import { Signal, computed, signal } from '@angular/core';

import { CommentSaver, FileEditor } from '../../file-workspace/editors/file-editor';

export type CommentSaveStatus = 'idle' | 'saving' | 'saved' | 'failed' | 'conflict' | 'cancelled';

/**
 * Saves comments made while viewing (ADR 0x045), one save at a time: changes made during a save
 * are written by the next one. Without a saver (a viewer outside the workspace, or a file the user
 * may not change), comments cannot be written while viewing.
 */
export class CommentAutosave {
  readonly status = signal<CommentSaveStatus>('idle');
  readonly available: Signal<boolean>;
  private running = false;
  private pending = false;

  constructor(
    private readonly saver: CommentSaver | null,
    private readonly editor: Pick<FileEditor, 'content' | 'markSaved' | 'losses'>,
  ) {
    this.available = computed(() => !!saver?.canComment());
  }

  /** Writes the comments as they are now, after any save already running. */
  request(): void {
    if (!this.saver) return;
    this.pending = true;
    if (!this.running) void this.run();
  }

  private async run(): Promise<void> {
    this.running = true;
    while (this.pending) {
      this.pending = false;
      this.status.set('saving');
      const result = await this.saver!.saveComments(this.editor);
      this.status.set(result);
      // A save that did not land drops what came after it too: the file is reloaded as it is.
      if (result !== 'saved') this.pending = false;
    }
    this.running = false;
  }
}

/** What the comments panel says about a save, if anything. */
export function saveStatusText(status: CommentSaveStatus): string | null {
  switch (status) {
    case 'saving':
      return 'Saving…';
    case 'saved':
      return 'Saved';
    case 'failed':
      return 'Your comment could not be saved. Try again.';
    case 'conflict':
      return 'This file changed while you were viewing it, so it was reloaded. Post your comment again.';
    default:
      return null;
  }
}

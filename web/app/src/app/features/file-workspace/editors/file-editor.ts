import { InjectionToken, Signal } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';

/**
 * What the workspace host needs from any editor it creates (ADR 0x043 §2): the content to save,
 * whether it changed, and a way to mark the saved state. Editors are created dynamically for the
 * file's kind, so this is a structural contract rather than a base class.
 */
export interface FileEditor {
  /** True once the content differs from what was loaded or last saved. */
  readonly dirty: Signal<boolean>;
  /** The content as it should be stored. */
  content(): Promise<Blob>;
  /**
   * Called after a successful save with what was saved, which is the new baseline. The editor may
   * have changed since `content()` was called, in which case it stays dirty.
   */
  markSaved(saved?: Blob): void;
  /**
   * Called after a save landed, with the file as stored (its new content_version) and what was
   * saved: an editor whose comments live beside the file (text and code, ADR 0x045) writes them
   * then, placed on the content just stored. A failure leaves the editor dirty, to save again.
   */
  saved?(file: FileAttachment, content: Blob): Promise<void>;
  /**
   * What saving would drop, in words (the office suite's fidelity report, ADR 0x043): the host
   * asks before overwriting the original when this is not empty.
   */
  readonly losses?: Signal<readonly string[]>;
}

/**
 * Saving comments made while a file is only being viewed (ADR 0x045). Comments live inside the
 * file, so a comment posted in the viewer is written as the file's new version straight away. The
 * workspace provides this to the viewer it hosts; a viewer without it shows comments read-only.
 */
export interface CommentSaver {
  /** Whether comments can be written here: the user may change the file, and it is only being viewed. */
  readonly canComment: Signal<boolean>;
  /**
   * Writes the file with the viewer's comments: `saved`, or `cancelled` (the user declined a save
   * that would lose content), `conflict` (the file changed elsewhere and was reloaded), `failed`.
   */
  saveComments(editor: Pick<FileEditor, 'content' | 'markSaved' | 'losses'>): Promise<CommentSaveResult>;
  /** The file changed elsewhere (comments kept beside it found it so): show it as it is stored now. */
  showCurrentContent(): void;
}

export type CommentSaveResult = 'saved' | 'cancelled' | 'conflict' | 'failed';

export const COMMENT_SAVER = new InjectionToken<CommentSaver>('COMMENT_SAVER');

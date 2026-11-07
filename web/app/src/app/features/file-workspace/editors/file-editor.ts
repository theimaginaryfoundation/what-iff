import { Signal } from '@angular/core';

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
  /** Called after a successful save (or a reload): the current content is the new baseline. */
  markSaved(): void;
  /**
   * What saving would drop, in words (the office suite's fidelity report, ADR 0x043): the host
   * asks before overwriting the original when this is not empty.
   */
  readonly losses?: Signal<readonly string[]>;
}

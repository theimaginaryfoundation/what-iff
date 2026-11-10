import { InjectionToken, Type } from '@angular/core';

import { FilePreviewKind } from '../../gallery/helpers/file-preview.helpers';

/**
 * What every viewer takes: the file and its bytes. The workspace host fetches the bytes once (it
 * also needs them for the download) and hands them over; a viewer decodes or renders as it likes.
 */
export interface FileViewerInputs {
  readonly file: import('../../../core/models/file-attachment.model').FileAttachment;
  readonly blob: Blob;
}

export interface ViewerEntry {
  /** Loads the viewer component on demand, so heavy libraries stay out of the initial bundle. */
  readonly load: () => Promise<Type<unknown>>;
  /** Whether the viewer can show on a phone. Everything can be viewed; editing is what phones lack. */
  readonly phoneView: boolean;
  /** Loads the editor for the kind, when there is one (a FileEditor, ADR 0x043 §2). */
  readonly edit?: () => Promise<Type<unknown>>;
  /** The file types the editor can write back, when it cannot write every type of its kind. */
  readonly editTypes?: readonly string[];
}

/**
 * The registry (ADR 0x043 §2): which component shows each kind of file. A kind with no entry has
 * no preview and gets the download placeholder. Editors are added per kind in later steps.
 */
const loadTextViewer = () => import('./text-viewer.component').then(m => m.TextViewerComponent);
const loadCodeEditor = () => import('../editors/code-editor.component').then(m => m.CodeEditorComponent);

const VIEWERS: Partial<Record<FilePreviewKind, ViewerEntry>> = {
  text: { load: loadTextViewer, phoneView: true, edit: loadCodeEditor },
  markdown: { load: loadTextViewer, phoneView: true, edit: loadCodeEditor },
  csv: { load: loadTextViewer, phoneView: true, edit: loadCodeEditor },
  image: {
    load: () => import('./image-viewer.component').then(m => m.ImageViewerComponent),
    phoneView: true,
    edit: () => import('../editors/image-editor.component').then(m => m.ImageEditorComponent),
    // What a canvas can encode. GIF, SVG and HEIC are viewed only.
    editTypes: ['image/png', 'image/jpeg', 'image/jpg', 'image/webp'],
  },
  pdf: {
    load: () => import('./pdf-viewer.component').then(m => m.PdfViewerComponent),
    phoneView: true,
    edit: () => import('../editors/pdf-editor.component').then(m => m.PdfEditorComponent),
  },
};

const loaded = new Map<FilePreviewKind, Promise<Type<unknown>>>();

export function viewerFor(kind: FilePreviewKind): ViewerEntry | null {
  return VIEWERS[kind] ?? null;
}

/** Loads the viewer for kind once; later calls share the promise. */
export function loadViewer(kind: FilePreviewKind): Promise<Type<unknown>> | null {
  const entry = viewerFor(kind);
  if (!entry) return null;
  let promise = loaded.get(kind);
  if (!promise) {
    const attempt = entry.load();
    loaded.set(kind, attempt);
    // A failed import is not kept, so opening the file again retries instead of waiting forever.
    attempt.catch(() => {
      if (loaded.get(kind) === attempt) loaded.delete(kind);
    });
    promise = attempt;
  }
  return promise;
}

const loadedEditors = new Map<FilePreviewKind, Promise<Type<unknown>>>();

/** Whether the kind has an editor. */
export function hasEditor(kind: FilePreviewKind, fileType?: string | null): boolean {
  const entry = VIEWERS[kind];
  if (!entry?.edit) return false;
  return !entry.editTypes || entry.editTypes.includes((fileType ?? '').toLowerCase());
}

/** Loads the editor for kind once; null when the kind has none. */
export function loadEditor(kind: FilePreviewKind): Promise<Type<unknown>> | null {
  const entry = VIEWERS[kind];
  if (!entry?.edit) return null;
  let promise = loadedEditors.get(kind);
  if (!promise) {
    const attempt = entry.edit();
    loadedEditors.set(kind, attempt);
    attempt.catch(() => {
      if (loadedEditors.get(kind) === attempt) loadedEditors.delete(kind);
    });
    promise = attempt;
  }
  return promise;
}

/** For tests: forget loaded viewers and editors. */
export function resetViewerRegistry(): void {
  loaded.clear();
  loadedEditors.clear();
}

/** The editor loader as a token, so a host can be given fake editors in tests. */
export const EDITOR_LOADER = new InjectionToken<typeof loadEditor>('EDITOR_LOADER', { providedIn: 'root', factory: () => loadEditor });

/** The loader as a token, so a host can be given fake viewers in tests. */
export const VIEWER_LOADER = new InjectionToken<typeof loadViewer>('VIEWER_LOADER', { providedIn: 'root', factory: () => loadViewer });

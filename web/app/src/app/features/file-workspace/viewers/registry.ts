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
}

/**
 * The registry (ADR 0x043 §2): which component shows each kind of file. A kind with no entry has
 * no preview and gets the download placeholder. Editors are added per kind in later steps.
 */
const VIEWERS: Partial<Record<FilePreviewKind, ViewerEntry>> = {
  text: { load: () => import('./text-viewer.component').then(m => m.TextViewerComponent), phoneView: true },
  markdown: { load: () => import('./text-viewer.component').then(m => m.TextViewerComponent), phoneView: true },
  csv: { load: () => import('./text-viewer.component').then(m => m.TextViewerComponent), phoneView: true },
  image: { load: () => import('./image-viewer.component').then(m => m.ImageViewerComponent), phoneView: true },
  pdf: { load: () => import('./pdf-viewer.component').then(m => m.PdfViewerComponent), phoneView: true },
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
    promise = entry.load();
    loaded.set(kind, promise);
  }
  return promise;
}

/** For tests: forget loaded viewers. */
export function resetViewerRegistry(): void {
  loaded.clear();
}

/** The loader as a token, so a host can be given fake viewers in tests. */
export const VIEWER_LOADER = new InjectionToken<typeof loadViewer>('VIEWER_LOADER', { providedIn: 'root', factory: () => loadViewer });

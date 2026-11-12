import { DOCUMENT } from '@angular/common';
import { Injectable, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';

import { CommentsPanelState } from '../office/comments/comments-panel-state.service';
import { Subscription } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { UnsavedChangesService } from '../../core/services/unsaved-changes.service';

/** Which page hosts the workspace. The gallery shows it in place; the chat beside or over the thread. */
export type WorkspaceHost = 'chat' | 'gallery';
/** The viewport band (ADR 0x043 §1): editing is offered on medium and large, never on a phone. */
export type WorkspaceBand = 'large' | 'medium' | 'phone';
/**
 * How the workspace is laid out: in the page (gallery), beside the thread (large), over it
 * (medium, or expanded on large), or filling a phone's screen (view only).
 */
export type WorkspaceLayout = 'inline' | 'docked' | 'overlay' | 'full' | 'phone';

export const LARGE_VIEWPORT_QUERY = '(min-width: 1280px)';
export const MEDIUM_VIEWPORT_QUERY = '(min-width: 768px)';
const STORAGE_KEY = 'fileWorkspace.v1';
/** The docked column's share of the viewport by default, and the bounds a drag can take it to. */
export const DOCKED_WIDTH_DEFAULT_FRACTION = 0.5;
export const DOCKED_WIDTH_MIN_FRACTION = 0.4;
export const DOCKED_WIDTH_MAX_FRACTION = 0.75;

interface StoredState {
  /** The file last open in each conversation, so returning to the thread restores it. */
  byChat: Record<string, { fileId: string }>;
  dockedWidth?: number;
  expanded?: boolean;
}

/**
 * The file workspace's state (ADR 0x043 §1): which file is open, from which host, and how it is
 * laid out for the viewport. The URL (`?file=<id>`) is the source of truth for what is open, so
 * links work and Back closes it: hosts react to the query parameter and call show / hide, and
 * anything that opens a file navigates through requestOpen.
 */
@Injectable({ providedIn: 'root' })
export class FileWorkspaceService {
  private readonly router = inject(Router);
  private readonly panel = inject(CommentsPanelState);
  private readonly gallery = inject(ImageGalleryService);
  private readonly document = inject(DOCUMENT);

  private readonly _host = signal<WorkspaceHost | null>(null);
  private readonly _fileId = signal<string | null>(null);
  private infoRequest: Subscription | null = null;
  private readonly _file = signal<FileAttachment | null>(null);
  private readonly _error = signal<string | null>(null);
  private readonly _chatId = signal<string | null>(null);
  private readonly stored = signal<StoredState>(readStored());
  private readonly _band = signal<WorkspaceBand>(this.measureBand());
  private readonly unsavedChanges = inject(UnsavedChangesService);
  private readonly _unsaved = signal(false);

  readonly host = this._host.asReadonly();
  readonly fileId = this._fileId.asReadonly();
  /** The open file's metadata: the row the host already had at once, then the server's copy. */
  readonly file = this._file.asReadonly();
  readonly error = this._error.asReadonly();
  /** The conversation the workspace was opened from, for saves and change events. */
  readonly chatId = this._chatId.asReadonly();
  readonly band = this._band.asReadonly();
  /** The open editor holds changes that are not saved. */
  readonly unsaved = this._unsaved.asReadonly();
  readonly open = computed(() => this._fileId() !== null);
  readonly expanded = computed(() => this.stored().expanded ?? false);
  readonly dockedWidth = computed(() => this.clampDockedWidth(this.stored().dockedWidth));
  readonly layout = computed<WorkspaceLayout>(() => {
    if (this._host() !== 'chat') return 'inline';
    const band = this._band();
    if (band === 'phone') return 'phone';
    if (this.expanded()) return 'full';
    return band === 'large' ? 'docked' : 'overlay';
  });
  /** Editors are offered on tablets and larger; a phone is view only (ADR 0x043 §1). */
  readonly canEdit = computed(() => this._band() !== 'phone');
  readonly dockedOpen = computed(() => this.open() && this.layout() === 'docked');
  readonly floatingOpen = computed(
    () => this.open() && (this.layout() === 'overlay' || this.layout() === 'full' || this.layout() === 'phone'),
  );

  constructor() {
    this.document.defaultView?.addEventListener('resize', () => {
      // Crossing a breakpoint moves the workspace to another host, which would rebuild an editor
      // and lose its edits, so the layout waits until they are saved.
      if (!this._unsaved()) this._band.set(this.measureBand());
    });
  }

  /**
   * Shows fileId in host. known is the row the host already has, shown at once while the server's
   * copy (which carries the thread link) loads. A second call for the same host and file is a no-op.
   */
  show(host: WorkspaceHost, fileId: string, opts: { chatId?: string | null; known?: FileAttachment | null } = {}): void {
    const chatId = opts.chatId ?? null;
    if (this._host() === host && this._fileId() === fileId && this._chatId() === chatId) return;
    this._host.set(host);
    this._chatId.set(chatId);
    this._fileId.set(fileId);
    this._error.set(null);
    this._file.set(opts.known ?? null);
    if (host === 'chat' && chatId) this.remember(chatId, fileId);
    // Only the shown file's details matter; a request for one shown before is dropped.
    this.infoRequest?.unsubscribe();
    this.infoRequest = this.gallery.getFileInfo(fileId).subscribe({
      next: info => {
        if (this._fileId() === fileId) this._file.set(info);
      },
      error: () => {
        if (this._fileId() === fileId && !this._file()) this._error.set('This file could not be found. It may have been deleted.');
      },
    });
  }

  /** Closes the workspace. forget drops the per-conversation memory of the file (the user closed it). */
  hide(opts: { forget?: boolean } = {}): void {
    const chatId = this._chatId();
    if (opts.forget && chatId) this.remember(chatId, null);
    this._fileId.set(null);
    this._file.set(null);
    this._error.set(null);
    this._host.set(null);
    this._chatId.set(null);
  }

  /** Replaces the open file's metadata after a rename, move or save. */
  update(file: FileAttachment): void {
    if (this._fileId() === file.id) this._file.update(current => ({ ...current, ...file, chat_id: file.chat_id ?? current?.chat_id }));
  }

  /**
   * Opens fileId on the current page by setting `?file=`; the page's host does the rest. With
   * `comment`, at that thread (`?comment=`, ADR 0x046), even when the file is open already.
   */
  requestOpen(fileId: string, opts: { replaceUrl?: boolean; comment?: string } = {}): void {
    if (opts.comment) {
      this.panel.focusThread(opts.comment);
      this.followedLink = `${fileId}#${opts.comment}`;
    }
    void this.router.navigate([], {
      queryParams: { file: fileId, comment: opts.comment ?? null },
      queryParamsHandling: 'merge',
      replaceUrl: opts.replaceUrl ?? false,
    });
  }

  /** The `?comment=` link followed last, so it is followed once. */
  private followedLink: string | null = null;

  /**
   * A page's `?file=` and `?comment=` (ADR 0x046): a link to a comment opens the comments panel at
   * its thread, once per link, so a later change of the page's query leaves the panel alone.
   */
  followCommentLink(fileId: string | null, comment: string | null): void {
    const link = fileId && comment ? `${fileId}#${comment}` : null;
    if (link && link !== this.followedLink) this.panel.focusThread(comment!);
    this.followedLink = link;
  }

  /** Closes the workspace on the current page by clearing `?file=`. */
  requestClose(): void {
    void this.router.navigate([], { queryParams: { file: null, comment: null }, queryParamsHandling: 'merge' });
  }

  setUnsaved(unsaved: boolean): void {
    const released = this._unsaved() && !unsaved;
    this._unsaved.set(unsaved);
    this.unsavedChanges.set(unsaved);
    if (released) this._band.set(this.measureBand());
  }

  toggleExpanded(): void {
    this.patchStored({ expanded: !this.expanded() });
  }

  setDockedWidth(px: number): void {
    this.patchStored({ dockedWidth: this.clampDockedWidth(px) });
  }

  /** The file last open in chatId, if the user did not close it. */
  rememberedFileFor(chatId: string): string | null {
    return this.stored().byChat[chatId]?.fileId ?? null;
  }

  private remember(chatId: string, fileId: string | null): void {
    const byChat = { ...this.stored().byChat };
    if (fileId) byChat[chatId] = { fileId };
    else delete byChat[chatId];
    this.patchStored({ byChat });
  }

  private patchStored(patch: Partial<StoredState>): void {
    const next = { ...this.stored(), ...patch };
    this.stored.set(next);
    writeStored(next);
  }

  private clampDockedWidth(px: number | undefined): number {
    const viewport = this.document.defaultView?.innerWidth ?? 1280;
    const fallback = Math.round(viewport * DOCKED_WIDTH_DEFAULT_FRACTION);
    const value = px && Number.isFinite(px) ? px : fallback;
    return Math.round(Math.min(viewport * DOCKED_WIDTH_MAX_FRACTION, Math.max(viewport * DOCKED_WIDTH_MIN_FRACTION, value)));
  }

  private measureBand(): WorkspaceBand {
    const view = this.document.defaultView;
    if (!view?.matchMedia) return 'large';
    if (view.matchMedia(LARGE_VIEWPORT_QUERY).matches) return 'large';
    if (view.matchMedia(MEDIUM_VIEWPORT_QUERY).matches) return 'medium';
    return 'phone';
  }
}

function readStored(): StoredState {
  try {
    const raw = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (!raw) return { byChat: {} };
    const parsed = JSON.parse(raw) as Partial<StoredState>;
    return { byChat: parsed.byChat ?? {}, dockedWidth: parsed.dockedWidth, expanded: parsed.expanded };
  } catch {
    return { byChat: {} };
  }
}

function writeStored(state: StoredState): void {
  try {
    globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // Storage can be unavailable (private mode, quota); the workspace still works for the session.
  }
}

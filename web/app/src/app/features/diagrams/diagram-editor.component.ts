import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { saveBlobAsFile } from '../../core/utils/download.helpers';
import { ButtonComponent } from '../../shared/ui/button/button.component';
import { COMMENT_SAVER, FileEditor } from '../file-workspace/editors/file-editor';
import { TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { TextCommentThread, TextCommentsService } from '../file-workspace/text-comments/text-comments.service';
import { CommentAgentService } from '../office/comments/comment-agent.service';
import { CommentPeopleService } from '../office/comments/comment-people.service';
import { CommentsPanelComponent } from '../office/comments/comments-panel.component';
import { CommentsPanelState } from '../office/comments/comments-panel-state.service';
import { DiagramComments } from './diagram-comments';
import { DIAGRAMS_URL } from './diagram-site.config';
import { BridgeEvent, DrawioAction, dataUrlToBlob, drawioUrl, exportName, readDrawioMessage } from './drawio-frame';

const DIAGRAM_MIME = 'application/vnd.jgraph.mxfile';
/** How long an image export may take before it is given up on. */
const EXPORT_TIMEOUT_MS = 30_000;

type ImageFormat = 'png' | 'svg';

/**
 * A draw.io diagram (.drawio) in the file workspace (ADR 0x049): the draw.io editor, from its own
 * site (DIAGRAMS_URL), in an iframe. Viewing shows the diagram with no menus or panels; editing
 * is the full editor. One component does both (editInPlace), as the office editors do.
 *
 * The file is plain XML. It goes to the editor once it says it is ready, and every change comes
 * back whole (autosave), so `content()` is the latest of those; saving is the workspace's.
 *
 * Comments are kept beside the file, as for text and images (ADR 0x045), on the diagram's elements
 * or the whole diagram (DiagramComments). A viewer writes them as they are made; an editor writes
 * them once a save has stored the diagram, so a thread on a new element finds it there.
 */
@Component({
  selector: 'app-diagram-editor',
  standalone: true,
  imports: [ButtonComponent, CommentsPanelComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './diagram-editor.component.html',
  styleUrl: './diagram-editor.component.scss',
})
export class DiagramEditorComponent implements FileEditor {
  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(true);
  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready'>('loading');
  /** The image being made for a download, if any. */
  readonly exporting = signal<ImageFormat | null>(null);
  readonly exportFailed = signal(false);

  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  private readonly origin = this.site ? new URL(this.site).origin : null;
  private readonly sanitizer = inject(DomSanitizer);
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  /** The frame's URL, fixed for this component: a viewer and an editor are separate instances. */
  readonly frameUrl = computed(() => {
    if (!this.site) return null;
    const dark = this.document.documentElement.getAttribute('data-theme') === 'dark';
    // The site was checked to be https (or local) on another origin (usableOwnOriginSite).
    return this.sanitizer.bypassSecurityTrustResourceUrl(drawioUrl(this.site, { readOnly: this.readOnly(), dark }));
  });

  /** The XML the file holds as last loaded or saved, as the editor writes it. */
  private baseline = '';
  /** The XML the editor holds now. */
  private latest = '';
  /** Text from the workspace waiting for the editor to be ready. */
  private pending: string | null = null;
  private ready = false;
  /** Until the workspace's bytes are read, the editor waits for them rather than load nothing. */
  private haveBytes = false;
  /** What each blob content() handed out holds, so markSaved knows what was saved. */
  private readonly handedOut = new WeakMap<Blob, string>();
  // --- comments (ADR 0x045, 0x049): on elements, kept beside the file ------------------------------
  private readonly saver = inject(COMMENT_SAVER, { optional: true });
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new TextCommentsState(
    inject(TextCommentsService),
    this.me,
    () => this.commentsChanged(),
    () => this.saver?.showCurrentContent(),
    inject(CommentAgentService).hooks(() => this.file()),
  );
  private readonly panel = inject(CommentsPanelState);
  /** Viewing, comments can be written when the user may change the file; editing, a save writes them. */
  readonly canComment = computed(() => this.comments.available() && (this.readOnly() ? !!this.saver?.canComment() : true));
  readonly cellComments = new DiagramComments(
    this.comments,
    action => this.send(action),
    () => this.panel.open.set(true),
    () => this.canComment(),
  );
  readonly commentsOpen = computed(() => this.comments.available() && (this.panel.open() ?? this.comments.openCount() > 0));
  readonly commentTitle = computed(() => {
    const cell = this.cellComments.chosen();
    return cell ? `Comment on “${cell.label || 'the element'}”` : 'Comment on the diagram (choose an element to comment on it)';
  });
  /** Comments changed while editing that a save has not written. */
  private commentsDirty = false;
  /** Whether the file has threads stored, so a save that removed the last one still writes. */
  private hadComments = false;
  /** The threads (and mentions) as they were when each blob handed to the host was made. */
  private readonly handedOutComments = new WeakMap<Blob, { threads: TextCommentThread[]; mentions: number }>();

  /** Exports asked for, by request id, waiting for the editor's answer. */
  private readonly exports = new Map<string, (data: string | null) => void>();
  private nextExport = 0;

  constructor() {
    effect(() => {
      const blob = this.blob();
      untracked(() => {
        void this.take(blob);
        void this.loadComments();
      });
    });
    // The editor shows the threads' elements marked.
    effect(() => {
      const action = this.cellComments.marksAction();
      if (this.state() === 'ready') untracked(() => this.send(action));
    });
    const onMessage = (event: MessageEvent) => this.onMessage(event);
    this.document.defaultView?.addEventListener('message', onMessage);
    inject(DestroyRef).onDestroy(() => {
      this.document.defaultView?.removeEventListener('message', onMessage);
      for (const resolve of this.exports.values()) resolve(null);
    });
  }

  /**
   * Downloads the diagram as it is shown now as an image, made by the editor. PNG and SVG both
   * carry the diagram itself, so draw.io opens either one again as an editable diagram.
   */
  async download(format: ImageFormat): Promise<void> {
    if (this.exporting()) return;
    this.exporting.set(format);
    this.exportFailed.set(false);
    try {
      const data = await this.requestExport(format);
      const blob = data ? dataUrlToBlob(data) : null;
      if (blob) saveBlobAsFile(blob, exportName(this.file().name, format));
      else this.exportFailed.set(true);
    } finally {
      this.exporting.set(null);
    }
  }

  private requestExport(format: ImageFormat): Promise<string | null> {
    const requestId = `whatiff-export-${++this.nextExport}`;
    return new Promise(resolve => {
      const timer = setTimeout(() => finish(null), EXPORT_TIMEOUT_MS);
      const finish = (data: string | null) => {
        clearTimeout(timer);
        this.exports.delete(requestId);
        resolve(data);
      };
      this.exports.set(requestId, finish);
      this.send(
        format === 'png'
          ? { action: 'export', format: 'xmlpng', scale: 2, background: '#ffffff', requestId }
          : { action: 'export', format: 'xmlsvg', background: '#ffffff', requestId },
      );
    });
  }

  async content(): Promise<Blob> {
    const blob = new Blob([this.latest], { type: this.file().file_type || DIAGRAM_MIME });
    this.handedOut.set(blob, this.latest);
    this.handedOutComments.set(blob, { threads: this.comments.snapshot(), mentions: this.comments.mentionMark() });
    return blob;
  }

  markSaved(saved?: Blob): void {
    this.baseline = (saved && this.handedOut.get(saved)) ?? this.latest;
    this.setDirty(this.latest !== this.baseline || this.commentsDirty);
  }

  /** Writes the comments, checked against the diagram the save stored (FileEditor.saved). */
  async saved(file: FileAttachment, content: Blob): Promise<void> {
    const handed = this.handedOutComments.get(content);
    const threads = handed?.threads ?? this.comments.snapshot();
    // A file that never had comments has nothing to write.
    if (!threads.length && !this.hadComments) return;
    await this.comments.save(file.content_version ?? this.comments.placedOn, threads, handed?.mentions);
    this.hadComments = threads.length > 0;
    // Comments changed while the save ran are still unsaved.
    this.commentsDirty = this.comments.unsaved();
    this.setDirty(this.latest !== this.baseline || this.commentsDirty);
  }

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  private commentsChanged(): void {
    // A viewer writes comments as they are made; an editor with its next save.
    if (this.readOnly()) {
      this.comments.autosave();
      return;
    }
    this.commentsDirty = true;
    this.setDirty(true);
  }

  /** Reads the file's threads, unless comments made here are still to be written. */
  private async loadComments(): Promise<void> {
    if (this.commentsDirty || this.comments.unsaved()) return;
    try {
      await this.comments.load(this.file().id);
      this.hadComments = this.comments.threads().length > 0;
    } catch {
      // A file the server cannot keep comments for has none.
    }
  }

  /** New bytes from the workspace: the file as loaded, saved, or changed elsewhere (an agent). */
  private async take(blob: Blob): Promise<void> {
    const text = await blob.text();
    this.haveBytes = true;
    // After a save the workspace hands back what this editor wrote.
    if (text === this.latest && this.ready) return;
    if (this.dirty()) return;
    this.pending = text;
    if (this.ready) this.loadPending();
  }

  private loadPending(): void {
    if (this.pending === null) return;
    const xml = this.pending;
    this.pending = null;
    this.send({ action: 'load', xml, autosave: 1, title: this.file().name });
  }

  private send(message: DrawioAction): void {
    const target = this.frame()?.nativeElement.contentWindow;
    if (target && this.origin) target.postMessage(JSON.stringify(message), this.origin);
  }

  private onMessage(event: MessageEvent): void {
    if (!this.origin) return;
    const message = readDrawioMessage(event, this.origin, this.frame()?.nativeElement.contentWindow);
    if (!message) return;
    switch (message.event) {
      case 'init':
        this.ready = true;
        // Bytes not read yet are loaded once they are (take); otherwise what the editor held.
        if (this.haveBytes) this.pending ??= this.latest;
        this.loadPending();
        break;
      case 'load':
        // The file as the editor writes it, so a save with no change saves it unchanged.
        this.baseline = this.latest = message.xml;
        this.setDirty(this.commentsDirty);
        this.state.set('ready');
        break;
      case 'autosave':
        if (this.readOnly()) return;
        this.latest = message.xml;
        this.setDirty(this.latest !== this.baseline || this.commentsDirty);
        break;
      case 'save':
        if (this.readOnly()) return;
        this.latest = message.xml;
        this.setDirty(this.latest !== this.baseline || this.commentsDirty);
        this.saveRequested.emit();
        break;
      case 'whatiff-page':
      case 'whatiff-selection':
      case 'whatiff-click':
      case 'whatiff-menu':
        this.cellComments.handle(message as BridgeEvent);
        // The editor drops its marks when it shows another page: they are sent again.
        if (message.event === 'whatiff-page') this.send(this.cellComments.marksAction());
        break;
      case 'export': {
        const requestId = message.message?.requestId;
        if (requestId) this.exports.get(requestId)?.(message.data || null);
        break;
      }
    }
  }

  private setDirty(dirty: boolean): void {
    if (this.dirty() === dirty) return;
    this.dirty.set(dirty);
    this.dirtyChange.emit(dirty);
  }
}

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
import { FileEditor } from '../file-workspace/editors/file-editor';
import { DIAGRAMS_URL } from './diagram-site.config';
import { DrawioAction, dataUrlToBlob, drawioUrl, exportName, readDrawioMessage } from './drawio-frame';

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
 */
@Component({
  selector: 'app-diagram-editor',
  standalone: true,
  imports: [ButtonComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (frameUrl(); as url) {
      @if (state() === 'loading') {
        <div class="diagram-editor__status" role="status">Opening the diagram…</div>
      } @else {
        <div class="diagram-editor__bar" role="toolbar" aria-label="Diagram">
          <span class="diagram-editor__bar-label">Download</span>
          <ui-button size="xs" variant="ghost" [loading]="exporting() === 'png'" [disabled]="!!exporting()" (activate)="download('png')"
            >PNG</ui-button
          >
          <ui-button size="xs" variant="ghost" [loading]="exporting() === 'svg'" [disabled]="!!exporting()" (activate)="download('svg')"
            >SVG</ui-button
          >
          @if (exportFailed()) {
            <span class="diagram-editor__bar-error" role="alert">Couldn't make the image.</span>
          }
        </div>
      }
      <iframe
        #frame
        class="diagram-editor__frame"
        [class.diagram-editor__frame--hidden]="state() === 'loading'"
        [src]="url"
        title="Diagram editor"
        sandbox="allow-scripts allow-same-origin allow-downloads allow-modals allow-forms"
        allow="clipboard-read; clipboard-write"
      ></iframe>
    } @else {
      <p class="diagram-editor__status">Diagrams can't be opened here: this server has no diagram editor set up.</p>
    }
  `,
  styles: [
    `
      :host {
        display: flex;
        flex: 1 1 auto;
        flex-direction: column;
        height: 100%;
        min-height: 0;
        position: relative;
      }

      .diagram-editor__bar {
        align-items: center;
        border-bottom: 1px solid var(--border);
        display: flex;
        flex: 0 0 auto;
        gap: 0.25rem;
        justify-content: flex-end;
        padding: 0.25rem 0.5rem;
      }

      .diagram-editor__bar-label {
        color: var(--text-secondary);
        font-size: 0.75rem;
        margin-right: 0.25rem;
      }

      .diagram-editor__bar-error {
        color: var(--danger);
        font-size: 0.75rem;
        margin-left: 0.5rem;
      }

      .diagram-editor__frame {
        border: 0;
        flex: 1 1 auto;
        /* A host that does not constrain the height (the gallery page) still gets a usable editor,
           as the office editors do. */
        min-height: clamp(360px, 75vh, 1000px);
        width: 100%;
      }

      .diagram-editor__frame--hidden {
        visibility: hidden;
      }

      .diagram-editor__status {
        color: var(--text-secondary);
        font-size: 0.875rem;
        inset: 0;
        padding: 1rem;
        position: absolute;
      }
    `,
  ],
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
  /** Exports asked for, by request id, waiting for the editor's answer. */
  private readonly exports = new Map<string, (data: string | null) => void>();
  private nextExport = 0;

  constructor() {
    effect(() => {
      const blob = this.blob();
      untracked(() => void this.take(blob));
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
    return blob;
  }

  markSaved(saved?: Blob): void {
    this.baseline = (saved && this.handedOut.get(saved)) ?? this.latest;
    this.setDirty(this.latest !== this.baseline);
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
        this.setDirty(false);
        this.state.set('ready');
        break;
      case 'autosave':
        if (this.readOnly()) return;
        this.latest = message.xml;
        this.setDirty(this.latest !== this.baseline);
        break;
      case 'save':
        if (this.readOnly()) return;
        this.latest = message.xml;
        this.setDirty(this.latest !== this.baseline);
        this.saveRequested.emit();
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

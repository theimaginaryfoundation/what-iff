import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  InjectionToken,
  afterNextRender,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { PDFJS_LOADER, type PdfJs } from '../viewers/pdfjs-loader';
import { FileEditor } from './file-editor';

type PdfViewerModule = typeof import('pdfjs-dist/web/pdf_viewer.mjs');
type PdfLoadingTask = ReturnType<PdfJs['getDocument']>;
type PdfDocument = Awaited<PdfLoadingTask['promise']>;
type ModifiedHooks = { onSetModified: (() => void) | null; onResetModified: (() => void) | null };

/** Loads PDF.js's viewer module (the page viewer with its annotation editor layers) on first use. */
export const PDFJS_VIEWER_LOADER = new InjectionToken<() => Promise<PdfViewerModule>>('PDFJS_VIEWER_LOADER', {
  providedIn: 'root',
  factory: () => () => import('pdfjs-dist/web/pdf_viewer.mjs'),
});

/** The editing tools offered (ADR 0x043 §2). Stamps (images) are a later addition. */
export type PdfTool = 'select' | 'text' | 'draw' | 'highlight';

const COLORS = ['#e02020', '#1f6feb', '#2da44e', '#d4a72c', '#000000'];

/**
 * A PDF with its annotations editable (ADR 0x043 §2): PDF.js's own viewer with the annotation
 * editor layers (free text, ink, highlight), saved as real annotation objects by saveDocument().
 * The host owns saving: this reports changes through the document's annotation storage and
 * hands over the new bytes.
 */
@Component({
  selector: 'app-pdf-editor',
  standalone: true,
  templateUrl: './pdf-editor.component.html',
  styleUrl: './pdf-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PdfEditorComponent implements FileEditor {
  private readonly document = inject(DOCUMENT);
  private readonly loadCore = inject(PDFJS_LOADER);
  private readonly loadViewer = inject(PDFJS_VIEWER_LOADER);
  private readonly container = viewChild.required<ElementRef<HTMLDivElement>>('container');
  private readonly viewerHost = viewChild.required<ElementRef<HTMLDivElement>>('viewerHost');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly tool = signal<PdfTool>('select');
  readonly color = signal(COLORS[0]);
  readonly colors = COLORS;
  readonly pageCount = signal(0);

  private pdfjs: PdfJs | null = null;
  private viewerModule: PdfViewerModule | null = null;
  private viewer: InstanceType<PdfViewerModule['PDFViewer']> | null = null;
  private eventBus: InstanceType<PdfViewerModule['EventBus']> | null = null;
  private doc: PdfDocument | null = null;
  private task: PdfLoadingTask | null = null;
  private rendered = false;
  private generation = 0;

  private lastContent: Blob | null = null;

  constructor() {
    afterNextRender(() => {
      this.rendered = true;
      void this.open(this.blob());
    });
    effect(() => {
      const blob = this.blob();
      if (!this.rendered) return;
      // After a save the host hands back this editor's own bytes; reopening them would discard
      // the undo history and anything changed since.
      if (blob === this.lastContent) return;
      untracked(() => void this.open(blob));
    });
    inject(DestroyRef).onDestroy(() => this.teardown());
  }

  async content(): Promise<Blob> {
    if (!this.doc) return this.blob();
    const bytes = await this.doc.saveDocument();
    this.lastContent = new Blob([bytes as BlobPart], { type: 'application/pdf' });
    return this.lastContent;
  }

  markSaved(): void {
    this.doc?.annotationStorage.resetModified();
    this.setDirty(false);
  }

  setTool(tool: PdfTool): void {
    this.tool.set(tool);
    this.applyTool();
  }

  setColor(color: string): void {
    this.color.set(color);
    this.applyColor();
  }

  private async open(blob: Blob): Promise<void> {
    const generation = ++this.generation;
    this.teardown();
    this.state.set('loading');
    try {
      [this.pdfjs, this.viewerModule] = await Promise.all([this.loadCore(this.document), this.loadViewer()]);
      const data = new Uint8Array(await blob.arrayBuffer());
      if (generation !== this.generation) return;
      const task = this.pdfjs.getDocument({ data });
      const doc: PdfDocument = await task.promise;
      if (generation !== this.generation) {
        await task.destroy();
        return;
      }
      this.doc = doc;
      this.task = task;
      const hooks = doc.annotationStorage as unknown as ModifiedHooks;
      hooks.onSetModified = () => this.setDirty(true);
      hooks.onResetModified = () => this.setDirty(false);
      const { PDFViewer, EventBus, SimpleLinkService } = this.viewerModule;
      const eventBus = new EventBus();
      const linkService = new SimpleLinkService();
      const viewer = new PDFViewer({
        container: this.container().nativeElement,
        viewer: this.viewerHost().nativeElement,
        eventBus,
        linkService,
        l10n: nullL10n() as never,
        annotationEditorMode: this.pdfjs.AnnotationEditorType.NONE,
      });
      linkService.setViewer(viewer);
      eventBus.on('pagesinit', () => {
        viewer.currentScaleValue = 'page-width';
        this.applyTool();
        this.applyColor();
      });
      // Set before setDocument: pagesinit may fire as soon as the document is in.
      this.viewer = viewer;
      this.eventBus = eventBus;
      viewer.setDocument(doc);
      linkService.setDocument(doc);
      this.pageCount.set(doc.numPages);
      this.setDirty(false);
      this.state.set('ready');
    } catch {
      if (generation === this.generation) this.state.set('error');
    }
  }

  private applyTool(): void {
    if (!this.viewer || !this.pdfjs) return;
    const types = this.pdfjs.AnnotationEditorType;
    const mode = { select: types.NONE, text: types.FREETEXT, draw: types.INK, highlight: types.HIGHLIGHT }[this.tool()];
    this.viewer.annotationEditorMode = { mode };
  }

  /** The colour for the tools that take one, sent the way the viewer's toolbar sends it. */
  private applyColor(): void {
    if (!this.eventBus || !this.pdfjs) return;
    const params = this.pdfjs.AnnotationEditorParamsType;
    for (const type of [params.FREETEXT_COLOR, params.INK_COLOR, params.HIGHLIGHT_COLOR]) {
      this.eventBus.dispatch('switchannotationeditorparams', { source: this, type, value: this.color() });
    }
  }

  private setDirty(dirty: boolean): void {
    if (this.dirty() === dirty) return;
    this.dirty.set(dirty);
    this.dirtyChange.emit(dirty);
  }

  private teardown(): void {
    this.viewer?.setDocument(null);
    this.viewer = null;
    this.eventBus = null;
    void this.task?.destroy();
    this.task = null;
    this.doc = null;
  }
}

/**
 * A localization service that returns the viewer's own fallback strings, so the editor never
 * fetches locale files (the app has none to serve).
 */
function nullL10n() {
  return {
    getLanguage: () => 'en-US',
    getDirection: () => 'ltr',
    get: async (ids: string | string[], _args?: unknown, fallback?: string) => fallback ?? (Array.isArray(ids) ? ids[0] : ids),
    translate: async () => undefined,
    translateOnce: async () => undefined,
    destroy: async () => undefined,
    pause: () => undefined,
    resume: () => undefined,
  };
}

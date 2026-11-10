import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, effect, inject, input, signal, untracked, viewChild } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { PDFJS_LOADER, type PdfJs } from './pdfjs-loader';

type PdfLoadingTask = ReturnType<PdfJs['getDocument']>;
type PdfDocument = Awaited<PdfLoadingTask['promise']>;
type PdfPage = Awaited<ReturnType<PdfDocument['getPage']>>;

const ZOOM_STEPS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];

/**
 * A PDF rendered by PDF.js (ADR 0x043 §2): every page on a canvas with a selectable text layer,
 * zoom, and the current page. It replaces the browser's own viewer, which the Android app's
 * WebView does not have. Annotations and search are a later step.
 */
@Component({
  selector: 'app-pdf-viewer',
  standalone: true,
  templateUrl: './pdf-viewer.component.html',
  styleUrl: './pdf-viewer.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PdfViewerComponent {
  private readonly document = inject(DOCUMENT);
  private readonly load = inject(PDFJS_LOADER);
  private readonly scroller = viewChild.required<ElementRef<HTMLElement>>('scroller');
  private readonly pagesHost = viewChild.required<ElementRef<HTMLElement>>('pages');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly pageCount = signal(0);
  readonly currentPage = signal(1);
  /** The zoom as a factor of "fit width"; 1 fills the viewer's width. */
  readonly zoom = signal(1);

  private pdfjs: PdfJs | null = null;
  private task: PdfLoadingTask | null = null;
  private pages: PdfPage[] = [];
  private fitWidthScale = 1;
  private observer: IntersectionObserver | null = null;
  private rendered = new Set<number>();
  private generation = 0;

  constructor() {
    effect(() => {
      const blob = this.blob();
      const generation = ++this.generation;
      untracked(() => void this.openDocument(blob, generation));
    });
    inject(DestroyRef).onDestroy(() => this.teardown());
  }

  zoomIn(): void {
    const next = ZOOM_STEPS.find(step => step > this.zoom() + 0.001);
    if (next) this.setZoom(next);
  }

  zoomOut(): void {
    const next = [...ZOOM_STEPS].reverse().find(step => step < this.zoom() - 0.001);
    if (next) this.setZoom(next);
  }

  fitWidth(): void {
    this.setZoom(1);
  }

  onScroll(): void {
    const host = this.scroller().nativeElement;
    const middle = host.scrollTop + host.clientHeight / 2;
    const shells = [...this.pagesHost().nativeElement.children] as HTMLElement[];
    const index = shells.findIndex(shell => shell.offsetTop + shell.offsetHeight >= middle);
    if (index >= 0) this.currentPage.set(index + 1);
  }

  private async openDocument(blob: Blob, generation: number): Promise<void> {
    this.teardown();
    this.state.set('loading');
    try {
      this.pdfjs ??= await this.load(this.document);
      const data = new Uint8Array(await blob.arrayBuffer());
      if (generation !== this.generation) return;
      const task = this.pdfjs.getDocument({ data });
      const doc: PdfDocument = await task.promise;
      if (generation !== this.generation) {
        await task.destroy();
        return;
      }
      this.task = task;
      this.pages = await Promise.all(Array.from({ length: doc.numPages }, (_, i) => doc.getPage(i + 1)));
      if (generation !== this.generation) return;
      this.pageCount.set(doc.numPages);
      this.currentPage.set(1);
      this.state.set('ready');
      this.layoutPages();
    } catch {
      if (generation === this.generation) this.state.set('error');
    }
  }

  /** Sizes a shell per page at the current zoom and renders the ones in view as they scroll in. */
  private layoutPages(): void {
    const host = this.pagesHost().nativeElement;
    host.replaceChildren();
    this.rendered.clear();
    this.observer?.disconnect();
    const width = Math.max(200, this.scroller().nativeElement.clientWidth - 32);
    const first = this.pages[0];
    this.fitWidthScale = first ? width / first.getViewport({ scale: 1 }).width : 1;
    const scale = this.fitWidthScale * this.zoom();
    this.pages.forEach((page, index) => {
      const viewport = page.getViewport({ scale });
      const shell = this.document.createElement('div');
      shell.className = 'pdf-viewer__page';
      shell.dataset['page'] = String(index + 1);
      shell.style.width = `${Math.floor(viewport.width)}px`;
      shell.style.height = `${Math.floor(viewport.height)}px`;
      shell.setAttribute('aria-label', `Page ${index + 1}`);
      host.appendChild(shell);
    });
    this.observer = new IntersectionObserver(
      entries => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          const index = Number((entry.target as HTMLElement).dataset['page']) - 1;
          void this.renderPage(index, scale);
        }
      },
      { root: this.scroller().nativeElement, rootMargin: '600px 0px' },
    );
    for (const shell of host.children) this.observer.observe(shell);
  }

  private async renderPage(index: number, scale: number): Promise<void> {
    if (this.rendered.has(index) || !this.pdfjs) return;
    this.rendered.add(index);
    const generation = this.generation;
    const page = this.pages[index];
    const shell = this.pagesHost().nativeElement.children[index] as HTMLElement | undefined;
    if (!page || !shell) return;
    const viewport = page.getViewport({ scale });
    const ratio = this.document.defaultView?.devicePixelRatio ?? 1;
    const canvas = this.document.createElement('canvas');
    canvas.width = Math.floor(viewport.width * ratio);
    canvas.height = Math.floor(viewport.height * ratio);
    canvas.style.width = `${Math.floor(viewport.width)}px`;
    canvas.style.height = `${Math.floor(viewport.height)}px`;
    const textLayer = this.document.createElement('div');
    textLayer.className = 'textLayer';
    shell.append(canvas, textLayer);
    try {
      await page.render({ canvas, viewport, transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] }).promise;
      if (generation !== this.generation) return;
      this.pdfjs.setLayerDimensions(textLayer, viewport);
      await new this.pdfjs.TextLayer({ textContentSource: page.streamTextContent(), container: textLayer, viewport }).render();
    } catch {
      // A page that fails to render leaves its shell blank; the rest of the document still shows.
    }
  }

  private setZoom(zoom: number): void {
    if (zoom === this.zoom()) return;
    this.zoom.set(zoom);
    if (this.state() === 'ready') this.layoutPages();
  }

  private teardown(): void {
    this.observer?.disconnect();
    this.observer = null;
    this.rendered.clear();
    void this.task?.destroy();
    this.task = null;
    this.pages = [];
  }
}

import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';

import { ButtonComponent } from '../../../shared/ui/button/button.component';
import { TagComponent } from '../../../shared/ui/tag/tag.component';
import {
  AREA_DISPLAY_ALPHA,
  AREA_DISPLAY_COLOR,
  CanvasFactory,
  MarkupStroke,
  MarkupTool,
  PEN_COLORS,
  SKETCH_SIZE,
  canvasToPng,
  documentCanvasFactory,
  drawStrokes,
  fitScale,
  hasTool,
  renderMarkup,
  renderMask,
  toImagePoint,
} from './markup-strokes';

/** What the person drew, as files: either can be missing when its tool was not used. */
export interface MarkupResult {
  /** White where the image may change (the area brush). Never set for a sketch. */
  mask: Blob | null;
  /** The image with the pen strokes on it; for a sketch, the sketch. */
  markup: Blob | null;
}

/** The pen colour a sketch starts with. */
const SKETCH_PEN_COLOR = '#111827';

/** Brush sizes on screen, in CSS pixels; strokes store them in image pixels. */
const DEFAULT_SIZES: Record<MarkupTool, number> = { area: 48, pen: 6 };
const MIN_SIZE = 2;
const MAX_SIZE = 160;

/**
 * Drawing on an image to steer image generation (ADR 0x045 §6–7): an area brush that becomes a
 * mask and a pen whose strokes become a marked-up copy. With no source it is a blank page for a
 * sketch, with the pen only. It never changes the image it is given; the host turns the result into
 * new files for the next message.
 */
@Component({
  selector: 'app-image-markup',
  standalone: true,
  imports: [ButtonComponent, TagComponent],
  template: `
    <div class="markup__toolbar" role="toolbar" aria-label="Markup tools">
      @if (!sketch()) {
        <ui-tag [selected]="tool() === 'area'" (toggle)="tool.set('area')">Area to change</ui-tag>
      }
      <ui-tag [selected]="tool() === 'pen'" (toggle)="tool.set('pen')">Pen</ui-tag>
      @if (tool() === 'pen') {
        <div class="markup__swatches" role="group" aria-label="Pen colour">
          @for (c of colors; track c.value) {
            <button
              type="button"
              class="markup__swatch"
              [style.background]="c.value"
              [attr.aria-label]="c.name"
              [attr.aria-pressed]="color() === c.value"
              (click)="color.set(c.value)"
            ></button>
          }
        </div>
      }
      <label class="markup__size">
        <span>Size</span>
        <input type="range" [min]="minSize" [max]="maxSize" [value]="size()" (input)="setSize($any($event.target).valueAsNumber)" />
      </label>
      <span class="markup__history">
        <ui-button size="xs" variant="ghost" [disabled]="strokes().length === 0" (activate)="undo()">Undo</ui-button>
        <ui-button size="xs" variant="ghost" [disabled]="strokes().length === 0" (activate)="clear()">Clear</ui-button>
      </span>
    </div>

    @if (notice(); as message) {
      <p class="markup__notice" role="status">{{ message }}</p>
    }

    <div #stage class="markup__stage">
      @if (state() === 'error') {
        <p class="markup__status" role="alert">This image could not be opened for markup.</p>
      } @else if (state() === 'loading') {
        <p class="markup__status" role="status">Loading…</p>
      }
      <canvas
        #view
        class="markup__canvas"
        [class.markup__canvas--hidden]="state() !== 'ready'"
        role="img"
        [attr.aria-label]="canvasLabel()"
        (pointerdown)="onPointerDown($event)"
        (pointermove)="onPointerMove($event)"
        (pointerup)="onPointerUp($event)"
        (pointercancel)="onPointerUp($event)"
      ></canvas>
    </div>

    <div class="markup__footer">
      <p class="markup__hint">{{ hint() }}</p>
      @if (error(); as message) {
        <p class="markup__error" role="alert">{{ message }}</p>
      }
      <div class="markup__actions">
        <ui-button size="sm" variant="secondary" [disabled]="exporting()" (activate)="cancel.emit()">Cancel</ui-button>
        <ui-button
          size="sm"
          variant="primary"
          [disabled]="strokes().length === 0 || busy()"
          [loading]="exporting() || busy()"
          (activate)="send()"
        >
          Add to message
        </ui-button>
      </div>
    </div>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
      }
      .markup__toolbar {
        align-items: center;
        border-bottom: 1px solid var(--border);
        display: flex;
        flex-wrap: wrap;
        gap: 0.5rem;
        padding: 0.5rem 1rem;
      }
      .markup__swatches {
        display: flex;
        gap: 0.25rem;
      }
      .markup__swatch {
        border: 1px solid var(--border);
        border-radius: 9999px;
        height: 1.25rem;
        width: 1.25rem;
      }
      .markup__swatch[aria-pressed='true'] {
        box-shadow:
          0 0 0 2px var(--bg-base),
          0 0 0 4px var(--accent);
      }
      .markup__swatch:focus-visible {
        outline: 2px solid var(--focus-ring);
        outline-offset: 2px;
      }
      .markup__size {
        align-items: center;
        color: var(--text-secondary);
        display: flex;
        font-size: 0.75rem;
        gap: 0.4rem;
      }
      .markup__size input {
        accent-color: var(--accent);
        width: 5rem;
      }
      .markup__history {
        display: flex;
        margin-left: auto;
      }
      .markup__notice {
        background: color-mix(in srgb, var(--warning) 12%, transparent);
        border-bottom: 1px solid var(--border);
        border-left: 3px solid var(--warning);
        color: var(--text-primary);
        font-size: 0.8rem;
        margin: 0;
        padding: 0.5rem 1rem;
      }
      .markup__stage {
        align-items: center;
        display: flex;
        flex: 1 1 auto;
        justify-content: center;
        min-height: 0;
        overflow: hidden;
        padding: 1rem;
        position: relative;
      }
      .markup__canvas {
        box-shadow: 0 0 0 1px var(--border);
        cursor: crosshair;
        touch-action: none;
      }
      .markup__canvas--hidden {
        visibility: hidden;
      }
      .markup__status {
        color: var(--text-secondary);
        position: absolute;
      }
      .markup__footer {
        align-items: center;
        border-top: 1px solid var(--border);
        display: flex;
        flex-wrap: wrap;
        gap: 0.5rem 1rem;
        padding: 0.5rem 1rem;
      }
      .markup__hint {
        color: var(--text-secondary);
        flex: 1 1 16rem;
        font-size: 0.8rem;
        margin: 0;
      }
      .markup__error {
        color: var(--danger);
        font-size: 0.8rem;
        margin: 0;
      }
      .markup__actions {
        display: flex;
        gap: 0.5rem;
        margin-left: auto;
      }
    `,
  ],
  host: { '(keydown)': 'onKeydown($event)' },
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageMarkupComponent {
  private readonly document = inject(DOCUMENT);
  private readonly stage = viewChild.required<ElementRef<HTMLElement>>('stage');
  private readonly view = viewChild.required<ElementRef<HTMLCanvasElement>>('view');

  /** The image to mark up; null is a blank page for a sketch. */
  readonly source = input<Blob | null>(null);
  /** The file's name, for labels. */
  readonly name = input('image');
  /** A warning shown above the image, such as an image vendor that cannot edit. */
  readonly notice = input<string | null>(null);
  /** The host is sending the result; the button waits. */
  readonly busy = input(false);
  /** Makes canvases for export; a seam for tests. */
  readonly canvasFactory = input<CanvasFactory | null>(null);

  readonly submitted = output<MarkupResult>();
  readonly cancel = output<void>();
  /** Whether there is drawing that leaving would lose. */
  readonly dirtyChange = output<boolean>();

  readonly colors = PEN_COLORS;
  readonly minSize = MIN_SIZE;
  readonly maxSize = MAX_SIZE;
  readonly sketch = computed(() => this.source() === null);
  readonly tool = signal<MarkupTool>('area');
  readonly color = signal(PEN_COLORS[0].value);
  readonly sizes = signal<Record<MarkupTool, number>>({ ...DEFAULT_SIZES });
  readonly size = computed(() => this.sizes()[this.tool()]);
  readonly strokes = signal<readonly MarkupStroke[]>([]);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly exporting = signal(false);
  readonly error = signal<string | null>(null);

  readonly canvasLabel = computed(() =>
    this.sketch() ? 'Sketch page. Draw with a mouse, pen or finger.' : `${this.name()} for markup. Draw with a mouse, pen or finger.`,
  );
  readonly hint = computed(() => {
    if (this.sketch()) return 'Sketch the layout you want, then add it to your message and say what to make of it.';
    return this.tool() === 'area'
      ? 'Paint over the part to change, then add it to your message and say what should go there.'
      : 'Circle, point and write notes on the image, then add it to your message and ask for the change.';
  });

  private image: HTMLImageElement | null = null;
  private objectUrl: string | null = null;
  /** The image's size in its own pixels. */
  private width = SKETCH_SIZE;
  private height = SKETCH_SIZE;
  /** Screen pixels per image pixel. */
  private scale = 1;
  private drawing: MarkupStroke | null = null;
  private frame = 0;
  private rendered = false;
  private resizeObserver: ResizeObserver | null = null;

  constructor() {
    effect(() => {
      const blob = this.source();
      if (!this.rendered) return;
      untracked(() => void this.load(blob));
    });
    effect(() => {
      // A sketch has no image to mask, so only the pen, and it starts in black: on a white page,
      // the lines are the drawing rather than notes on one.
      if (this.sketch()) {
        untracked(() => {
          this.tool.set('pen');
          this.color.set(SKETCH_PEN_COLOR);
        });
      }
    });
    effect(() => {
      const dirty = this.strokes().length > 0;
      untracked(() => this.dirtyChange.emit(dirty));
    });
    afterNextRender(() => {
      this.rendered = true;
      if (typeof ResizeObserver !== 'undefined') {
        this.resizeObserver = new ResizeObserver(() => this.layout());
        this.resizeObserver.observe(this.stage().nativeElement);
      }
      void this.load(this.source());
    });
    inject(DestroyRef).onDestroy(() => {
      this.resizeObserver?.disconnect();
      if (this.frame) this.document.defaultView?.cancelAnimationFrame(this.frame);
      this.revoke();
    });
  }

  setSize(value: number): void {
    if (!Number.isFinite(value)) return;
    const size = Math.min(MAX_SIZE, Math.max(MIN_SIZE, value));
    this.sizes.update(sizes => ({ ...sizes, [this.tool()]: size }));
  }

  undo(): void {
    this.strokes.update(strokes => strokes.slice(0, -1));
    this.redraw();
  }

  clear(): void {
    this.strokes.set([]);
    this.redraw();
  }

  onKeydown(event: KeyboardEvent): void {
    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'z' && this.strokes().length > 0) {
      event.preventDefault();
      this.undo();
    }
  }

  // --- drawing -----------------------------------------------------------------------------------

  onPointerDown(event: PointerEvent): void {
    if (this.state() !== 'ready' || event.button > 0 || this.scale <= 0) return;
    event.preventDefault();
    (event.target as Element).setPointerCapture?.(event.pointerId);
    const tool = this.tool();
    const [x, y] = this.pointOf(event);
    this.drawing = { tool, color: this.color(), width: this.size() / this.scale, points: [x, y] };
    this.strokes.update(strokes => [...strokes, this.drawing!]);
    this.error.set(null);
    this.scheduleRedraw();
  }

  onPointerMove(event: PointerEvent): void {
    const stroke = this.drawing;
    if (!stroke) return;
    const events = typeof event.getCoalescedEvents === 'function' ? event.getCoalescedEvents() : [];
    for (const e of events.length ? events : [event]) {
      const [x, y] = this.pointOf(e);
      stroke.points.push(x, y);
    }
    this.scheduleRedraw();
  }

  onPointerUp(event: PointerEvent): void {
    if (!this.drawing) return;
    (event.target as Element).releasePointerCapture?.(event.pointerId);
    this.drawing = null;
    // A new array, so anything reading strokes() sees the finished stroke.
    this.strokes.update(strokes => [...strokes]);
    this.redraw();
  }

  private pointOf(event: PointerEvent): [number, number] {
    const rect = this.view().nativeElement.getBoundingClientRect();
    return toImagePoint(event.clientX, event.clientY, rect, this.scale);
  }

  // --- export ------------------------------------------------------------------------------------

  async send(): Promise<void> {
    if (this.exporting() || this.strokes().length === 0) return;
    this.exporting.set(true);
    this.error.set(null);
    try {
      this.submitted.emit(await this.export());
    } catch (error) {
      this.error.set(error instanceof Error && error.message ? error.message : 'The drawing could not be saved.');
    } finally {
      this.exporting.set(false);
    }
  }

  /** The drawing as files at the image's full size. */
  async export(): Promise<MarkupResult> {
    const make = this.canvasFactory() ?? documentCanvasFactory(this.document);
    const strokes = this.strokes();
    if (this.sketch()) {
      return { mask: null, markup: await canvasToPng(renderMarkup(make, null, strokes, this.width, this.height)) };
    }
    const mask = hasTool(strokes, 'area') ? await canvasToPng(renderMask(make, strokes, this.width, this.height)) : null;
    const markup = hasTool(strokes, 'pen') ? await canvasToPng(renderMarkup(make, this.image, strokes, this.width, this.height)) : null;
    return { mask, markup };
  }

  // --- view --------------------------------------------------------------------------------------

  private async load(blob: Blob | null): Promise<void> {
    this.revoke();
    this.image = null;
    this.strokes.set([]);
    if (!blob) {
      this.width = SKETCH_SIZE;
      this.height = SKETCH_SIZE;
      this.state.set('ready');
      this.layout();
      return;
    }
    this.state.set('loading');
    const url = URL.createObjectURL(blob);
    this.objectUrl = url;
    try {
      const image = await loadImage(this.document, url);
      if (this.objectUrl !== url) return;
      this.image = image;
      this.width = image.naturalWidth;
      this.height = image.naturalHeight;
      this.state.set('ready');
      this.layout();
    } catch {
      if (this.objectUrl === url) this.state.set('error');
    }
  }

  /** Sizes the canvas to fit the stage, then redraws. */
  private layout(): void {
    const stage = this.stage().nativeElement;
    const style = this.document.defaultView?.getComputedStyle(stage);
    const padX = style ? parseFloat(style.paddingLeft) + parseFloat(style.paddingRight) : 0;
    const padY = style ? parseFloat(style.paddingTop) + parseFloat(style.paddingBottom) : 0;
    const scale = fitScale(this.width, this.height, stage.clientWidth - (padX || 0), stage.clientHeight - (padY || 0));
    if (scale <= 0) return;
    this.scale = scale;
    const canvas = this.view().nativeElement;
    const dpr = this.document.defaultView?.devicePixelRatio ?? 1;
    const cssWidth = Math.round(this.width * scale);
    const cssHeight = Math.round(this.height * scale);
    canvas.style.width = `${cssWidth}px`;
    canvas.style.height = `${cssHeight}px`;
    canvas.width = Math.round(cssWidth * dpr);
    canvas.height = Math.round(cssHeight * dpr);
    this.redraw();
  }

  private scheduleRedraw(): void {
    const view = this.document.defaultView;
    if (!view?.requestAnimationFrame) {
      this.redraw();
      return;
    }
    if (this.frame) return;
    this.frame = view.requestAnimationFrame(() => {
      this.frame = 0;
      this.redraw();
    });
  }

  /** Draws the image, the area strokes as one see-through layer, then the pen strokes. */
  private redraw(): void {
    const canvas = this.view().nativeElement;
    const ctx = canvas.getContext('2d');
    if (!ctx || canvas.width === 0) return;
    const ratio = canvas.width / this.width;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    if (this.image) {
      ctx.drawImage(this.image, 0, 0, canvas.width, canvas.height);
    } else {
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, canvas.width, canvas.height);
    }
    const strokes = this.strokes();
    if (hasTool(strokes, 'area')) {
      // Painted solid on its own layer and laid on see-through, so overlapping strokes do not darken.
      const layer = this.document.createElement('canvas');
      layer.width = canvas.width;
      layer.height = canvas.height;
      const layerCtx = layer.getContext('2d');
      if (layerCtx) {
        layerCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
        drawStrokes(layerCtx, strokes, 'area', AREA_DISPLAY_COLOR);
        ctx.globalAlpha = AREA_DISPLAY_ALPHA;
        ctx.drawImage(layer, 0, 0);
        ctx.globalAlpha = 1;
      }
    }
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    drawStrokes(ctx, strokes, 'pen');
    ctx.setTransform(1, 0, 0, 1, 0, 0);
  }

  private revoke(): void {
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl);
      this.objectUrl = null;
    }
  }
}

function loadImage(doc: Document, url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = doc.createElement('img');
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error('image failed to load'));
    image.src = url;
  });
}

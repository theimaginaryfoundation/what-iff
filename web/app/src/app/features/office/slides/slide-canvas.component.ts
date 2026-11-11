import { NgStyle, NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, afterRenderEffect, computed, inject, input, output, signal } from '@angular/core';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';

import { Border, ChartElement, Fill, Paragraph, Slide, SlideElement, TableCell, TextBody } from '../pptx/deck-model';
import { chartSvgDataUrl } from '../shared/chart-svg';
import { isLinePreset, shapePath } from './shape-paths';
import { domToParagraphs, paragraphsToHtml } from './text-dom';

/** Points to CSS pixels: the slide is laid out in points and scaled to fit. */
export const PX_PER_PT = 96 / 72;
const PIN_SIZE_PX = 22;

/** A comment thread's marker on the slide. */
export interface CommentPin {
  id: string;
  /** Slide points: where the pin's tail points. */
  x: number;
  y: number;
  active: boolean;
  label: string;
}

export type ResizeHandle = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';
const HANDLES: readonly ResizeHandle[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

export interface ElementGeometry {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * One slide drawn at a scale (ADR 0x043, office suite): shapes as SVG, text as HTML in points,
 * images, tables, charts and groups. Interactive, it selects, drags and resizes elements and
 * edits text in place; the deck itself belongs to the editor, which applies the changes.
 */
@Component({
  selector: 'app-slide-canvas',
  standalone: true,
  imports: [NgStyle, NgTemplateOutlet],
  templateUrl: './slide-canvas.component.html',
  styleUrl: './slide-canvas.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SlideCanvasComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly sanitizer = inject(DomSanitizer);

  readonly slide = input.required<Slide>();
  readonly size = input.required<{ width: number; height: number }>();
  readonly scale = input(1);
  readonly interactive = input(false);
  readonly selectedId = input<string | null>(null);
  readonly editingId = input<string | null>(null);
  /** Drawn for the rail: no handles, no pointer handling, a label for assistive tech. */
  readonly label = input<string>('');
  /** Comment threads' pins (ADR 0x045), in slide points. */
  readonly pins = input<readonly CommentPin[]>([]);
  /** The element the active comment thread is about, outlined. */
  readonly highlightId = input<string | null>(null);

  readonly select = output<string | null>();
  /** The user started dragging or resizing: a point for the editor's undo history. */
  readonly gestureStart = output<string>();
  readonly geometryChange = output<{ id: string; geometry: ElementGeometry }>();
  readonly rotationChange = output<{ id: string; rotate: number }>();
  readonly requestEdit = output<string>();
  readonly textCommit = output<{ id: string; paragraphs: Paragraph[]; cell?: { row: number; col: number } }>();
  readonly open = output<string>();
  /** A comment pin was chosen. */
  readonly pinSelect = output<string>();

  readonly handles = HANDLES;
  readonly cssWidth = computed(() => this.size().width * PX_PER_PT * this.scale());
  readonly cssHeight = computed(() => this.size().height * PX_PER_PT * this.scale());
  /** A slide with no background of its own is white, whatever the app's theme. */
  readonly background = computed(() => {
    const bg = this.slide().background;
    return bg ? fillCss(bg) : '#FFFFFF';
  });
  /** Geometry overrides while a drag or resize is in flight, so the deck is updated once at the end. */
  readonly liveGeometry = signal<{ id: string; geometry: ElementGeometry } | null>(null);
  /** The angle while a rotation is in flight. */
  readonly liveRotation = signal<{ id: string; rotate: number } | null>(null);

  /**
   * One value per text body, which is replaced rather than mutated on an edit. A new value on each
   * check would make Angular set innerHTML again, wiping what is being typed and the caret with it.
   */
  private readonly htmlByBody = new WeakMap<TextBody, SafeHtml>();
  private readonly emptyHtml = this.sanitizer.bypassSecurityTrustHtml('');

  private gesture: {
    id: string;
    kind: 'move' | 'resize' | 'rotate';
    handle?: ResizeHandle;
    startX: number;
    startY: number;
    start: ElementGeometry;
    pointerId: number;
    centre?: { x: number; y: number };
    startRotate?: number;
  } | null = null;

  constructor() {
    // Once the element being edited is rendered editable, put the caret at its end.
    afterRenderEffect(() => {
      const id = this.editingId();
      if (!id) return;
      const target = this.host.nativeElement.querySelector<HTMLElement>(`[data-element-id="${CSS.escape(id)}"] [contenteditable="true"]`);
      if (target && this.host.nativeElement.ownerDocument.activeElement !== target) this.focusText(target);
    });
  }

  geometryOf(el: SlideElement): ElementGeometry {
    const live = this.liveGeometry();
    return live && live.id === el.id ? live.geometry : { left: el.left, top: el.top, width: el.width, height: el.height };
  }

  transformOf(el: SlideElement): string {
    const parts: string[] = [];
    const live = this.liveRotation();
    const rotate = live && live.id === el.id ? live.rotate : el.rotate;
    if (rotate) parts.push(`rotate(${rotate}deg)`);
    if (el.flipH || el.flipV) parts.push(`scale(${el.flipH ? -1 : 1}, ${el.flipV ? -1 : 1})`);
    return parts.join(' ');
  }

  pathOf(el: SlideElement, g: ElementGeometry): string {
    if (el.kind === 'line') return `M0,0L${g.width},${g.height}`;
    if (el.kind !== 'shape') return '';
    if (el.path && el.viewBox && g.width === el.width && g.height === el.height) return el.path;
    return shapePath(el.shapeType, Math.max(1, g.width), Math.max(1, g.height), el.adjustments);
  }

  viewBoxOf(el: SlideElement, g: ElementGeometry): string {
    if (el.kind === 'shape' && el.path && el.viewBox && g.width === el.width && g.height === el.height)
      return `${el.viewBox.x} ${el.viewBox.y} ${Math.max(1, el.viewBox.width)} ${Math.max(1, el.viewBox.height)}`;
    return `0 0 ${Math.max(1, g.width)} ${Math.max(1, g.height)}`;
  }

  isLine(el: SlideElement): boolean {
    return el.kind === 'line' || (el.kind === 'shape' && isLinePreset(el.shapeType));
  }

  fillCss(fill: Fill | null): string {
    return fillCss(fill);
  }

  svgFill(el: SlideElement): string {
    if (el.kind !== 'shape' || !el.fill) return 'none';
    if (el.fill.type === 'color') return el.fill.color;
    if (el.fill.type === 'gradient') return `url(#grad-${el.id})`;
    return 'none';
  }

  strokeOf(border: Border | null): { color: string; width: number; dash: string | null } {
    if (!border) return { color: 'none', width: 0, dash: null };
    return { color: border.color, width: border.width, dash: border.style === 'dashed' ? '6 4' : border.style === 'dotted' ? '2 3' : null };
  }

  /**
   * The text as HTML. It is built from the model (text escaped, styles from known fields), so
   * it bypasses the sanitiser, which would otherwise strip the inline styles that carry colours
   * and sizes.
   */
  textHtml(body: TextBody | null): SafeHtml {
    if (!body) return this.emptyHtml;
    let html = this.htmlByBody.get(body);
    if (!html) {
      html = this.sanitizer.bypassSecurityTrustHtml(paragraphsToHtml(body.paragraphs));
      this.htmlByBody.set(body, html);
    }
    return html;
  }

  imageFillOf(el: SlideElement): { src: string; opacity: number } | null {
    if (el.kind !== 'shape' || el.fill?.type !== 'image') return null;
    return { src: el.fill.src, opacity: el.fill.opacity ?? 1 };
  }

  gradientOf(el: SlideElement): { id: string; angle: number; stops: { pos: number; color: string }[] } | null {
    if (el.kind !== 'shape' || el.fill?.type !== 'gradient') return null;
    return { id: `grad-${el.id}`, angle: el.fill.angle, stops: el.fill.stops };
  }

  textStyle(body: TextBody): Record<string, string> {
    return {
      padding: `${body.inset[1]}pt ${body.inset[2]}pt ${body.inset[3]}pt ${body.inset[0]}pt`,
      'justify-content': body.vAlign === 'middle' ? 'center' : body.vAlign === 'bottom' ? 'flex-end' : 'flex-start',
      'white-space': body.wrap ? 'pre-wrap' : 'pre',
    };
  }

  imageStyle(el: SlideElement): Record<string, string> {
    if (el.kind !== 'image' || !el.crop) return {};
    const { top, bottom, left, right } = el.crop;
    const w = 100 / Math.max(0.01, 1 - left - right);
    const h = 100 / Math.max(0.01, 1 - top - bottom);
    return { width: `${w}%`, height: `${h}%`, left: `-${left * w}%`, top: `-${top * h}%`, position: 'absolute', 'max-width': 'none' };
  }

  cellStyle(cell: TableCell): Record<string, string> {
    const b = (border: Border | undefined) => (border ? `${border.width}pt ${border.style} ${border.color}` : 'none');
    return {
      background: cell.fill?.type === 'color' ? cell.fill.color : 'transparent',
      'border-top': b(cell.borders.top),
      'border-bottom': b(cell.borders.bottom),
      'border-left': b(cell.borders.left),
      'border-right': b(cell.borders.right),
      'vertical-align': cell.text.vAlign === 'middle' ? 'middle' : cell.text.vAlign === 'bottom' ? 'bottom' : 'top',
      padding: `${cell.text.inset[1]}pt ${cell.text.inset[2]}pt ${cell.text.inset[3]}pt ${cell.text.inset[0]}pt`,
    };
  }

  // --- charts (a plain rendering; the data is what gets saved) ------------------------------------

  chartImage(el: ChartElement, g: ElementGeometry): string {
    return chartSvgDataUrl(
      {
        type: el.chartType,
        barDirection: el.barDirection,
        series: el.series,
        colors: el.colors,
        holeSize: el.holeSize,
        grouping: el.grouping,
      },
      { width: Math.max(40, g.width), height: Math.max(30, g.height) },
    );
  }

  // --- interaction -------------------------------------------------------------------------------

  /** A pin's place on screen, kept inside the slide so a pin at its edge stays clickable. */
  pinStyle(pin: CommentPin): { left: number; top: number } {
    const scale = PX_PER_PT * this.scale();
    return {
      left: Math.max(0, Math.min(pin.x * scale, this.cssWidth() - PIN_SIZE_PX)),
      top: Math.max(0, Math.min(pin.y * scale - PIN_SIZE_PX, this.cssHeight() - PIN_SIZE_PX)),
    };
  }

  onBackgroundPointerDown(event: PointerEvent): void {
    if (!this.interactive()) return;
    if ((event.target as HTMLElement).closest('.slide__element')) return;
    this.select.emit(null);
  }

  onElementPointerDown(event: PointerEvent, el: SlideElement): void {
    if (!this.interactive()) return;
    if (this.editingId() === el.id) return;
    event.stopPropagation();
    if (this.selectedId() !== el.id) this.select.emit(el.id);
    if (event.button !== 0) return;
    this.beginGesture(event, el, 'move');
  }

  onRotatePointerDown(event: PointerEvent, el: SlideElement): void {
    if (!this.interactive()) return;
    event.stopPropagation();
    if (event.button !== 0) return;
    event.preventDefault();
    const target = event.currentTarget as HTMLElement;
    target.setPointerCapture?.(event.pointerId);
    const box = (target.parentElement as HTMLElement).getBoundingClientRect();
    this.gesture = {
      id: el.id,
      kind: 'rotate',
      startX: event.clientX,
      startY: event.clientY,
      start: { left: el.left, top: el.top, width: el.width, height: el.height },
      pointerId: event.pointerId,
      centre: { x: box.left + box.width / 2, y: box.top + box.height / 2 },
      startRotate: el.rotate,
    };
    this.gestureStart.emit(el.id);
  }

  onHandlePointerDown(event: PointerEvent, el: SlideElement, handle: ResizeHandle): void {
    if (!this.interactive()) return;
    event.stopPropagation();
    if (event.button !== 0) return;
    this.beginGesture(event, el, 'resize', handle);
  }

  private beginGesture(event: PointerEvent, el: SlideElement, kind: 'move' | 'resize', handle?: ResizeHandle): void {
    event.preventDefault();
    const target = event.currentTarget as HTMLElement;
    target.setPointerCapture?.(event.pointerId);
    this.gesture = {
      id: el.id,
      kind,
      handle,
      startX: event.clientX,
      startY: event.clientY,
      start: { left: el.left, top: el.top, width: el.width, height: el.height },
      pointerId: event.pointerId,
    };
    this.gestureStart.emit(el.id);
  }

  onPointerMove(event: PointerEvent): void {
    const g = this.gesture;
    if (!g || event.pointerId !== g.pointerId) return;
    if (g.kind === 'rotate' && g.centre) {
      const angle = (Math.atan2(event.clientY - g.centre.y, event.clientX - g.centre.x) * 180) / Math.PI + 90;
      const snapped = event.shiftKey ? Math.round(angle / 15) * 15 : Math.round(angle);
      this.liveRotation.set({ id: g.id, rotate: ((snapped % 360) + 360) % 360 });
      return;
    }
    const dx = (event.clientX - g.startX) / (PX_PER_PT * this.scale());
    const dy = (event.clientY - g.startY) / (PX_PER_PT * this.scale());
    this.liveGeometry.set({
      id: g.id,
      geometry:
        g.kind === 'move'
          ? { ...g.start, left: round(g.start.left + dx), top: round(g.start.top + dy) }
          : resized(g.start, g.handle!, dx, dy),
    });
  }

  onPointerUp(event: PointerEvent): void {
    const g = this.gesture;
    if (!g || event.pointerId !== g.pointerId) return;
    this.gesture = null;
    const rotation = this.liveRotation();
    this.liveRotation.set(null);
    if (g.kind === 'rotate') {
      if (rotation && rotation.rotate !== g.startRotate) this.rotationChange.emit(rotation);
      return;
    }
    const live = this.liveGeometry();
    this.liveGeometry.set(null);
    if (
      live &&
      (live.geometry.left !== g.start.left ||
        live.geometry.top !== g.start.top ||
        live.geometry.width !== g.start.width ||
        live.geometry.height !== g.start.height)
    ) {
      this.geometryChange.emit(live);
    }
  }

  onElementDoubleClick(event: MouseEvent, el: SlideElement): void {
    if (!this.interactive()) return;
    event.stopPropagation();
    if (el.kind === 'shape' || el.kind === 'table') this.requestEdit.emit(el.id);
  }

  /** Text editing ends on blur or Escape; the edited DOM becomes paragraphs. */
  onTextBlur(event: FocusEvent, el: SlideElement, cell?: { row: number; col: number }): void {
    const target = event.target as HTMLElement;
    if (this.editingId() !== el.id) return;
    this.textCommit.emit({ id: el.id, paragraphs: domToParagraphs(target), ...(cell ? { cell } : {}) });
  }

  onTextKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      (event.target as HTMLElement).blur();
    }
    // Editing keys stay in the text; the editor's shortcuts must not see them.
    event.stopPropagation();
  }

  private focusText(element: HTMLElement): void {
    element.focus();
    const range = this.host.nativeElement.ownerDocument.createRange();
    range.selectNodeContents(element);
    range.collapse(false);
    const selection = this.host.nativeElement.ownerDocument.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
  }

  onLinkClick(event: MouseEvent, el: SlideElement): void {
    if (this.interactive() || !el.link) return;
    event.preventDefault();
    this.open.emit(el.link);
  }
}

function round(n: number): number {
  return Math.round(n * 100) / 100;
}

function resized(start: ElementGeometry, handle: ResizeHandle, dx: number, dy: number): ElementGeometry {
  let { left, top, width, height } = start;
  if (handle.includes('e')) width = start.width + dx;
  if (handle.includes('s')) height = start.height + dy;
  if (handle.includes('w')) {
    width = start.width - dx;
    left = start.left + dx;
  }
  if (handle.includes('n')) {
    height = start.height - dy;
    top = start.top + dy;
  }
  if (width < 4) {
    if (handle.includes('w')) left = start.left + start.width - 4;
    width = 4;
  }
  if (height < 0) {
    if (handle.includes('n')) top = start.top + start.height;
    height = 0;
  }
  return { left: round(left), top: round(top), width: round(width), height: round(height) };
}

export function fillCss(fill: Fill | null): string {
  if (!fill) return 'transparent';
  if (fill.type === 'color') return fill.color;
  if (fill.type === 'gradient') return `linear-gradient(${90 + fill.angle}deg, ${fill.stops.map(s => `${s.color} ${s.pos}%`).join(', ')})`;
  return `url("${fill.src}") center / cover`;
}

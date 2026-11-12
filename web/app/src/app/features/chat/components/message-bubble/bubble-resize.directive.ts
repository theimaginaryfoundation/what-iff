import { DOCUMENT } from '@angular/common';
import { DestroyRef, Directive, ElementRef, Injectable, effect, inject, input, signal } from '@angular/core';

const STORAGE_KEY = 'whatiff.bubbleWidths';
const MAX_KEPT = 300;
/** The narrowest a bubble can be dragged to. */
export const MIN_BUBBLE_WIDTH = 240;
const KEY_STEP = 40;

/**
 * The widths users dragged message bubbles to, by message id, kept in this browser (localStorage)
 * so a resized bubble stays resized after a reload. The oldest are dropped past MAX_KEPT.
 */
@Injectable({ providedIn: 'root' })
export class BubbleWidths {
  private readonly widths = signal<Record<string, number>>(this.read());

  get(id: string): number | null {
    return this.widths()[id] ?? null;
  }

  set(id: string, width: number | null): void {
    const next = { ...this.widths() };
    delete next[id];
    if (width !== null) next[id] = Math.round(width);
    const ids = Object.keys(next);
    for (const old of ids.slice(0, Math.max(0, ids.length - MAX_KEPT))) delete next[old];
    this.widths.set(next);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      // Storage full or blocked: the width holds for this page only.
    }
  }

  private read(): Record<string, number> {
    try {
      const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}') as unknown;
      return parsed && typeof parsed === 'object' ? (parsed as Record<string, number>) : {};
    } catch {
      return {};
    }
  }
}

/**
 * A handle on a message bubble's outer edge that resizes it (desktop): drag it, or use the arrow
 * keys on it; double-click it to go back to the bubble's own width. The width is the bubble's
 * (BubbleWidths), never wider than the conversation leaves it. A resized bubble carries
 * `bubble--sized`, so what is drawn in it (a diagram) can fill it.
 */
@Directive({
  selector: '[appBubbleResize]',
  standalone: true,
  host: {
    '[class.bubble--sized]': 'width() !== null',
    '[style.width.px]': 'width()',
  },
})
export class BubbleResizeDirective {
  /** The message the bubble shows; its width is kept under its id. */
  readonly appBubbleResize = input.required<string>();
  /** The edge the handle is on: the outer one, right for the agent's bubbles, left for the user's. */
  readonly resizeEdge = input<'left' | 'right'>('right');

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  private readonly document = inject(DOCUMENT);
  private readonly store = inject(BubbleWidths);
  readonly width = signal<number | null>(null);
  private readonly handle: HTMLElement;

  constructor() {
    this.handle = this.document.createElement('div');
    this.handle.className = 'bubble__resize';
    this.handle.tabIndex = 0;
    this.handle.setAttribute('role', 'separator');
    this.handle.setAttribute('aria-orientation', 'vertical');
    this.handle.setAttribute('aria-label', 'Resize message. Double-click to reset.');
    this.handle.title = 'Drag to resize; double-click to reset';
    this.handle.addEventListener('pointerdown', this.onPointerDown);
    this.handle.addEventListener('dblclick', this.reset);
    this.handle.addEventListener('keydown', this.onKeyDown);
    this.host.append(this.handle);

    effect(() => {
      this.handle.dataset['edge'] = this.resizeEdge();
      this.width.set(this.store.get(this.appBubbleResize()));
    });
    inject(DestroyRef).onDestroy(() => this.handle.remove());
  }

  /** The widest the bubble may be: its message row, less the avatar beside it. */
  private maxWidth(): number {
    const row = this.host.closest('.message-group');
    if (!row) return Number.POSITIVE_INFINITY;
    const avatar = row.querySelector('.message-group__avatar-column');
    const beside = avatar ? avatar.getBoundingClientRect().width + 12 : 0;
    return Math.max(MIN_BUBBLE_WIDTH, row.getBoundingClientRect().width - beside);
  }

  private apply(width: number, keep: boolean): void {
    const clamped = Math.min(this.maxWidth(), Math.max(MIN_BUBBLE_WIDTH, width));
    this.width.set(clamped);
    this.handle.setAttribute('aria-valuenow', String(Math.round(clamped)));
    if (keep) this.store.set(this.appBubbleResize(), clamped);
  }

  private readonly onPointerDown = (event: PointerEvent): void => {
    if (event.button !== 0) return;
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = this.host.getBoundingClientRect().width;
    const sign = this.resizeEdge() === 'right' ? 1 : -1;
    this.handle.setPointerCapture?.(event.pointerId);
    this.host.classList.add('bubble--resizing');
    const move = (e: PointerEvent) => this.apply(startWidth + sign * (e.clientX - startX), false);
    const up = (e: PointerEvent) => {
      this.handle.removeEventListener('pointermove', move);
      this.handle.removeEventListener('pointerup', up);
      this.handle.removeEventListener('pointercancel', up);
      this.host.classList.remove('bubble--resizing');
      this.apply(startWidth + sign * (e.clientX - startX), true);
    };
    this.handle.addEventListener('pointermove', move);
    this.handle.addEventListener('pointerup', up);
    this.handle.addEventListener('pointercancel', up);
  };

  private readonly onKeyDown = (event: KeyboardEvent): void => {
    const sign = this.resizeEdge() === 'right' ? 1 : -1;
    const now = this.width() ?? this.host.getBoundingClientRect().width;
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
      event.preventDefault();
      this.apply(now + (event.key === 'ArrowRight' ? sign : -sign) * KEY_STEP, true);
    } else if (event.key === 'Escape' || event.key === 'Home') {
      event.preventDefault();
      this.reset();
    }
  };

  private readonly reset = (): void => {
    this.width.set(null);
    this.handle.removeAttribute('aria-valuenow');
    this.store.set(this.appBubbleResize(), null);
  };
}

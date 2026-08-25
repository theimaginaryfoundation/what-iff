import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  effect,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';

import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';

type DragMode = 'move' | 'resize' | null;

@Component({
  selector: 'app-personality-portrait-focus-editor',
  standalone: true,
  template: `
    <div class="relative h-[16.6875rem] w-[12.5rem] overflow-hidden rounded-lg bg-(--color-surface-elevated)">
      @if (imageUrl(); as url) {
        <img #image class="h-full w-full object-cover object-top" [src]="url" alt="" />
      } @else {
        <div class="flex h-full w-full items-center justify-center text-xs text-(--color-text-secondary)">No portrait selected</div>
      }
      @if (imageUrl()) {
        <svg
          class="absolute inset-0 h-full w-full touch-none"
          (pointerdown)="onPointerDown($event)"
          (pointermove)="onPointerMove($event)"
          (pointerup)="onPointerUp($event)"
          (pointercancel)="onPointerUp($event)"
          (lostpointercapture)="onPointerUp($event)"
        >
          <circle
            [attr.cx]="cxPx()"
            [attr.cy]="cyPx()"
            [attr.r]="rPx()"
            fill="none"
            stroke="white"
            stroke-width="2"
            stroke-dasharray="6 4"
          />
          <circle
            [attr.cx]="handleX()"
            [attr.cy]="handleY()"
            r="7"
            fill="var(--color-surface-card)"
            stroke="var(--color-border-base)"
            stroke-width="1.5"
          />
        </svg>
      }
    </div>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityPortraitFocusEditorComponent {
  readonly imageUrl = input<string | null>(null);
  readonly circle = input<PersonalityThumbnailCircle | null>(null);
  readonly circleChange = output<PersonalityThumbnailCircle>();

  readonly imageEl = viewChild<ElementRef<HTMLImageElement>>('image');

  private dragMode = signal<DragMode>(null);

  readonly cxPx = signal(100);
  readonly cyPx = signal(106.8);
  readonly rPx = signal(68);

  readonly handleX = signal(148);
  readonly handleY = signal(154);

  constructor() {
    effect(() => {
      this.circle();
      this.imageUrl();
      queueMicrotask(() => this.syncFromInput());
    });
  }

  syncFromInput(): void {
    const next = this.circle() ?? { cx: 0.5, cy: 0.4, r: 0.34 };
    const { width, height } = this.viewportSize();
    this.cxPx.set(next.cx * width);
    this.cyPx.set(next.cy * height);
    this.rPx.set(next.r * Math.min(width, height));
    this.syncHandle();
  }

  onPointerDown(event: PointerEvent): void {
    const target = event.target as SVGElement | null;
    const isHandle = target?.tagName.toLowerCase() === 'circle' && target.getAttribute('r') === '7';
    this.dragMode.set(isHandle ? 'resize' : 'move');
    (event.currentTarget as SVGElement).setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  onPointerMove(event: PointerEvent): void {
    const mode = this.dragMode();
    if (!mode) return;
    const svg = event.currentTarget as SVGElement;
    const rect = svg.getBoundingClientRect();
    const x = clamp(event.clientX - rect.left, 0, rect.width);
    const y = clamp(event.clientY - rect.top, 0, rect.height);
    if (mode === 'move') {
      const r = this.rPx();
      this.cxPx.set(clamp(x, r, rect.width - r));
      this.cyPx.set(clamp(y, r, rect.height - r));
    } else {
      const dx = x - this.cxPx();
      const dy = y - this.cyPx();
      const maxR = Math.min(
        this.cxPx(),
        this.cyPx(),
        rect.width - this.cxPx(),
        rect.height - this.cyPx(),
      );
      this.rPx.set(clamp(Math.sqrt(dx * dx + dy * dy), 14, maxR));
    }
    this.syncHandle();
    this.emitNormalized(rect.width, rect.height);
  }

  onPointerUp(event: PointerEvent): void {
    if ((event.currentTarget as SVGElement).hasPointerCapture(event.pointerId)) {
      (event.currentTarget as SVGElement).releasePointerCapture(event.pointerId);
    }
    this.dragMode.set(null);
  }

  private emitNormalized(width: number, height: number): void {
    const minSize = Math.min(width, height);
    this.circleChange.emit({
      cx: this.cxPx() / width,
      cy: this.cyPx() / height,
      r: this.rPx() / minSize,
    });
  }

  private viewportSize(): { width: number; height: number } {
    const image = this.imageEl()?.nativeElement;
    return {
      width: image?.clientWidth ?? 200,
      height: image?.clientHeight ?? 267,
    };
  }

  private syncHandle(): void {
    this.handleX.set(this.cxPx() + this.rPx() * Math.cos(Math.PI / 4));
    this.handleY.set(this.cyPx() + this.rPx() * Math.sin(Math.PI / 4));
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

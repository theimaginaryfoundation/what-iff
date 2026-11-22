import { ChangeDetectionStrategy, Component, ElementRef, afterNextRender, input, output, signal, viewChild } from '@angular/core';
import { ConstellationIconComponent } from '../../shared/ui/icons/constellation-icon.component';
import { UserIconComponent } from '../../shared/ui/icons/icons';
import { LEARNING_OBJECTIVES } from './constellation.data';
import { BOARD_SIZE, MAP_EDGES, MAP_LABELS, NODE_POSITIONS, ORIGIN } from './constellation-layout';
import { objectiveReady } from './constellation-progress';
import type { LearningObjective } from './constellation.model';

@Component({
  selector: 'app-constellation-map',
  standalone: true,
  imports: [ConstellationIconComponent, UserIconComponent],
  templateUrl: './constellation-map.component.html',
  styleUrl: './constellation-map.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ConstellationMapComponent {
  readonly selected = input('S1');
  readonly completed = input<ReadonlySet<string>>(new Set());
  readonly select = output<string>();
  readonly zoom = signal(0.85);
  readonly dragging = signal(false);
  readonly offsetX = signal(0);
  readonly offsetY = signal(0);
  readonly nodes = LEARNING_OBJECTIVES;
  readonly positions = NODE_POSITIONS;
  readonly edges = MAP_EDGES;
  readonly labels = MAP_LABELS;
  readonly boardSize = BOARD_SIZE;
  readonly origin = ORIGIN;
  readonly rings = [170, 290, 520, 780, 1050, 1390];
  readonly rails = ['top', 'right', 'bottom', 'left'];
  readonly viewport = viewChild.required<ElementRef<HTMLElement>>('viewport');
  private drag: { x: number; y: number; left: number; top: number } | null = null;

  constructor() {
    afterNextRender(() => this.recenter());
  }

  ready(node: LearningObjective): boolean {
    return objectiveReady(node, this.completed());
  }

  recenter(): void {
    this.setZoom(this.viewport().nativeElement.clientWidth < 760 ? 0.78 : 0.95, false);
    this.centerOn('S1');
  }

  centerOn(id: string): void {
    const point = this.positions[id];
    if (!point) return;
    const viewport = this.viewport().nativeElement;
    viewport.scrollLeft = this.offsetX() + point.x * this.zoom() - viewport.clientWidth / 2;
    viewport.scrollTop = this.offsetY() + point.y * this.zoom() - viewport.clientHeight * 0.51;
  }

  focusNode(id: string): void {
    this.viewport().nativeElement.querySelector<HTMLButtonElement>(`[data-objective="${id}"]`)?.focus({ preventScroll: true });
  }

  atlas(): void {
    const viewport = this.viewport().nativeElement;
    this.setZoom(Math.min((viewport.clientWidth - 100) / 3700, (viewport.clientHeight - 100) / 3700), false);
    this.centerOn('S1');
  }

  zoomBy(delta: number): void {
    this.setZoom(this.zoom() + delta);
  }

  startPan(event: PointerEvent, rail = false): void {
    if (event.button !== 0 || (!rail && (event.pointerType === 'touch' || (event.target as Element).closest('button')))) return;
    event.preventDefault();
    const viewport = this.viewport().nativeElement;
    this.drag = { x: event.clientX, y: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop };
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
    this.dragging.set(true);
  }

  movePan(event: PointerEvent): void {
    if (!this.drag) return;
    event.preventDefault();
    const viewport = this.viewport().nativeElement;
    viewport.scrollLeft = this.drag.left - (event.clientX - this.drag.x);
    viewport.scrollTop = this.drag.top - (event.clientY - this.drag.y);
  }

  endPan(): void {
    this.drag = null;
    this.dragging.set(false);
  }

  onWheel(event: WheelEvent, rail = false): void {
    if (event.ctrlKey || event.metaKey) {
      event.preventDefault();
      this.setZoom(this.zoom() * (event.deltaY > 0 ? 0.92 : 1.08));
    } else if (rail) {
      event.preventDefault();
      const viewport = this.viewport().nativeElement;
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.clientHeight : 1;
      viewport.scrollLeft += (event.shiftKey ? event.deltaY : event.deltaX) * unit;
      viewport.scrollTop += (event.shiftKey ? 0 : event.deltaY) * unit;
    }
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.target !== event.currentTarget) return;
    const moves: Record<string, [number, number]> = { ArrowLeft: [-80, 0], ArrowRight: [80, 0], ArrowUp: [0, -80], ArrowDown: [0, 80] };
    const move = moves[event.key];
    if (!move) return;
    event.preventDefault();
    this.viewport().nativeElement.scrollLeft += move[0];
    this.viewport().nativeElement.scrollTop += move[1];
  }

  private setZoom(value: number, keepCenter = true): void {
    const viewport = this.viewport().nativeElement;
    const x = (viewport.scrollLeft + viewport.clientWidth / 2 - this.offsetX()) / this.zoom();
    const y = (viewport.scrollTop + viewport.clientHeight / 2 - this.offsetY()) / this.zoom();
    this.zoom.set(Math.max(0.12, Math.min(1.6, value)));
    this.offsetX.set(Math.max(0, (viewport.clientWidth - BOARD_SIZE * this.zoom()) / 2));
    this.offsetY.set(Math.max(0, (viewport.clientHeight - BOARD_SIZE * this.zoom()) / 2));
    if (keepCenter) {
      viewport.scrollLeft = this.offsetX() + x * this.zoom() - viewport.clientWidth / 2;
      viewport.scrollTop = this.offsetY() + y * this.zoom() - viewport.clientHeight / 2;
    }
  }
}

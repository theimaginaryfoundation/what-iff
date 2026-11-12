import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ImageMarkupComponent, MarkupResult } from './image-markup.component';
import { CanvasFactory, MarkupStroke, PEN_COLORS, SKETCH_SIZE } from './markup-strokes';

/** A canvas that records what is drawn on it and "saves" to a Blob holding that record. */
interface FakeCanvas {
  width: number;
  height: number;
  calls: string[];
}

function fakeCanvasFactory(made: FakeCanvas[]): CanvasFactory {
  return (width, height) => {
    const canvas: FakeCanvas = { width, height, calls: [] };
    made.push(canvas);
    const ctx = {
      lineCap: 'butt',
      lineJoin: 'miter',
      lineWidth: 1,
      strokeStyle: '',
      fillStyle: '',
      fillRect: () => canvas.calls.push(`fill ${ctx.fillStyle}`),
      drawImage: () => canvas.calls.push('image'),
      beginPath: () => undefined,
      moveTo: () => undefined,
      lineTo: () => undefined,
      arc: () => undefined,
      fill: () => undefined,
      stroke: () => canvas.calls.push(`stroke ${ctx.strokeStyle}`),
    };
    return {
      ...canvas,
      getContext: () => ctx,
      toBlob: (done: (blob: Blob | null) => void) => done(new Blob([canvas.calls.join('|')], { type: 'image/png' })),
    } as unknown as HTMLCanvasElement;
  };
}

/** A pointer event at (x, y) in image pixels on canvas: it sits at the origin and the scale is 1. */
function pointer(canvas: HTMLCanvasElement, x: number, y: number): PointerEvent {
  return { button: 0, clientX: x, clientY: y, pointerId: 1, target: canvas, preventDefault: () => undefined } as unknown as PointerEvent;
}

describe('ImageMarkupComponent', () => {
  let fixture: ComponentFixture<ImageMarkupComponent>;
  let component: ImageMarkupComponent;
  let dirty: boolean[];
  let made: FakeCanvas[];

  async function render(source: Blob | null): Promise<HTMLElement> {
    await TestBed.configureTestingModule({ imports: [ImageMarkupComponent] }).compileComponents();
    fixture = TestBed.createComponent(ImageMarkupComponent);
    component = fixture.componentInstance;
    dirty = [];
    made = [];
    component.dirtyChange.subscribe(value => dirty.push(value));
    fixture.componentRef.setInput('source', source);
    fixture.componentRef.setInput('canvasFactory', fakeCanvasFactory(made));
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function draw(...points: [number, number][]): void {
    const canvas = (fixture.nativeElement as HTMLElement).querySelector('canvas')!;
    const at = ([x, y]: [number, number]) => pointer(canvas, x, y);
    const [first, ...rest] = points;
    component.onPointerDown(at(first));
    for (const p of rest) component.onPointerMove(at(p));
    component.onPointerUp(at(points[points.length - 1]));
    fixture.detectChanges();
  }

  const stroke = (tool: MarkupStroke['tool'], color = PEN_COLORS[0].value): MarkupStroke => ({
    tool,
    color,
    width: 4,
    points: [1, 1, 2, 2],
  });

  it('switches between the area brush and the pen, which alone offers colours', async () => {
    const host = await render(new Blob(['img'], { type: 'image/png' }));
    expect(component.tool()).toBe('area');
    expect(host.querySelector('[aria-label="Pen colour"]')).toBeNull();

    const [, penTag] = Array.from(host.querySelectorAll<HTMLButtonElement>('ui-tag button'));
    penTag.click();
    fixture.detectChanges();
    expect(component.tool()).toBe('pen');

    host.querySelector<HTMLButtonElement>('[aria-label="Green"]')!.click();
    fixture.detectChanges();
    expect(component.color()).toBe('#22c55e');
    expect(host.querySelector('[aria-label="Green"]')!.getAttribute('aria-pressed')).toBe('true');
  });

  it('keeps a size per tool, clamped to the slider range', async () => {
    await render(new Blob(['img'], { type: 'image/png' }));
    component.setSize(500);
    expect(component.size()).toBe(component.maxSize);
    component.tool.set('pen');
    expect(component.size()).toBe(6);
    component.setSize(Number.NaN);
    expect(component.size()).toBe(6);
  });

  it('starts a sketch on the black pen, with no area brush', async () => {
    const host = await render(null);
    expect(component.tool()).toBe('pen');
    expect(component.color()).toBe('#111827');
    expect(host.querySelectorAll('ui-tag')).toHaveLength(1);
  });

  it('reports drawing as dirty, and clean again after undo or clear', async () => {
    await render(null);
    expect(dirty.at(-1)).toBe(false);

    draw([10, 10], [20, 20], [30, 30]);
    expect(component.strokes()).toHaveLength(1);
    expect(component.strokes()[0].points).toEqual([10, 10, 20, 20, 30, 30]);
    expect(dirty.at(-1)).toBe(true);

    component.onKeydown(new KeyboardEvent('keydown', { key: 'z', ctrlKey: true }));
    fixture.detectChanges();
    expect(component.strokes()).toHaveLength(0);
    expect(dirty.at(-1)).toBe(false);

    draw([5, 5]);
    draw([6, 6]);
    component.clear();
    fixture.detectChanges();
    expect(component.strokes()).toHaveLength(0);
    expect(dirty.at(-1)).toBe(false);
  });

  it('exports a sketch as one markup file on a white page at the sketch size', async () => {
    await render(null);
    draw([10, 10], [20, 20]);
    const result = await component.export();
    expect(result.mask).toBeNull();
    expect(result.markup).not.toBeNull();
    expect(made.map(c => [c.width, c.height])).toEqual([[SKETCH_SIZE, SKETCH_SIZE]]);
    expect(await result.markup!.text()).toBe('fill #ffffff|stroke #111827');
  });

  it('exports only the files whose tool was used: the area as a white-on-black mask', async () => {
    await render(new Blob(['img'], { type: 'image/png' }));
    component.strokes.set([stroke('area')]);
    const areaOnly = await component.export();
    expect(areaOnly.markup).toBeNull();
    expect(await areaOnly.mask!.text()).toBe('fill #000000|stroke #ffffff');

    component.strokes.set([stroke('area'), stroke('pen', '#3b82f6')]);
    const both = await component.export();
    expect(await both.mask!.text()).toBe('fill #000000|stroke #ffffff');
    expect(await both.markup!.text()).toContain('stroke #3b82f6');
  });

  it('emits the export on Add to message, and nothing without a drawing', async () => {
    await render(null);
    const sent: MarkupResult[] = [];
    component.submitted.subscribe(result => sent.push(result));

    await component.send();
    expect(sent).toHaveLength(0);

    draw([1, 1], [2, 2]);
    await component.send();
    expect(sent).toHaveLength(1);
    expect(sent[0].markup).not.toBeNull();
    expect(component.exporting()).toBe(false);
  });

  it('shows the export error instead of emitting when the drawing cannot be saved', async () => {
    await render(null);
    fixture.componentRef.setInput('canvasFactory', () => ({ getContext: () => null }) as unknown as HTMLCanvasElement);
    const sent: MarkupResult[] = [];
    component.submitted.subscribe(result => sent.push(result));

    draw([1, 1], [2, 2]);
    await component.send();
    fixture.detectChanges();
    expect(sent).toHaveLength(0);
    expect(component.error()).toBe('This browser cannot draw on a canvas.');
    expect((fixture.nativeElement as HTMLElement).querySelector('.markup__error')?.textContent).toContain('cannot draw');
  });
});

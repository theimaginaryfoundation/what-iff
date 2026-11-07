import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ChartElement, ImageElement, Slide, TableElement, blankSlide, emptyTextBody } from '../pptx/deck-model';
import { newLine, newShape, newTextBox } from './deck-commands';
import { PX_PER_PT, SlideCanvasComponent } from './slide-canvas.component';

describe('SlideCanvasComponent', () => {
  let fixture: ComponentFixture<SlideCanvasComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [SlideCanvasComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
  });

  function render(
    slide: Slide,
    inputs: Partial<{ scale: number; interactive: boolean; selectedId: string | null; editingId: string | null }> = {},
  ): HTMLElement {
    fixture = TestBed.createComponent(SlideCanvasComponent);
    fixture.componentRef.setInput('slide', slide);
    fixture.componentRef.setInput('size', { width: 720, height: 405 });
    for (const [k, v] of Object.entries(inputs)) fixture.componentRef.setInput(k, v);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function slideWith(...elements: Slide['elements']): Slide {
    return { ...blankSlide(), background: { type: 'color', color: '#F3F4F6' }, elements };
  }

  it('draws shapes, text, lines, images, tables and charts in points at the scale', () => {
    const shape = newShape('ellipse', 10, 20, 100, 50, '#FF0000');
    const text = newTextBox(0, 0, 200, 40, 'Hello');
    const line = newLine(0, 100, 300, 0, '#00FF00');
    const image: ImageElement = {
      id: 'img',
      name: 'P',
      kind: 'image',
      left: 5,
      top: 5,
      width: 50,
      height: 50,
      rotate: 0,
      flipH: false,
      flipV: false,
      src: 'data:image/png;base64,AAAA',
      border: null,
      geometry: 'rect',
      crop: { top: 0.1, bottom: 0.1, left: 0, right: 0 },
    };
    const table: TableElement = {
      id: 'tbl',
      name: 'T',
      kind: 'table',
      left: 0,
      top: 200,
      width: 200,
      height: 50,
      rotate: 0,
      flipH: false,
      flipV: false,
      colWidths: [100, 100],
      rowHeights: [25, 25],
      rows: [
        [
          {
            text: emptyTextBody([{ align: 'left', runs: [{ text: 'A1' }] }]),
            fill: { type: 'color', color: '#DDDDDD' },
            borders: {},
            rowSpan: 1,
            colSpan: 2,
            covered: false,
          },
          { text: emptyTextBody(), fill: null, borders: {}, rowSpan: 0, colSpan: 0, covered: true },
        ],
        [
          {
            text: emptyTextBody([{ align: 'left', runs: [{ text: 'B1' }] }]),
            fill: null,
            borders: { top: { color: '#000000', width: 1, style: 'solid' } },
            rowSpan: 1,
            colSpan: 1,
            covered: false,
          },
          {
            text: emptyTextBody([{ align: 'left', runs: [{ text: 'B2' }] }]),
            fill: null,
            borders: {},
            rowSpan: 1,
            colSpan: 1,
            covered: false,
          },
        ],
      ],
    };
    const chart: ChartElement = {
      id: 'ch',
      name: 'C',
      kind: 'chart',
      left: 300,
      top: 0,
      width: 200,
      height: 100,
      rotate: 0,
      flipH: false,
      flipV: false,
      chartType: 'bar',
      series: [{ name: 'S', labels: ['a', 'b'], values: [1, 3] }],
      colors: ['#123456'],
    };
    const el = render(slideWith(shape, text, line, image, table, chart), { scale: 0.5 });
    const surface = el.querySelector<HTMLElement>('.slide__surface')!;
    expect(surface.style.width).toBe('720pt');
    expect(surface.style.transform).toBe('scale(0.5)');
    expect(surface.style.background).toContain('rgb(243, 244, 246)');
    expect(el.querySelector<HTMLElement>('.slide')!.style.width).toBe(`${720 * PX_PER_PT * 0.5}px`);
    const elements = el.querySelectorAll<HTMLElement>('.slide__element');
    expect(elements).toHaveLength(6);
    expect(elements[0].style.left).toBe('10pt');
    expect(elements[0].querySelector('path')?.getAttribute('d')).toContain('A50,25');
    expect(elements[0].querySelector('path')?.getAttribute('fill')).toBe('#FF0000');
    expect(elements[1].querySelector('.slide__text')?.textContent).toBe('Hello');
    expect(elements[2].querySelector('path')?.getAttribute('d')).toBe('M0,0L300,0');
    expect(elements[3].querySelector<HTMLImageElement>('img')?.style.height).toBe('125%');
    expect(elements[4].querySelectorAll('td')).toHaveLength(3);
    expect(elements[4].querySelector('td')?.getAttribute('colspan')).toBe('2');
    const chartImg = elements[5].querySelector<HTMLImageElement>('img.slide__chart')!;
    expect(chartImg.src.startsWith('data:image/svg+xml')).toBe(true);
    expect(decodeURIComponent(chartImg.src)).toContain('<rect');
  });

  it('selects, drags and resizes elements when interactive, in slide points', () => {
    const shape = newShape('rect', 100, 100, 200, 100, '#FF0000');
    const el = render(slideWith(shape), { scale: 1, interactive: true });
    const select = vi.fn();
    const start = vi.fn();
    const change = vi.fn();
    fixture.componentInstance.select.subscribe(select);
    fixture.componentInstance.gestureStart.subscribe(start);
    fixture.componentInstance.geometryChange.subscribe(change);
    const target = el.querySelector<HTMLElement>('.slide__element')!;
    target.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: 100, clientY: 100, button: 0, pointerId: 1 }));
    expect(select).toHaveBeenCalledWith(shape.id);
    expect(start).toHaveBeenCalledWith(shape.id);
    const slide = el.querySelector<HTMLElement>('.slide')!;
    slide.dispatchEvent(
      new PointerEvent('pointermove', { bubbles: true, clientX: 100 + 30 * PX_PER_PT, clientY: 100 + 15 * PX_PER_PT, pointerId: 1 }),
    );
    fixture.detectChanges();
    expect(target.style.left).toBe('130pt');
    slide.dispatchEvent(
      new PointerEvent('pointerup', { bubbles: true, clientX: 100 + 30 * PX_PER_PT, clientY: 100 + 15 * PX_PER_PT, pointerId: 1 }),
    );
    expect(change).toHaveBeenCalledWith({ id: shape.id, geometry: { left: 130, top: 115, width: 200, height: 100 } });

    fixture.componentRef.setInput('selectedId', shape.id);
    fixture.detectChanges();
    const handle = el.querySelector<HTMLElement>('.slide__handle--se')!;
    expect(el.querySelectorAll('.slide__handle')).toHaveLength(8);
    handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: 0, clientY: 0, button: 0, pointerId: 2 }));
    slide.dispatchEvent(
      new PointerEvent('pointermove', { bubbles: true, clientX: 20 * PX_PER_PT, clientY: -10 * PX_PER_PT, pointerId: 2 }),
    );
    slide.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, clientX: 20 * PX_PER_PT, clientY: -10 * PX_PER_PT, pointerId: 2 }));
    expect(change).toHaveBeenLastCalledWith({ id: shape.id, geometry: { left: 100, top: 100, width: 220, height: 90 } });

    slide.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: 5, clientY: 5, button: 0, pointerId: 3 }));
    expect(select).toHaveBeenLastCalledWith(null);
  });

  it('rotates with the handle above the element', () => {
    const shape = newShape('rect', 100, 100, 200, 100, '#FF0000');
    const el = render(slideWith(shape), { interactive: true, selectedId: shape.id });
    const rotated = vi.fn();
    fixture.componentInstance.rotationChange.subscribe(rotated);
    const handle = el.querySelector<HTMLElement>('.slide__rotate')!;
    const box = handle.parentElement!.getBoundingClientRect();
    const cx = box.left + box.width / 2;
    const cy = box.top + box.height / 2;
    handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: cx, clientY: cy - 50, button: 0, pointerId: 9 }));
    const slide = el.querySelector<HTMLElement>('.slide')!;
    slide.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: cx + 50, clientY: cy, pointerId: 9 }));
    fixture.detectChanges();
    expect(el.querySelector<HTMLElement>('.slide__element')!.style.transform).toContain('rotate(90deg)');
    slide.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, clientX: cx + 50, clientY: cy, pointerId: 9 }));
    expect(rotated).toHaveBeenCalledWith({ id: shape.id, rotate: 90 });
  });

  it('asks to edit on double-click and commits the edited text on blur', () => {
    const text = newTextBox(0, 0, 200, 40, 'Hello');
    const el = render(slideWith(text), { interactive: true, selectedId: text.id });
    const requestEdit = vi.fn();
    const commit = vi.fn();
    fixture.componentInstance.requestEdit.subscribe(requestEdit);
    fixture.componentInstance.textCommit.subscribe(commit);
    el.querySelector<HTMLElement>('.slide__element')!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    expect(requestEdit).toHaveBeenCalledWith(text.id);
    fixture.componentRef.setInput('editingId', text.id);
    fixture.detectChanges();
    const editable = el.querySelector<HTMLElement>('.slide__text')!;
    expect(editable.getAttribute('contenteditable')).toBe('true');
    editable.innerHTML = '<p><b>Changed</b></p>';
    editable.dispatchEvent(new FocusEvent('blur'));
    expect(commit).toHaveBeenCalledWith({ id: text.id, paragraphs: [{ align: 'left', runs: [{ text: 'Changed', bold: true }] }] });
  });

  it('leaves the text being typed alone when the canvas is checked again', () => {
    const text = newTextBox(0, 0, 200, 40, 'Hello');
    const el = render(slideWith(text), { interactive: true, selectedId: text.id, editingId: text.id });
    const box = el.querySelector('.slide__text') as HTMLElement;
    const node = box.querySelector('p, div') ?? box;
    node.append(' typed');
    fixture.componentRef.setInput('scale', 2);
    fixture.detectChanges();
    expect(box.textContent).toContain('Hello typed');
  });

  it('shows a text body again once the model replaces it', () => {
    const text = newTextBox(0, 0, 200, 40, 'Hello');
    const el = render(slideWith(text));
    const replaced = { ...text, text: emptyTextBody([{ align: 'left', runs: [{ text: 'Changed' }] }]) };
    fixture.componentRef.setInput('slide', slideWith(replaced));
    fixture.detectChanges();
    expect(el.querySelector('.slide__text')?.textContent).toContain('Changed');
  });

  it('does nothing with pointers when not interactive', () => {
    const shape = newShape('rect', 0, 0, 10, 10, '#000000');
    const el = render(slideWith(shape));
    const select = vi.fn();
    fixture.componentInstance.select.subscribe(select);
    el.querySelector<HTMLElement>('.slide__element')!.dispatchEvent(
      new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerId: 1 }),
    );
    expect(select).not.toHaveBeenCalled();
    expect(el.querySelector('.slide__handle')).toBeNull();
  });
});

import { TestBed } from '@angular/core/testing';
import { ConstellationMapComponent } from './constellation-map.component';

describe('ConstellationMapComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [ConstellationMapComponent] }).compileComponents();
  });

  it('offers every objective even when no prerequisites have been completed', async () => {
    const fixture = TestBed.createComponent(ConstellationMapComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    const buttons = [...fixture.nativeElement.querySelectorAll('button[data-objective]')] as HTMLButtonElement[];
    expect(buttons).toHaveLength(46);
    expect(buttons.every(button => !button.disabled)).toBe(true);
    const select = vi.spyOn(fixture.componentInstance.select, 'emit');
    buttons.find(button => button.dataset['objective'] === 'B3')!.click();
    expect(select).toHaveBeenCalledWith('B3');
  });

  it('pans from all four touch-friendly margins and forwards wheel scrolling', async () => {
    const fixture = TestBed.createComponent(ConstellationMapComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    const viewport = fixture.nativeElement.querySelector('.map') as HTMLElement;
    const rails = [...fixture.nativeElement.querySelectorAll('.map__rail')] as HTMLElement[];
    expect(rails).toHaveLength(4);
    for (const rail of rails) {
      viewport.scrollLeft = 500;
      viewport.scrollTop = 500;
      rail.dispatchEvent(new PointerEvent('pointerdown', { pointerType: 'touch', button: 0, clientX: 100, clientY: 100 }));
      rail.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'touch', clientX: 140, clientY: 130 }));
      expect(viewport.scrollLeft).toBe(460);
      expect(viewport.scrollTop).toBe(470);
      rail.dispatchEvent(new PointerEvent('pointerup'));
      rail.dispatchEvent(new WheelEvent('wheel', { deltaX: 15, deltaY: 30 }));
      expect(viewport.scrollLeft).toBe(475);
      expect(viewport.scrollTop).toBe(500);
    }
  });

  it('does not hijack native touch scrolling on the board or begin a drag on a task', async () => {
    const fixture = TestBed.createComponent(ConstellationMapComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    const viewport = fixture.nativeElement.querySelector('.map') as HTMLElement;
    const task = fixture.nativeElement.querySelector('[data-objective="S1"]') as HTMLElement;
    task.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerType: 'mouse' }));
    expect(fixture.componentInstance.dragging()).toBe(false);
    viewport.dispatchEvent(new PointerEvent('pointerdown', { button: 0, pointerType: 'touch' }));
    expect(fixture.componentInstance.dragging()).toBe(false);
  });
});

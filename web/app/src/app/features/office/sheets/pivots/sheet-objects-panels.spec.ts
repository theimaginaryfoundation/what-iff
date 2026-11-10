import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { xlsxToWorkbook } from '../../xlsx/xlsx-import';
import { FakeSheetGrid } from '../fake-sheet-grid.spec-helper';
import { SheetSlicersPanelComponent } from '../slicers/sheet-slicers-panel.component';
import { SOURCE_ROWS, slicerWorkbook } from '../slicers/slicer-fixture.spec-helper';
import { SlicerWorkspace } from '../slicers/slicer-workspace';
import { PivotWorkspace } from './pivot-workspace';
import { SheetPivotsPanelComponent } from './sheet-pivots-panel.component';

async function workspaces() {
  const { preserved } = await xlsxToWorkbook(await slicerWorkbook(), { id: 'wb', name: 's.xlsx' });
  const grid = new FakeSheetGrid({ Data: SOURCE_ROWS.map(r => [...r]), Pivot: [[], [], ['Row Labels', 'Sum of Qty'], ['East', 15]] });
  const pivots = new PivotWorkspace(preserved!);
  pivots.attach(grid);
  const slicers = new SlicerWorkspace(preserved!, pivots);
  slicers.attach(grid, null);
  return { grid, pivots, slicers };
}

describe('pivot and slicer panels', () => {
  beforeEach(() => TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] }));

  async function pivotPanel(ctx: Awaited<ReturnType<typeof workspaces>>): Promise<{ fixture: ComponentFixture<SheetPivotsPanelComponent>; el: HTMLElement; changed: ReturnType<typeof vi.fn> }> {
    const fixture = TestBed.createComponent(SheetPivotsPanelComponent);
    fixture.componentRef.setInput('workspace', ctx.pivots);
    fixture.componentRef.setInput('grid', ctx.grid);
    const changed = vi.fn();
    fixture.componentInstance.changed.subscribe(changed);
    fixture.detectChanges();
    await fixture.whenStable();
    return { fixture, el: fixture.nativeElement, changed };
  }

  it('assigns a field to an area and lays the pivot out', async () => {
    const ctx = await workspaces();
    const { fixture, el, changed } = await pivotPanel(ctx);
    const select = el.querySelector('select[aria-label="Area for Year"]') as HTMLSelectElement;
    expect(select.value).toBe('none');
    select.value = 'columns';
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    await fixture.whenStable();
    expect(ctx.pivots.entries()[0].config.columns).toEqual(['Year']);
    expect(ctx.grid.sheets['Pivot'][3]).toEqual(['Row Labels', '2024', '2025', 'Grand Total']);
    expect(changed).toHaveBeenCalled();
  });

  it('changes how a value aggregates, and hides items through its filter', async () => {
    const ctx = await workspaces();
    const { fixture, el } = await pivotPanel(ctx);
    const aggregate = el.querySelector('section, [aria-label="Values"] select') as HTMLSelectElement;
    aggregate.value = 'max';
    aggregate.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    expect(ctx.grid.sheets['Pivot'][2][1]).toBe('Max of Qty');
    (el.querySelector('button[aria-label="Filter Region"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    await fixture.whenStable();
    (el.querySelector('.objects-panel__items input') as HTMLInputElement).click();
    fixture.detectChanges();
    expect(ctx.pivots.entries()[0].config.hidden).toEqual({ Region: ['East'] });
  });

  it('asks before overwriting, and overwrites only when told', async () => {
    const ctx = await workspaces();
    const { fixture, el } = await pivotPanel(ctx);
    ctx.grid.sheets['Pivot'][3][3] = 'mine';
    const select = el.querySelector('select[aria-label="Area for Year"]') as HTMLSelectElement;
    select.value = 'columns';
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    await fixture.whenStable();
    expect(el.querySelector('[role="alertdialog"]')?.textContent).toContain('D4');
    expect(ctx.pivots.entries()[0].config.columns).toEqual([]);
    (Array.from(el.querySelectorAll('button')).find(b => b.textContent?.trim() === 'Overwrite') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(ctx.pivots.entries()[0].config.columns).toEqual(['Year']);
    expect(el.querySelector('[role="alertdialog"]')).toBeNull();
  });

  it('makes a new pivot from the selection, on a new sheet', async () => {
    const ctx = await workspaces();
    ctx.grid.chosen = { sheet: 'Data', range: { startRow: 0, startColumn: 0, endRow: 4, endColumn: 2 } };
    const { fixture, el } = await pivotPanel(ctx);
    (Array.from(el.querySelectorAll('button')).find(b => b.textContent?.includes('New from selection')) as HTMLButtonElement).click();
    fixture.detectChanges();
    fixture.componentInstance.newSheet.set('__new__');
    fixture.detectChanges();
    (Array.from(el.querySelectorAll('button')).find(b => b.textContent?.trim() === 'Create') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(ctx.grid.sheetNames()).toContain('Pivot1');
    expect(ctx.pivots.entries().at(-1)).toMatchObject({ sheet: 'Pivot1', isNew: true });
    expect(fixture.componentInstance.selected()?.name).toBe('PivotTable1');
  });

  it('explains why a pivot cannot be changed', async () => {
    const ctx = await workspaces();
    ctx.pivots.entries.update(list => list.map(e => ({ ...e, limits: ['it uses grouped items'] })));
    const { el } = await pivotPanel(ctx);
    expect(el.textContent).toContain("can't be changed here because it uses grouped items");
    expect(el.querySelector('select[aria-label="Area for Year"]')).toBeNull();
  });

  it('chooses slicer items, one by click and more with Ctrl, and clears', async () => {
    const ctx = await workspaces();
    const fixture = TestBed.createComponent(SheetSlicersPanelComponent);
    fixture.componentRef.setInput('workspace', ctx.slicers);
    const changed = vi.fn();
    fixture.componentInstance.changed.subscribe(changed);
    fixture.detectChanges();
    const el: HTMLElement = fixture.nativeElement;
    const group = () => el.querySelector('[aria-label="Region items"]') as HTMLElement;
    const buttons = () => Array.from(group().querySelectorAll('button'));
    expect(buttons().map(b => b.textContent?.trim())).toEqual(['East', 'North', 'South']);
    buttons()[1].click();
    fixture.detectChanges();
    expect(buttons().map(b => b.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'false']);
    buttons()[2].dispatchEvent(new MouseEvent('click', { ctrlKey: true, bubbles: true }));
    fixture.detectChanges();
    expect(buttons().map(b => b.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'true']);
    expect(changed).toHaveBeenCalledTimes(2);
    (Array.from(el.querySelectorAll('button')).find(b => b.textContent?.trim() === 'Clear filter' && !b.disabled) as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(buttons().every(b => b.getAttribute('aria-pressed') === 'true')).toBe(true);
    (Array.from(el.querySelectorAll('button')).find(b => b.textContent?.trim() === 'Add slicer') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(ctx.slicers.entries()).toHaveLength(3);
  });
});

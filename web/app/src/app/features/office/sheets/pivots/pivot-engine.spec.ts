import { describe, expect, it } from 'vitest';

import { GridValue } from '../sheet-grid';
import { PivotConfig, PivotSourceError, aggregate, compareLabels, computePivot, sourceFields } from './pivot-engine';

const SOURCE: GridValue[][] = [
  ['Region', 'Rep', 'Year', 'Qty', 'Note'],
  ['North', 'Ann', 2024, 10, 'a'],
  ['North', 'Bo', 2024, 5, null],
  ['South', 'Ann', 2024, 25, 'b'],
  ['South', 'Ann', 2025, 15, 'c'],
  ['East', 'Cy', 2025, 2, 'd'],
];
const base = (over: Partial<PivotConfig> = {}): PivotConfig => ({
  rows: ['Region'],
  columns: [],
  values: [{ field: 'Qty', aggregate: 'sum' }],
  filters: [],
  hidden: {},
  rowGrandTotals: true,
  colGrandTotals: true,
  ...over,
});

describe('pivot engine', () => {
  it('groups rows with sorted labels and a grand total', () => {
    const r = computePivot(base(), SOURCE);
    expect(r.cells).toEqual([
      ['Row Labels', 'Sum of Qty'],
      ['East', 2],
      ['North', 15],
      ['South', 40],
      ['Grand Total', 57],
    ]);
    expect([r.firstHeaderRow, r.firstDataRow, r.firstDataCol]).toEqual([1, 1, 1]);
    expect(r.rowItems.map(i => i.kind)).toEqual(['item', 'item', 'item', 'grand']);
  });

  it('nests row fields with the subtotal on the parent', () => {
    const r = computePivot(base({ rows: ['Region', 'Rep'], rowGrandTotals: false }), SOURCE);
    expect(r.cells.slice(1)).toEqual([
      ['East', 2],
      ['   Cy', 2],
      ['North', 15],
      ['   Ann', 10],
      ['   Bo', 5],
      ['South', 40],
      ['   Ann', 40],
    ]);
  });

  it('lays out column fields under Column Labels with totals', () => {
    const r = computePivot(base({ columns: ['Year'] }), SOURCE);
    expect(r.cells).toEqual([
      ['Sum of Qty', 'Column Labels', null, null],
      ['Row Labels', '2024', '2025', 'Grand Total'],
      ['East', null, 2, 2],
      ['North', 15, null, 15],
      ['South', 25, 15, 40],
      ['Grand Total', 40, 17, 57],
    ]);
    expect([r.firstHeaderRow, r.firstDataRow]).toEqual([1, 2]);
  });

  it('puts several value fields side by side, with their own totals under column fields', () => {
    const values = [
      { field: 'Qty', aggregate: 'sum' as const },
      { field: 'Qty', aggregate: 'average' as const },
    ];
    const flat = computePivot(base({ values, rows: ['Rep'] }), SOURCE);
    expect(flat.cells[0]).toEqual(['Row Labels', 'Sum of Qty', 'Average of Qty']);
    expect(flat.cells[1]).toEqual(['Ann', 50, 50 / 3]);
    expect(flat.firstHeaderRow).toBe(0);
    const crossed = computePivot(base({ values, columns: ['Year'], rows: ['Rep'] }), SOURCE);
    expect(crossed.cells[0]).toEqual([null, 'Column Labels', null, null, null, null, null]);
    expect(crossed.cells[1]).toEqual([null, '2024', null, '2025', null, 'Grand Total', null]);
    expect(crossed.cells[2]).toEqual(['Row Labels', 'Sum of Qty', 'Average of Qty', 'Sum of Qty', 'Average of Qty', 'Total Sum of Qty', 'Total Average of Qty']);
    expect(crossed.firstDataRow).toBe(3);
    expect(crossed.colItems[1]).toEqual({ path: ['2024'], kind: 'item', valueIndex: 1 });
  });

  it('subtotals outer column fields', () => {
    const r = computePivot(base({ rows: [], columns: ['Year', 'Rep'], colGrandTotals: false }), SOURCE);
    expect(r.cells[1]).toEqual(['2024', null, '2024 Total', '2025', null, '2025 Total']);
    expect(r.cells[2]).toEqual(['Ann', 'Bo', null, 'Ann', 'Cy', null]);
    expect(r.cells[3]).toEqual([35, 5, 40, 15, 2, 17]);
  });

  it('filters by hidden items in any area, and reports filter selections', () => {
    const r = computePivot(base({ filters: ['Year'], hidden: { Year: ['2024'], Rep: ['Cy'] } }), SOURCE);
    expect(r.cells).toEqual([
      ['Row Labels', 'Sum of Qty'],
      ['South', 15],
      ['Grand Total', 15],
    ]);
    expect(r.pageCells).toEqual([['Year', '2025']]);
    expect(computePivot(base({ filters: ['Rep'] }), SOURCE).pageCells).toEqual([['Rep', '(All)']]);
    expect(computePivot(base({ filters: ['Rep'], hidden: { Rep: ['Cy'] } }), SOURCE).pageCells).toEqual([['Rep', '(Multiple Items)']]);
  });

  it('counts text and treats blanks as their own item', () => {
    const r = computePivot(base({ rows: ['Note'], values: [{ field: 'Note', aggregate: 'count' }], rowGrandTotals: true }), SOURCE);
    expect(r.cells[0]).toEqual(['Row Labels', 'Count of Note']);
    expect(r.cells.at(-2)).toEqual(['(blank)', 0]);
    expect(r.cells.at(-1)).toEqual(['Grand Total', 4]);
    expect(r.fieldItems['Note']).toEqual(['a', 'b', 'c', 'd', '(blank)']);
  });

  it('shows just the total with no row or column fields', () => {
    expect(computePivot(base({ rows: [] }), SOURCE).cells).toEqual([['Sum of Qty'], [57]]);
  });

  it('computes every aggregate', () => {
    const v = [1, 2, 3, 4, null, 'x'];
    expect(aggregate('sum', v)).toBe(10);
    expect(aggregate('count', v)).toBe(5);
    expect(aggregate('countNums', v)).toBe(4);
    expect(aggregate('average', v)).toBe(2.5);
    expect(aggregate('max', v)).toBe(4);
    expect(aggregate('min', v)).toBe(1);
    expect(aggregate('product', v)).toBe(24);
    expect(aggregate('var', v)).toBeCloseTo(1.6667, 3);
    expect(aggregate('varp', v)).toBeCloseTo(1.25, 3);
    expect(aggregate('stdDev', v)).toBeCloseTo(1.291, 3);
    expect(aggregate('stdDevp', v)).toBeCloseTo(1.118, 3);
    expect(aggregate('stdDev', [1])).toBeNull();
  });

  it('sorts numbers, then text, then blanks', () => {
    expect(['b', '(blank)', '10', 'A', '9'].sort(compareLabels)).toEqual(['9', '10', 'A', 'b', '(blank)']);
  });

  it('refuses sources it cannot use, and says why', () => {
    expect(() => sourceFields([['a']])).toThrow(PivotSourceError);
    expect(() => sourceFields([['a', null], [1, 2]])).toThrow(/needs a name/);
    expect(() => sourceFields([['a', 'A'], [1, 2]])).toThrow(/same name/);
    expect(() => computePivot(base({ rows: ['Gone'] }), SOURCE)).toThrow(/no longer has/);
  });
});

import ExcelJS from 'exceljs';
import { existsSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage, parseXml } from '../../shared/ooxml';
import { RESOURCE_DRAWINGS, UniverCell, UniverDrawingsForSheet, UniverWorkbook, readResource } from '../../xlsx/univer-sheet-types';
import { workbookToXlsx } from '../../xlsx/xlsx-export';
import { xlsxToWorkbook } from '../../xlsx/xlsx-import';
import { parsePivotTable, parsePivotCache } from '../../xlsx/xlsx-pivot-xml';
import { FakeSheetGrid } from '../fake-sheet-grid.spec-helper';
import { GridValue } from '../sheet-grid';
import { SOURCE_ROWS, slicerWorkbook } from '../slicers/slicer-fixture.spec-helper';
import { SlicerPictures, SlicerWorkspace } from '../slicers/slicer-workspace';
import { PivotWorkspace } from './pivot-workspace';

const samplesDir = process.env['OFFICE_SAMPLES_DIR'];

async function open() {
  const { workbook, preserved, report } = await xlsxToWorkbook(await slicerWorkbook(), { id: 'wb', name: 's.xlsx' });
  const pivotCells: GridValue[][] = [[], [], ['Row Labels', 'Sum of Qty'], ['East', 15], ['North', 15], ['South', 25], ['Grand Total', 55]];
  const grid = new FakeSheetGrid({ Data: SOURCE_ROWS.map(r => [...r]), Pivot: pivotCells });
  const pivots = new PivotWorkspace(preserved!);
  pivots.attach(grid);
  const calls: string[] = [];
  const pictures: SlicerPictures = {
    update: (e, items) => calls.push(`update ${e.name} ${items.filter(i => i.selected).map(i => i.label)}`),
    add: e => calls.push(`add ${e.name}`),
    remove: e => calls.push(`remove ${e.name}`),
  };
  const slicers = new SlicerWorkspace(preserved!, pivots);
  slicers.attach(grid, pictures);
  return { workbook, preserved: preserved!, report, grid, pivots, slicers, calls };
}

/** The grid's cells into the snapshot, as Univer's would be. */
function syncCells(workbook: UniverWorkbook, grid: FakeSheetGrid): void {
  for (const sheet of Object.values(workbook.sheets)) {
    const rows = grid.sheets[sheet.name];
    if (!rows) continue;
    sheet.cellData = {};
    rows.forEach((row, r) => {
      (row ?? []).forEach((value, c) => {
        if (value === null || value === undefined) return;
        const cell: UniverCell = typeof value === 'number' ? { v: value, t: 2 } : { v: String(value), t: 1 };
        (sheet.cellData[r] ??= {})[c] = cell;
      });
    });
    const hidden = grid.hiddenRows.get(sheet.name);
    for (const r of hidden ?? []) sheet.rowData[r] = { ...(sheet.rowData[r] ?? {}), hd: 1 };
  }
}

async function save(ctx: Awaited<ReturnType<typeof open>>): Promise<{ pkg: OoxmlPackage; bytes: Uint8Array }> {
  ctx.pivots.flush();
  ctx.slicers.flush();
  syncCells(ctx.workbook, ctx.grid);
  const { bytes } = await workbookToXlsx(ctx.workbook, ctx.preserved);
  return { pkg: await OoxmlPackage.open(new Uint8Array(bytes)), bytes };
}

/** Every relationship target exists, and every part has a declared content type. */
async function expectConsistent(pkg: OoxmlPackage): Promise<void> {
  const types = (await pkg.text('[Content_Types].xml')) ?? '';
  for (const path of pkg.paths()) {
    if (path.endsWith('.rels') || path === '[Content_Types].xml') {
      if (path.endsWith('.rels')) {
        const owner = path.replace('_rels/', '').replace(/\.rels$/, '');
        for (const rel of (await pkg.rels(owner)).values()) {
          if (rel.external) continue;
          const dir = owner.includes('/') ? owner.slice(0, owner.lastIndexOf('/') + 1) : '';
          const parts: string[] = [];
          for (const s of (rel.target.startsWith('/') ? rel.target.slice(1) : dir + rel.target).split('/')) {
            if (s === '..') parts.pop();
            else parts.push(s);
          }
          expect(pkg.has(parts.join('/')), `${owner} -> ${rel.target}`).toBe(true);
        }
      }
      continue;
    }
    const ext = path.slice(path.lastIndexOf('.') + 1);
    expect(types.includes(`PartName="/${path}"`) || types.includes(`Extension="${ext}"`), `content type of ${path}`).toBe(true);
    if (path.endsWith('.xml')) parseXml((await pkg.text(path)) ?? '');
  }
}

describe('pivots and slicers, opened and saved', () => {
  it('reads both slicers and keeps everything through a save nobody changed', async () => {
    const ctx = await open();
    expect(ctx.report.dropped).toEqual([]);
    expect(ctx.pivots.entries().map(e => [e.name, e.sheet, e.limits])).toEqual([['Sales', 'Pivot', []]]);
    expect(ctx.slicers.entries().map(e => [e.name, e.kind, e.sheet, e.field, e.excluded])).toEqual(
      expect.arrayContaining([
        ['Region', 'pivot', 'Pivot', 'Region', []],
        ['Year', 'table', 'Data', 'Year', []],
      ]),
    );
    const before = await OoxmlPackage.open(await slicerWorkbook());
    const { pkg } = await save(ctx);
    await expectConsistent(pkg);
    for (const path of ['xl/pivotTables/pivotTable1.xml', 'xl/pivotCache/pivotCacheDefinition1.xml', 'xl/pivotCache/pivotCacheRecords1.xml', 'xl/tables/table1.xml', 'xl/slicers/slicer1.xml', 'xl/slicers/slicer2.xml']) {
      expect(await pkg.text(path), path).toBe(await before.text(path));
    }
    // ExcelJS numbers sheets afresh; the pivot slicer's address follows.
    const book = (await pkg.text('xl/workbook.xml')) ?? '';
    const pivotId = /sheetId="(\d+)"/.exec(/<sheet [^>]*name="Pivot"[^>]*>/.exec(book)?.[0] ?? '')?.[1];
    expect(await pkg.text('xl/slicerCaches/slicerCache1.xml')).toContain(`tabId="${pivotId}"`);
    expect(book).toContain('{BBE1A952-AA13-448e-AADC-164F8A28A991}');
    expect(book).toContain('{46BE6895-7355-4a93-B00E-2C351335B9C9}');
    expect(book).toMatch(/<definedName name="Slicer_Region">#N\/A<\/definedName>/);
    const paths = pkg.paths();
    const dataSheet = (await pkg.text('xl/worksheets/sheet1.xml')) ?? '';
    const pivotSheet = (await pkg.text('xl/worksheets/sheet2.xml')) ?? '';
    expect(dataSheet).toContain('{3A4CF648-6AED-40f4-86FF-DC5316D8AED3}');
    expect(pivotSheet).toContain('{A8765BA9-456A-4dab-B4F3-ACF838C121DE}');
    const ids = [...pivotSheet.matchAll(/<x14:slicer r:id="([^"]+)"/g)].map(m => m[1]);
    const rels = await pkg.rels('xl/worksheets/sheet2.xml');
    expect(rels.get(ids[0])?.target).toContain('slicers/slicer1.xml');
    expect(paths.some(p => /drawing.*\.xml$/.test(p) && !p.endsWith('.rels'))).toBe(true);
    await new ExcelJS.Workbook().xlsx.load(new Uint8Array((await save(ctx)).bytes) as never);
    // Reopens: both slicers still there.
    const again = await xlsxToWorkbook((await save(ctx)).bytes, { id: 'wb2', name: 's.xlsx' });
    expect(Object.keys(again.preserved!.slicers!.sheetSlicers).sort()).toEqual(['Data', 'Pivot']);
  });

  it('draws slicers at their anchors, leaving them out of the pictures it saves', async () => {
    const ctx = await open();
    const drawings = readResource<Record<string, UniverDrawingsForSheet>>(ctx.workbook, RESOURCE_DRAWINGS)!;
    const ids = Object.values(drawings).flatMap(d => d.order);
    expect(ids).toEqual(expect.arrayContaining(['slicer-Region', 'slicer-Year']));
    const image = Object.values(drawings).flatMap(d => Object.values(d.data)).find(i => i.drawingId === 'slicer-Region')!;
    expect(image.sheetTransform.from.column).toBe(3);
    expect(decodeURIComponent(image.source)).toContain('North');
    const { pkg } = await save(ctx);
    expect(pkg.paths().filter(p => /xl\/media/.test(p))).toEqual([]);
  });

  it('filters a pivot from its slicer and writes both states', async () => {
    const ctx = await open();
    ctx.slicers.click('Region', 'North', false);
    expect(ctx.grid.sheets['Pivot'].slice(2, 6).map(r => r.slice(0, 2))).toEqual([
      ['Row Labels', 'Sum of Qty'],
      ['North', 15],
      ['Grand Total', 15],
      [null, null],
    ]);
    ctx.slicers.click('Region', 'South', true);
    expect(ctx.grid.sheets['Pivot'][4]).toEqual(['South', 25]);
    expect(ctx.calls.at(-1)).toBe('update Region North,South');
    const { pkg } = await save(ctx);
    await expectConsistent(pkg);
    const slicerXml = (await pkg.text('xl/slicerCaches/slicerCache1.xml')) ?? '';
    expect(slicerXml).toContain('<i x="2"/>');
    expect(slicerXml).toContain('<i x="0" s="1"/>');
    const pivotXml = (await pkg.text('xl/pivotTables/pivotTable1.xml')) ?? '';
    const cache = parsePivotCache((await pkg.text('xl/pivotCache/pivotCacheDefinition1.xml')) ?? '', 'c');
    expect(parsePivotTable(pivotXml, 'p', cache).config.hidden).toEqual({ Region: ['East'] });
    expect((await pkg.text('xl/pivotCache/pivotCacheDefinition1.xml')) ?? '').toContain('refreshOnLoad="1"');
    expect(cache.pivotCacheId).toBe('77');
    ctx.slicers.clearFilter('Region');
    expect(ctx.grid.sheets['Pivot'][3][0]).toBe('East');
  });

  it('leaves the pivot and the slicer as they were when the pivot would grow over other cells', async () => {
    const ctx = await open();
    ctx.slicers.click('Region', 'North', false);
    const before = ctx.grid.sheets['Pivot'].map(r => [...r]);
    ctx.grid.sheets['Pivot'][5] = ['keep me'];
    ctx.slicers.click('Region', 'South', true);
    expect(ctx.slicers.notice()).toContain('A6');
    expect(ctx.grid.sheets['Pivot'][5]).toEqual(['keep me']);
    expect(ctx.grid.sheets['Pivot'].slice(2, 5)).toEqual(before.slice(2, 5));
    expect(ctx.slicers.entries().find(e => e.name === 'Region')!.excluded).toEqual(['East', 'South']);
  });

  it('filters a table from its slicer, hiding rows and writing the filter', async () => {
    const ctx = await open();
    ctx.slicers.click('Year', '2025', false);
    expect([...ctx.grid.hiddenRows.get('Data')!].sort()).toEqual([1, 2]);
    const { pkg } = await save(ctx);
    const table = (await pkg.text('xl/tables/table1.xml')) ?? '';
    expect(table).toContain('<filterColumn colId="1"><filters><filter val="2025"/></filters></filterColumn>');
    const sheet = (await pkg.text('xl/worksheets/sheet1.xml')) ?? '';
    expect(sheet).toMatch(/<row r="2"[^>]*hidden="1"/);
    ctx.slicers.clearFilter('Year');
    expect(ctx.grid.hiddenRows.get('Data')!.size).toBe(0);
  });

  it('reads back a filter a table already has', async () => {
    const first = await open();
    first.slicers.click('Year', '2024', false);
    const { bytes } = await save(first);
    const { workbook, preserved } = await xlsxToWorkbook(bytes, { id: 'x', name: 's.xlsx' });
    const grid = new FakeSheetGrid({ Data: SOURCE_ROWS.map(r => [...r]), Pivot: [] });
    const pivots = new PivotWorkspace(preserved!);
    pivots.attach(grid);
    const slicers = new SlicerWorkspace(preserved!, pivots);
    slicers.attach(grid, null);
    expect(slicers.entries().find(e => e.name === 'Year')?.excluded).toEqual(['2025']);
    expect(workbook.sheetOrder).toHaveLength(2);
  });

  it('adds a pivot-backed and a table-backed slicer, with all their parts', async () => {
    const ctx = await open();
    expect(ctx.slicers.targets().map(t => t.label)).toEqual(['Pivot: Sales', 'Table: SalesTable']);
    const pivotSlicer = ctx.slicers.add('xl/pivotTables/pivotTable1.xml', 'Year');
    const tableSlicer = ctx.slicers.add('xl/tables/table1.xml', 'Region');
    expect(pivotSlicer?.cacheName).toBe('Slicer_Year1');
    expect(tableSlicer?.name).toBe('Region 1');
    expect(ctx.calls).toEqual(['add Year 1', 'add Region 1']);
    const { pkg } = await save(ctx);
    await expectConsistent(pkg);
    const book = (await pkg.text('xl/workbook.xml')) ?? '';
    expect(book).toContain('<definedName name="Slicer_Year1">#N/A</definedName>');
    expect(book).toContain('<definedName name="Slicer_Region1">#N/A</definedName>');
    const pivotSlicerXml = (await pkg.text('xl/slicerCaches/slicerCache3.xml')) ?? '';
    expect(pivotSlicerXml).toContain('sourceName="Year"');
    expect(pivotSlicerXml).toContain('pivotCacheId="77"');
    const tableSlicerXml = (await pkg.text('xl/slicerCaches/slicerCache4.xml')) ?? '';
    expect(tableSlicerXml).toContain('tableId="5"');
    expect(tableSlicerXml).toContain('column="1"');
    const drawings = await Promise.all(pkg.paths().filter(p => /^xl\/drawings\/[^/]+\.xml$/.test(p)).map(p => pkg.text(p)));
    const frames = drawings.join('');
    expect(frames).toContain('name="Year 1"');
    expect(frames).toContain('Requires="sle15"');
    expect(frames).toContain('mc:Fallback');
    // A pivot field a slicer filters needs an item list even off the axes.
    const pivotXml = (await pkg.text('xl/pivotTables/pivotTable1.xml')) ?? '';
    expect(parseXml(pivotXml).getElementsByTagName('pivotField')[1].getElementsByTagName('item').length).toBe(3);
    if (samplesDir && existsSync(samplesDir)) writeFileSync(join(samplesDir, 'objects-out.xlsx'), (await save(ctx)).bytes);
  });

  it('removes a slicer from every place it is written', async () => {
    const ctx = await open();
    ctx.slicers.remove('Region');
    const { pkg } = await save(ctx);
    await expectConsistent(pkg);
    expect(pkg.has('xl/slicers/slicer1.xml')).toBe(false);
    expect(pkg.has('xl/slicerCaches/slicerCache1.xml')).toBe(false);
    const book = (await pkg.text('xl/workbook.xml')) ?? '';
    expect(book).not.toContain('Slicer_Region');
    expect(book).not.toContain('{BBE1A952-AA13-448e-AADC-164F8A28A991}');
    const drawings = await Promise.all(pkg.paths().filter(p => /^xl\/drawings\/[^/]+\.xml$/.test(p)).map(p => pkg.text(p)));
    expect(drawings.join('')).not.toContain('name="Region"');
    expect(ctx.calls).toEqual(['remove Region']);
  });

  it('changes a pivot, offers a refresh when the source moves, and asks before overwriting', async () => {
    const ctx = await open();
    const id = ctx.pivots.entries()[0].id;
    const entry = ctx.pivots.entries()[0];
    expect(ctx.pivots.fieldsOf(entry)).toEqual(['Region', 'Year', 'Qty']);
    ctx.grid.sheets['Pivot'][9] = ['keep me'];
    expect(ctx.pivots.apply(id, { ...entry.config, columns: ['Year'] })).toEqual({ kind: 'ok' });
    expect(ctx.grid.sheets['Pivot'][2].slice(0, 2)).toEqual(['Sum of Qty', 'Column Labels']);
    expect(ctx.grid.sheets['Pivot'][3]).toEqual(['Row Labels', '2024', '2025', 'Grand Total']);
    expect(ctx.pivots.entries()[0].location.range.endColumn).toBe(3);

    ctx.grid.sheets['Pivot'][5][5] = 'neighbour';
    const wider = { ...ctx.pivots.entries()[0].config, rows: ['Region', 'Year'], columns: [], values: [{ field: 'Qty', aggregate: 'sum' as const }, { field: 'Qty', aggregate: 'max' as const }, { field: 'Qty', aggregate: 'min' as const }, { field: 'Qty', aggregate: 'count' as const }, { field: 'Qty', aggregate: 'average' as const }] };
    expect(ctx.pivots.apply(id, wider)).toEqual({ kind: 'conflict', where: expect.stringMatching(/^F6/) });
    expect(ctx.pivots.apply(id, wider, { force: true })).toEqual({ kind: 'ok' });

    expect(ctx.pivots.sourceChanged()).toBe(false);
    ctx.grid.sheets['Data'][1][2] = 100;
    ctx.grid.changed();
    expect(ctx.pivots.sourceChanged()).toBe(true);
    ctx.pivots.refreshAll();
    expect(ctx.pivots.sourceChanged()).toBe(false);
  });

  it('refuses to lay a pivot over its own source', async () => {
    const ctx = await open();
    const made = ctx.pivots.create({ sheet: 'Data', range: { startRow: 0, startColumn: 0, endRow: 4, endColumn: 2 } }, { sheet: 'Data', row: 2, column: 1 });
    if ('error' in made) throw new Error(made.error);
    expect(ctx.pivots.apply(made.entry.id, { ...made.entry.config, rows: ['Region'] })).toMatchObject({ kind: 'error', message: expect.stringContaining('own source') });
  });

  it('creates a new pivot with every part, and removes one', async () => {
    const ctx = await open();
    const made = ctx.pivots.create({ sheet: 'Data', range: { startRow: 0, startColumn: 0, endRow: 4, endColumn: 2 } }, { sheet: 'Pivot', row: 10, column: 0 });
    if ('error' in made) throw new Error(made.error);
    expect(made.entry.name).toBe('PivotTable1');
    expect(ctx.pivots.apply(made.entry.id, { ...made.entry.config, rows: ['Year'], values: [{ field: 'Qty', aggregate: 'sum' }] })).toEqual({ kind: 'ok' });
    expect(ctx.grid.sheets['Pivot'][11]).toEqual(['2024', 35]);
    const { pkg, bytes } = await save(ctx);
    await expectConsistent(pkg);
    const book = (await pkg.text('xl/workbook.xml')) ?? '';
    expect(book.match(/<pivotCache /g)).toHaveLength(2);
    expect(pkg.has('xl/pivotTables/pivotTable2.xml')).toBe(true);
    const records = (await pkg.text('xl/pivotCache/pivotCacheRecords2.xml')) ?? '';
    expect(records).toContain('count="4"');
    if (samplesDir && existsSync(samplesDir)) writeFileSync(join(samplesDir, 'pivot-out.xlsx'), bytes);
    const again = await xlsxToWorkbook(bytes, { id: 'x', name: 's.xlsx' });
    expect(again.preserved!.bySheet['Pivot'].pivotTables).toHaveLength(2);

    ctx.pivots.remove(made.entry.id);
    expect(ctx.grid.sheets['Pivot'][11]).toEqual([null, null]);
    const after = await save(ctx);
    await expectConsistent(after.pkg);
    expect(after.pkg.has('xl/pivotTables/pivotTable2.xml')).toBe(false);
    expect(((await after.pkg.text('xl/workbook.xml')) ?? '').match(/<pivotCache /g)).toHaveLength(1);
  });

  it('refuses a source without proper headers', async () => {
    const ctx = await open();
    ctx.grid.sheets['Data'][0][1] = null;
    const made = ctx.pivots.create({ sheet: 'Data', range: { startRow: 0, startColumn: 0, endRow: 4, endColumn: 2 } }, { sheet: 'Pivot', row: 10, column: 0 });
    expect(made).toEqual({ error: expect.stringContaining('needs a name') });
  });
});


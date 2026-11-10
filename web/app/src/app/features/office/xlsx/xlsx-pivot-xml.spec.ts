import { describe, expect, it } from 'vitest';

import { computePivot, PivotConfig } from '../sheets/pivots/pivot-engine';
import { GridValue } from '../sheets/sheet-grid';
import { children, elementChildren, parseXml } from '../shared/ooxml';
import {
  SS,
  cacheColumns,
  newPivotCacheXml,
  newPivotTableXml,
  parsePivotCache,
  parsePivotTable,
  pivotRecordsXml,
  updatePivotCacheXml,
  updatePivotTableXml,
} from './xlsx-pivot-xml';

const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const CACHE = `<?xml version="1.0"?><pivotCacheDefinition xmlns="${SS}" xmlns:r="${R}" r:id="rId1" recordCount="3"><cacheSource type="worksheet"><worksheetSource ref="A1:C4" sheet="Data"/></cacheSource><cacheFields count="3"><cacheField name="Region" numFmtId="0"><sharedItems count="3"><s v="North"/><s v="South"/><s v="East"/></sharedItems></cacheField><cacheField name="Year" numFmtId="0"><sharedItems containsSemiMixedTypes="0" containsString="0" containsNumber="1" containsInteger="1" minValue="2024" maxValue="2025" count="2"><n v="2024"/><n v="2025"/></sharedItems></cacheField><cacheField name="Qty" numFmtId="4"><sharedItems containsSemiMixedTypes="0" containsString="0" containsNumber="1" containsInteger="1" minValue="10" maxValue="25"/></cacheField></cacheFields><extLst><ext xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main" uri="{725AE2AE-9491-48be-B2B4-4EB974FC3084}"><x14:pivotCacheDefinition pivotCacheId="7"/></ext></extLst></pivotCacheDefinition>`;
const TABLE = `<?xml version="1.0"?><pivotTableDefinition xmlns="${SS}" name="Sales" cacheId="1" dataCaption="Values" rowGrandTotals="0" updatedVersion="6"><location ref="A3:B6" firstHeaderRow="1" firstDataRow="1" firstDataCol="1" rowPageCount="1" colPageCount="1"/><pivotFields count="3"><pivotField axis="axisRow" showAll="0"><items count="4"><item x="2"/><item x="0"/><item h="1" x="1"/><item t="default"/></items></pivotField><pivotField axis="axisPage" showAll="0"><items count="3"><item x="0"/><item x="1"/><item t="default"/></items></pivotField><pivotField dataField="1" showAll="0"/></pivotFields><rowFields count="1"><field x="0"/></rowFields><rowItems count="2"><i><x/></i><i><x v="1"/></i></rowItems><colItems count="1"><i/></colItems><pageFields count="1"><pageField fld="1" item="1" hier="-1"/></pageFields><dataFields count="1"><dataField name="Total" fld="2" subtotal="average" baseField="0" baseItem="0" numFmtId="4"/></dataFields><formats count="1"><format dxfId="1"><pivotArea/></format></formats><pivotTableStyleInfo name="PivotStyleLight16" showRowHeaders="1"/><extLst><ext uri="{962EF5D1-5CA2-4c93-8EF4-DBF5C05439D2}"/></extLst></pivotTableDefinition>`;
const SOURCE: GridValue[][] = [
  ['Region', 'Year', 'Qty'],
  ['North', 2024, 10],
  ['South', 2025, 25],
  ['East', 2024, 15],
];

/** Children's local names must follow CT_pivotTableDefinition's sequence. */
const ORDER = ['location', 'pivotFields', 'rowFields', 'rowItems', 'colFields', 'colItems', 'pageFields', 'dataFields', 'formats', 'conditionalFormats', 'chartFormats', 'pivotHierarchies', 'pivotTableStyleInfo', 'filters', 'rowHierarchiesUsage', 'colHierarchiesUsage', 'extLst'];
function expectOrdered(root: Element): void {
  const indexes = elementChildren(root).map(el => ORDER.indexOf(el.localName));
  expect(indexes.every(i => i >= 0)).toBe(true);
  expect(indexes).toEqual([...indexes].sort((a, b) => a - b));
}
/** Every element with a `count` attribute must have that many children. */
function expectCounts(root: Element): void {
  for (const el of [root, ...Array.from(root.getElementsByTagName('*'))]) {
    const count = el.getAttribute('count');
    if (count !== null && el.localName !== 'pivotCacheRecords') expect(`${el.localName}:${elementChildren(el).length}`).toBe(`${el.localName}:${count}`);
  }
}

describe('pivot XML', () => {
  it('reads the cache: source, fields, items, the slicer id', () => {
    const cache = parsePivotCache(CACHE, 'c.xml');
    expect(cache.source).toEqual({ kind: 'range', sheet: 'Data', range: { startRow: 0, startColumn: 0, endRow: 3, endColumn: 2 } });
    expect(cache.fields.map(f => f.name)).toEqual(['Region', 'Year', 'Qty']);
    expect(cache.fields[0].items).toEqual(['North', 'South', 'East']);
    expect(cache.fields[1].items).toEqual(['2024', '2025']);
    expect(cache.pivotCacheId).toBe('7');
  });

  it('holds back a pivot whose formatting points at particular items', () => {
    const withRefs = TABLE.replace('<pivotArea/>', '<pivotArea><references count="1"><reference field="0"><x v="1"/></reference></references></pivotArea>');
    expect(parsePivotTable(withRefs, 'p.xml', parsePivotCache(CACHE, 'c.xml')).limits).toEqual([
      'its formatting points at particular items, which a rewrite would move',
    ]);
  });

  it('reads the table into a configuration by field name', () => {
    const model = parsePivotTable(TABLE, 'p.xml', parsePivotCache(CACHE, 'c.xml'));
    expect(model.name).toBe('Sales');
    expect(model.limits).toEqual([]);
    expect(model.location).toMatchObject({ row: 2, column: 0, pageRows: 1 });
    expect(model.config).toEqual({
      rows: ['Region'],
      columns: [],
      filters: ['Year'],
      values: [{ field: 'Qty', aggregate: 'average', name: 'Total', numFmtId: 4 }],
      hidden: { Region: ['South'], Year: ['2024'] },
      rowGrandTotals: false,
      colGrandTotals: true,
    });
  });

  it('names what it cannot edit', () => {
    const grouped = CACHE.replace('<sharedItems count="3">', '<fieldGroup base="0"/><sharedItems count="3">');
    expect(parsePivotTable(TABLE, 'p', parsePivotCache(grouped, 'c')).limits).toContain('it uses grouped items');
    const calc = CACHE.replace('name="Qty"', 'name="Qty" databaseField="0" formula="a"');
    expect(parsePivotTable(TABLE, 'p', parsePivotCache(calc, 'c')).limits).toContain('it uses calculated fields');
    const olap = CACHE.replace('type="worksheet"', 'type="external"');
    expect(parsePivotTable(TABLE, 'p', parsePivotCache(olap, 'c')).limits[0]).toContain('outside this workbook');
    const shown = TABLE.replace('subtotal="average"', 'showDataAs="percentOfTotal"');
    expect(parsePivotTable(shown, 'p', parsePivotCache(CACHE, 'c')).limits).toContain('it shows values as calculations');
  });

  const write = (config: PivotConfig, xml = TABLE, table = SOURCE) => {
    const cache = parsePivotCache(CACHE, 'c');
    const columns = cacheColumns(table, cache.fields, new Set([...config.rows, ...config.columns, ...config.filters]));
    const result = computePivot(config, table);
    return { columns, result, root: parseXml(updatePivotTableXml(xml, { config, result, columns, ref: 'A3:C9' })).documentElement };
  };

  const config: PivotConfig = { rows: ['Region'], columns: ['Year'], values: [{ field: 'Qty', aggregate: 'sum' }], filters: [], hidden: {}, rowGrandTotals: true, colGrandTotals: true };

  it('writes the layout in schema order with counts that match, and keeps style and extensions', () => {
    const { root } = write(config);
    expectOrdered(root);
    expectCounts(root);
    expect(root.getAttribute('name')).toBe('Sales');
    expect(root.getAttribute('rowGrandTotals')).toBeNull();
    expect(children(root, SS, 'pivotTableStyleInfo')).toHaveLength(1);
    expect(children(root, SS, 'extLst')).toHaveLength(1);
    // Whole-table formatting points at no items, so it survives the rewrite.
    expect(children(root, SS, 'formats')).toHaveLength(1);
    const fields = children(children(root, SS, 'pivotFields')[0], SS, 'pivotField');
    expect(fields.map(f => f.getAttribute('axis'))).toEqual(['axisRow', 'axisCol', null]);
    expect(fields[2].getAttribute('dataField')).toBe('1');
    expect(children(root, SS, 'location')[0].getAttribute('firstDataRow')).toBe('2');
    // Items sort for display and point at shared indexes: East is shared 2, North 0, South 1.
    const items = children(children(fields[0], SS, 'items')[0], SS, 'item');
    expect(items.map(i => i.getAttribute('x'))).toEqual(['2', '0', '1', null]);
    expect(items[3].getAttribute('t')).toBe('default');
  });

  it('writes row items with repeats, grand totals and hidden items', () => {
    const { root } = write({ ...config, rows: ['Region', 'Year'], columns: [], hidden: { Region: ['South'] } });
    const rowItems = children(children(root, SS, 'rowItems')[0], SS, 'i');
    // East, 2024; North, 2024; grand: parents, then children repeating one level.
    expect(rowItems.map(i => [i.getAttribute('t'), i.getAttribute('r'), children(i, SS, 'x').map(x => x.getAttribute('v'))])).toEqual([
      [null, null, [null]],
      [null, '1', [null]],
      [null, null, ['1']],
      [null, '1', [null]],
      ['grand', null, [null]],
    ]);
    const south = children(children(children(root, SS, 'pivotFields')[0], SS, 'pivotField')[0], SS, 'items')[0];
    expect(children(south, SS, 'item')[2].getAttribute('h')).toBe('1');
  });

  it('puts several values on a data axis', () => {
    const { root } = write({ ...config, columns: [], values: [{ field: 'Qty', aggregate: 'sum' }, { field: 'Qty', aggregate: 'max' }] });
    expectCounts(root);
    expect(children(children(root, SS, 'colFields')[0], SS, 'field')[0].getAttribute('x')).toBe('-2');
    const col = children(children(root, SS, 'colItems')[0], SS, 'i');
    expect(col[1].getAttribute('i')).toBe('1');
    expect(children(col[1], SS, 'x')[0].getAttribute('v')).toBe('1');
    const data = children(children(root, SS, 'dataFields')[0], SS, 'dataField');
    expect(data[1].getAttribute('subtotal')).toBe('max');
    expect(data[1].getAttribute('name')).toBe('Max of Qty');
  });

  it('writes report filters, one chosen item or many', () => {
    const one = write({ ...config, columns: [], filters: ['Year'], hidden: { Year: ['2024'] } }).root;
    const page = children(children(one, SS, 'pageFields')[0], SS, 'pageField')[0];
    expect(page.getAttribute('fld')).toBe('1');
    expect(page.getAttribute('item')).toBe('1');
    expect(children(one, SS, 'location')[0].getAttribute('rowPageCount')).toBe('1');
    const table: GridValue[][] = [...SOURCE, ['West', 2026, 1]];
    const many = write({ ...config, columns: [], filters: ['Year'], hidden: { Year: ['2024'] } }, TABLE, table).root;
    const field = children(children(many, SS, 'pivotFields')[0], SS, 'pivotField')[1];
    expect(field.getAttribute('multipleItemSelectionAllowed')).toBe('1');
    expect(children(children(many, SS, 'pageFields')[0], SS, 'pageField')[0].getAttribute('item')).toBeNull();
  });

  it('rewrites the cache from the source, keeping item order and the slicer extension', () => {
    const cache = parsePivotCache(CACHE, 'c');
    const table: GridValue[][] = [...SOURCE, ['West', 2024, null], ['North', 2025, 3.5]];
    const columns = cacheColumns(table, cache.fields, new Set(['Region']));
    const root = parseXml(updatePivotCacheXml(CACHE, columns, 5, { previous: cache.fields, ref: 'A1:C6' })).documentElement;
    expectCounts(root);
    expect(root.getAttribute('refreshOnLoad')).toBe('1');
    expect(root.getAttribute('recordCount')).toBe('5');
    expect(root.getElementsByTagName('worksheetSource')[0].getAttribute('ref')).toBe('A1:C6');
    const reread = parsePivotCache(`<?xml version="1.0"?>${new XMLSerializer().serializeToString(root)}`, 'c');
    expect(reread.fields[0].items).toEqual(['North', 'South', 'East', 'West']);
    expect(reread.pivotCacheId).toBe('7');
    const qty = root.getElementsByTagName('cacheField')[2];
    expect(qty.getAttribute('numFmtId')).toBe('4');
    expect(qty.getElementsByTagName('sharedItems')[0].getAttribute('containsBlank')).toBe('1');
    const records = pivotRecordsXml(table, columns);
    expect(records).toContain('count="5"');
    expect(records).toContain('<r><x v="3"/>');
    expect(records).not.toContain('undefined');
  });

  it('builds a new pivot and its cache that read back as written', () => {
    const cacheXml = newPivotCacheXml('Data', 'A1:C4');
    const columns = cacheColumns(SOURCE, [], new Set(['Region']));
    const updated = updatePivotCacheXml(cacheXml, columns, 3, { pivotCacheId: '9' });
    const cache = parsePivotCache(updated, 'c');
    expect(cache.pivotCacheId).toBe('9');
    const cfg = { ...config, columns: [] };
    const xml = updatePivotTableXml(newPivotTableXml('PivotTable1', '5'), { config: cfg, columns, result: computePivot(cfg, SOURCE), ref: 'E1:F5' });
    expectOrdered(parseXml(xml).documentElement);
    const model = parsePivotTable(xml, 'p', cache);
    expect(model.config).toMatchObject({ rows: ['Region'], values: [{ field: 'Qty', aggregate: 'sum', name: 'Sum of Qty' }] });
    expect(model.location.range).toEqual({ startRow: 0, startColumn: 4, endRow: 4, endColumn: 5 });
  });

  it('escapes names', () => {
    const table: GridValue[][] = [['A & "B"', 'Qty'], ['<x>', 1]];
    const columns = cacheColumns(table, [], new Set(['A & "B"']));
    const xml = updatePivotCacheXml(newPivotCacheXml('It\'s & "me"', 'A1:B2'), columns, 1);
    expect(parsePivotCache(xml, 'c').fields[0]).toMatchObject({ name: 'A & "B"', items: ['<x>'] });
  });
});

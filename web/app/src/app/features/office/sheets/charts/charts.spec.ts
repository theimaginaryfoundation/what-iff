import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import ExcelJS from 'exceljs';
import JSZip from 'jszip';

import { parseXml } from '../../shared/ooxml';
import { UniverWorkbook } from '../../xlsx/univer-sheet-types';
import { applyChartChanges, chartSpecsFromPreserved, drawingBaseline } from '../../xlsx/xlsx-chart-save';
import { newChartXml } from '../../xlsx/xlsx-chart-write';
import { workbookToXlsx } from '../../xlsx/xlsx-export';
import { xlsxToWorkbook } from '../../xlsx/xlsx-import';
import { PreservedParts } from '../../xlsx/xlsx-preserve';
import { parseThemeColors } from '../../xlsx/xlsx-theme';
import { FakeSheetGrid } from '../fake-sheet-grid.spec-helper';
import { GridValue } from '../sheet-grid';
import { ChartSession } from './chart-session';
import { C, ChartSpec, chartDataFor, chartSpecFromXml, formatChartRef, liveSeries, parseChartRef, sameAsCached, seriesRef, specFromSelection } from './chart-spec';
import { FakeSheetDrawings } from './fake-sheet-drawings.spec-helper';
import { SheetChartsPanelComponent } from './sheet-charts-panel.component';

const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const OUT_DIR = process.env['CHARTS_OUT_DIR'];
const rels = (entries: string) => `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${entries}</Relationships>`;
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEBgIApD5fRAAAAABJRU5ErkJggg==';

const CHART_XML = (kind = 'barChart', extra = '') =>
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<c:chartSpace xmlns:c="${C}" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="${R}"><c:roundedCorners val="0"/><c:chart>` +
  `<c:title><c:tx><c:rich><a:bodyPr/><a:p><a:r><a:rPr sz="1400" b="1"/><a:t>Quantity</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title><c:autoTitleDeleted val="0"/><c:plotArea><c:layout/>` +
  `<c:${kind}>${kind === 'barChart' ? '<c:barDir val="col"/><c:grouping val="clustered"/>' : ''}<c:varyColors val="0"/><c:ser><c:idx val="0"/><c:order val="0"/><c:tx><c:strRef><c:f>'My Data'!$B$1</c:f><c:strCache><c:ptCount val="1"/><c:pt idx="0"><c:v>Qty</c:v></c:pt></c:strCache></c:strRef></c:tx>` +
  `<c:spPr><a:solidFill><a:schemeClr val="accent2"/></a:solidFill></c:spPr><c:invertIfNegative val="0"/>${extra}<c:cat><c:strRef><c:f>'My Data'!$A$2:$A$4</c:f><c:strCache><c:ptCount val="3"/><c:pt idx="0"><c:v>North</c:v></c:pt><c:pt idx="1"><c:v>South</c:v></c:pt><c:pt idx="2"><c:v>East</c:v></c:pt></c:strCache></c:strRef></c:cat>` +
  `<c:val><c:numRef><c:f>'My Data'!$B$2:$B$4</c:f><c:numCache><c:formatCode>#,##0</c:formatCode><c:ptCount val="3"/><c:pt idx="0"><c:v>10</c:v></c:pt><c:pt idx="1"><c:v>25</c:v></c:pt><c:pt idx="2"><c:v>15</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser>` +
  `<c:gapWidth val="77"/><c:axId val="1"/><c:axId val="2"/></c:${kind}><c:catAx><c:axId val="1"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:crossAx val="2"/></c:catAx><c:valAx><c:axId val="2"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="l"/><c:crossAx val="1"/></c:valAx></c:plotArea>` +
  `<c:legend><c:legendPos val="r"/><c:overlay val="0"/></c:legend><c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/></c:chart><c:extLst><c:ext uri="{KEEP}"><keep xmlns="urn:x">me</keep></c:ext></c:extLst></c:chartSpace>`;

const ANCHOR = (from: number, inner: string) =>
  `<xdr:twoCellAnchor><xdr:from><xdr:col>${from}</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>1</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>${from + 6}</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>12</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>${inner}<xdr:clientData/></xdr:twoCellAnchor>`;

/** "My Data" with a chart, a picture, a shape and a chart style part, as the OOXML parts Excel writes. */
async function workbookBytes(chartXml = CHART_XML()): Promise<ArrayBuffer> {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('My Data');
  ws.addRows([
    ['Region', 'Qty', 'Cost'],
    ['North', 10, 3],
    ['South', 25, 7],
    ['East', 15, 5],
  ]);
  wb.addWorksheet('Other').addRows([
    ['Item', 'Value'],
    ['A', 1],
    ['B', 2],
  ]);
  const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
  const text = (p: string) => zip.file(p)!.async('string');
  zip.file('xl/charts/chart1.xml', chartXml);
  zip.file('xl/charts/style1.xml', '<cs:chartStyle xmlns:cs="http://schemas.microsoft.com/office/drawing/2012/chartStyle" id="201"/>');
  zip.file('xl/charts/_rels/chart1.xml.rels', rels('<Relationship Id="rId1" Type="http://schemas.microsoft.com/office/2011/relationships/chartStyle" Target="style1.xml"/>'));
  zip.file('xl/media/image1.png', PNG, { base64: true });
  zip.file(
    'xl/drawings/drawing1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="${R}">` +
      ANCHOR(4, `<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="2" name="Chart 1"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="${C}"><c:chart xmlns:c="${C}" r:id="rId1"/></a:graphicData></a:graphic></xdr:graphicFrame>`) +
      ANCHOR(12, `<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="3" name="Picture 1"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rId2"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic>`) +
      ANCHOR(20, `<xdr:sp macro="" textlink=""><xdr:nvSpPr><xdr:cNvPr id="4" name="Box"/><xdr:cNvSpPr/></xdr:nvSpPr><xdr:spPr><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:sp>`) +
      `</xdr:wsDr>`,
  );
  zip.file('xl/drawings/_rels/drawing1.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/chart" Target="../charts/chart1.xml"/><Relationship Id="rId2" Type="${R}/image" Target="../media/image1.png"/>`));
  zip.file('xl/worksheets/sheet1.xml', (await text('xl/worksheets/sheet1.xml')).replace('</worksheet>', '<drawing r:id="rId1"/></worksheet>'));
  zip.file('xl/worksheets/_rels/sheet1.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/drawing" Target="../drawings/drawing1.xml"/>`));
  zip.file(
    '[Content_Types].xml',
    (await text('[Content_Types].xml')).replace(
      '</Types>',
      '<Default Extension="png" ContentType="image/png"/><Override PartName="/xl/charts/chart1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/><Override PartName="/xl/charts/style1.xml" ContentType="application/vnd.ms-office.chartstyle+xml"/><Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/></Types>',
    ),
  );
  return zip.generateAsync({ type: 'arraybuffer' });
}

interface Opened {
  workbook: UniverWorkbook;
  preserved: PreservedParts;
  grid: FakeSheetGrid;
  drawings: FakeSheetDrawings;
  session: ChartSession;
  edited: ReturnType<typeof vi.fn>;
}

/** Reads the file and stands up a session over in-memory cells, as the sheets editor does over Univer. */
async function open(bytes: ArrayBuffer): Promise<Opened> {
  const { workbook, preserved } = await xlsxToWorkbook(bytes, { id: 'wb', name: 'book.xlsx' });
  const sheets: Record<string, GridValue[][]> = {};
  for (const id of workbook.sheetOrder) {
    const sheet = workbook.sheets[id];
    const rows: GridValue[][] = [];
    for (const [r, row] of Object.entries(sheet.cellData)) for (const [c, cell] of Object.entries(row ?? {})) (rows[Number(r)] ??= [])[Number(c)] = (cell?.v ?? null) as GridValue;
    sheets[sheet.name] = rows;
  }
  const grid = new FakeSheetGrid(sheets);
  const drawings = new FakeSheetDrawings(workbook);
  const theme = parseThemeColors(preserved!.extras?.themes?.['theme1']);
  const edited = vi.fn();
  const session = new ChartSession(chartSpecsFromPreserved(preserved!, workbook, theme), drawingBaseline(workbook), grid, drawings, theme, edited);
  session.start();
  return { workbook, preserved: preserved!, grid, drawings, session, edited };
}

async function save(o: Opened, name?: string): Promise<JSZip> {
  const { bytes } = await workbookToXlsx(o.workbook, applyChartChanges(o.preserved, o.workbook, o.session.saveState()));
  if (OUT_DIR && name) {
    mkdirSync(OUT_DIR, { recursive: true });
    writeFileSync(join(OUT_DIR, name), bytes);
  }
  // Every save must load in another reader.
  await new ExcelJS.Workbook().xlsx.load((await new Blob([bytes as BlobPart]).arrayBuffer()) as never);
  return JSZip.loadAsync(bytes);
}

const partText = (zip: JSZip, path: string) => zip.file(path)!.async('string');
const firstSpec = (o: Opened): ChartSpec => o.session.charts()[0];

/** Every relationship target exists, and every part has a content type. */
async function expectConsistent(zip: JSZip): Promise<void> {
  const types = await partText(zip, '[Content_Types].xml');
  for (const path of Object.keys(zip.files)) {
    if (zip.files[path].dir || path === '[Content_Types].xml') continue;
    const ext = path.slice(path.lastIndexOf('.') + 1);
    expect(types.includes(`/${path}"`) || types.includes(`Extension="${ext}"`), `type of ${path}`).toBe(true);
    if (/\.xml$|\.rels$/.test(path)) parseXml(await partText(zip, path));
    if (!path.endsWith('.rels')) continue;
    const owner = path.replace('_rels/', '').replace(/\.rels$/, '');
    const dir = owner.includes('/') ? owner.slice(0, owner.lastIndexOf('/') + 1) : '';
    for (const m of (await partText(zip, path)).matchAll(/Target="([^"]+)"(?! TargetMode)/g)) {
      const parts: string[] = [];
      const target = m[1].startsWith('/') ? m[1].slice(1) : dir + m[1];
      for (const s of target.split('/')) {
        if (s === '..') parts.pop();
        else if (s !== '.') parts.push(s);
      }
      expect(zip.file(parts.join('/')), `${path} -> ${m[1]}`).not.toBeNull();
    }
  }
}

describe('chart references', () => {
  it('reads plain ranges, quoted names and absolute refs, and nothing else', () => {
    expect(parseChartRef('Sheet1!$A$2:$A$4')).toEqual({ sheet: 'Sheet1', range: { startRow: 1, startColumn: 0, endRow: 3, endColumn: 0 } });
    expect(parseChartRef("'My ''Data'!B1")).toEqual({ sheet: "My 'Data", range: { startRow: 0, startColumn: 1, endRow: 0, endColumn: 1 } });
    for (const unread of ['[1]Sheet1!$A$1', "'[1]Sheet 1'!$A$1", 'MyName', '(Sheet1!$A$1,Sheet1!$B$1)', 'Sheet1!$A:$A']) expect(parseChartRef(unread), unread).toBeNull();
  });

  it('writes them back as Excel does', () => {
    expect(formatChartRef({ sheet: 'Sheet1', range: { startRow: 1, startColumn: 0, endRow: 3, endColumn: 0 } })).toBe('Sheet1!$A$2:$A$4');
    expect(formatChartRef({ sheet: "My 'Data", range: { startRow: 0, startColumn: 1, endRow: 0, endColumn: 1 } })).toBe("'My ''Data'!$B$1");
  });
});

describe('chartSpecFromXml', () => {
  const ctx = { id: 'c', sheet: 'S', name: 'Chart', sourcePath: 'p', anchor: { from: { col: 0, colOff: 0, row: 0, rowOff: 0 }, to: { col: 1, colOff: 0, row: 1, rowOff: 0 } }, theme: parseThemeColors(undefined) };

  it('reads what the panel edits', () => {
    const spec = chartSpecFromXml(parseXml(CHART_XML()), ctx)!;
    expect(spec).toMatchObject({ kind: 'column', grouping: 'standard', title: 'Quantity', legend: 'r', editable: true, visibleOnly: true });
    expect(spec.series[0].values?.cells?.sheet).toBe('My Data');
    expect(spec.series[0].nameRef?.formula).toBe("'My Data'!$B$1");
    expect(spec.series[0].cached).toMatchObject({ name: 'Qty', labels: ['North', 'South', 'East'], values: [10, 25, 15] });
    expect(spec.series[0].color).toBe(ctx.theme.scheme[5]);
  });

  it('marks what stays as it is', () => {
    expect(chartSpecFromXml(parseXml(CHART_XML().replace(/barChart/g, 'bar3DChart')), ctx)?.editable).toBe(false);
    const combo = CHART_XML().replace('</c:barChart>', '</c:barChart><c:lineChart><c:grouping val="standard"/><c:ser><c:idx val="1"/><c:order val="1"/></c:ser></c:lineChart>');
    expect(chartSpecFromXml(parseXml(combo), ctx)?.editable).toBe(false);
    expect(chartSpecFromXml(parseXml(CHART_XML().replace(/barChart/g, 'bubbleChart')), ctx)?.editable).toBe(false);
    const trend = chartSpecFromXml(parseXml(CHART_XML('barChart', '<c:trendline><c:trendlineType val="linear"/></c:trendline>')), ctx)!;
    expect(trend.editable).toBe(true);
    expect(trend.limits).toContain('Trendlines and error bars');
  });
});

describe('specFromSelection', () => {
  const grid = new FakeSheetGrid({ S: [['Region', 'Qty', 'Cost'], ['North', 10, 3], ['South', 25, 7]] });

  it('takes headers from the first row and labels from the first column', () => {
    const spec = specFromSelection('c', grid, 'S', { startRow: 0, startColumn: 0, endRow: 2, endColumn: 2 }, 'Chart')!;
    expect(spec.series).toHaveLength(2);
    expect(spec.series[0].nameRef?.formula).toBe('S!$B$1');
    expect(spec.series[0].categories?.formula).toBe('S!$A$2:$A$3');
    expect(spec.series[1].values?.formula).toBe('S!$C$2:$C$3');
  });

  it('takes bare numbers as one nameless series', () => {
    const spec = specFromSelection('c', grid, 'S', { startRow: 1, startColumn: 1, endRow: 2, endColumn: 1 }, 'Chart')!;
    expect(spec.series).toHaveLength(1);
    expect(spec.series[0].nameLiteral).toBe('Series 1');
    expect(spec.series[0].categories).toBeNull();
  });
});

describe('live data', () => {
  it('leaves hidden rows out when the chart does, and matches caches however numbers print', async () => {
    const o = await open(await workbookBytes());
    const spec = firstSpec(o);
    expect(sameAsCached(spec, liveSeries(spec, o.grid))).toBe(true);
    o.grid.setRowsHidden('My Data', 2, 1, true);
    expect(liveSeries(spec, o.grid)[0].values).toEqual([10, 15]);
    expect(sameAsCached(spec, liveSeries(spec, o.grid))).toBe(false);
  });

  it('stacks and normalises what the renderer draws', async () => {
    const o = await open(await workbookBytes());
    const live = [{ name: 'a', labels: ['x'], values: [1], numericLabels: false }, { name: 'b', labels: ['x'], values: [3], numericLabels: false }];
    const base = { ...firstSpec(o), series: [firstSpec(o).series[0], firstSpec(o).series[0]] };
    expect(chartDataFor({ ...base, grouping: 'percentStacked' }, live).series.map(s => s.values)).toEqual([[25], [75]]);
    expect(chartDataFor({ ...base, kind: 'line', grouping: 'stacked' }, live).series.map(s => s.values)).toEqual([[1], [4]]);
    expect(chartDataFor({ ...base, legend: 'none' }, live).legend).toBe(false);
  });

  it('redraws only when the data or the size changes', async () => {
    const o = await open(await workbookBytes());
    o.grid.changed();
    expect(o.drawings.sources).toHaveLength(0);
    o.grid.write('My Data', 1, 1, [[99]]);
    expect(o.drawings.sources).toHaveLength(1);
    expect(decodeURIComponent(o.drawings.sources[0].source)).toContain('<svg');
    o.grid.changed();
    expect(o.drawings.sources).toHaveLength(1);
  });

  it('follows a picture deleted in the grid, and undone', async () => {
    const o = await open(await workbookBytes());
    const set = o.drawings.ids('My Data');
    const id = firstSpec(o).id;
    expect(set).toContain(id);
    const saved = o.workbook.resources.find(r => r.name === 'SHEET_DRAWING_PLUGIN')!.data;
    o.drawings.remove('My Data', id);
    o.grid.changed();
    expect(o.session.charts()).toHaveLength(0);
    o.workbook.resources.find(r => r.name === 'SHEET_DRAWING_PLUGIN')!.data = saved;
    o.grid.changed();
    expect(o.session.charts()).toHaveLength(1);
  });
});

describe('saving charts', () => {
  it('leaves an untouched chart and drawing byte for byte', async () => {
    const o = await open(await workbookBytes());
    const zip = await save(o);
    expect(await partText(zip, 'xl/charts/chart1.xml')).toBe(CHART_XML());
    const drawing = Object.keys(zip.files).find(p => /^xl\/drawings\/drawing[^/]*\.xml$/.test(p))!;
    expect(await partText(zip, drawing)).toContain('name="Box"');
    expect(await partText(zip, drawing)).toContain('<xdr:col>4</xdr:col>');
    await expectConsistent(zip);
  });

  it('refreshes only the caches when the cells changed', async () => {
    const o = await open(await workbookBytes());
    o.grid.write('My Data', 2, 0, [['Southern', 30]]);
    const xml = await partText(await save(o, 'refreshed.xlsx'), 'xl/charts/chart1.xml');
    expect(xml).toContain('<c:v>30</c:v>');
    expect(xml).toContain('<c:v>Southern</c:v>');
    expect(xml).toContain('<c:formatCode>#,##0</c:formatCode>');
    expect(xml).toContain('<a:rPr sz="1400" b="1"/>');
    expect(xml).toContain('<keep xmlns="urn:x">me</keep>');
    expect(xml).toContain('<c:gapWidth val="77"/>');
    expect(xml).toContain('<a:schemeClr val="accent2"/>');
  });

  it('patches an edit in place and keeps what it does not model', async () => {
    const o = await open(await workbookBytes());
    const spec = firstSpec(o);
    o.session.update(spec.id, s => ({
      ...s,
      title: 'New title',
      legend: 'b',
      valueAxisTitle: 'Units',
      series: [{ ...s.series[0], values: seriesRef({ sheet: 'My Data', range: { startRow: 1, startColumn: 2, endRow: 3, endColumn: 2 } }) }],
    }));
    expect(o.edited).toHaveBeenCalled();
    const zip = await save(o, 'edited.xlsx');
    const xml = await partText(zip, 'xl/charts/chart1.xml');
    expect(xml).toContain('<a:rPr sz="1400" b="1"/><a:t>New title</a:t>');
    expect(xml).toContain('<c:legendPos val="b"/>');
    expect(xml).toContain("<c:f>'My Data'!$C$2:$C$4</c:f>");
    expect(xml).toContain('<c:v>7</c:v>');
    expect(xml).toContain('Units');
    expect(xml).toContain('<keep xmlns="urn:x">me</keep>');
    expect(xml).toContain('<a:schemeClr val="accent2"/>');
    expect(zip.file('xl/charts/style1.xml')).not.toBeNull();
    await expectConsistent(zip);
  });

  it('saving twice from the same state is the same as saving once', async () => {
    const o = await open(await workbookBytes());
    o.session.update(firstSpec(o).id, s => ({ ...s, title: 'Once' }));
    const first = await partText(await save(o), 'xl/charts/chart1.xml');
    expect(await partText(await save(o), 'xl/charts/chart1.xml')).toBe(first);
  });

  it('changes the chart type with the schema of the new type', async () => {
    const o = await open(await workbookBytes());
    const id = firstSpec(o).id;
    const read = async (name: string) => parseXml(await partText(await save(o, name), 'xl/charts/chart1.xml'));
    const names = (doc: Document, local: string) => Array.from(doc.getElementsByTagNameNS(C, local)[0].childNodes).map(n => (n as Element).localName);

    o.session.update(id, s => ({ ...s, kind: 'line', grouping: 'stacked' }));
    let doc = await read('line.xlsx');
    expect(doc.getElementsByTagNameNS(C, 'barChart')).toHaveLength(0);
    expect(names(doc, 'lineChart')).toEqual(['grouping', 'varyColors', 'ser', 'marker', 'axId', 'axId']);
    expect(names(doc, 'ser')).toEqual(['idx', 'order', 'tx', 'spPr', 'cat', 'val', 'smooth']);

    o.session.update(id, s => ({ ...s, kind: 'pie', grouping: 'standard' }));
    doc = await read('pie.xlsx');
    expect(names(doc, 'pieChart')).toEqual(['varyColors', 'ser', 'firstSliceAng']);
    expect(doc.getElementsByTagNameNS(C, 'catAx')).toHaveLength(0);

    o.session.update(id, s => ({ ...s, kind: 'scatter' }));
    doc = await read('scatter.xlsx');
    expect(names(doc, 'scatterChart')).toEqual(['scatterStyle', 'varyColors', 'ser', 'axId', 'axId']);
    expect(names(doc, 'ser')).toEqual(['idx', 'order', 'tx', 'spPr', 'xVal', 'yVal', 'smooth']);
    expect(doc.getElementsByTagNameNS(C, 'valAx')).toHaveLength(2);

    o.session.update(id, s => ({ ...s, kind: 'bar', grouping: 'percentStacked' }));
    doc = await read('bar.xlsx');
    expect(names(doc, 'barChart')).toEqual(['barDir', 'grouping', 'varyColors', 'ser', 'gapWidth', 'overlap', 'axId', 'axId']);
    expect(doc.getElementsByTagNameNS(C, 'barDir')[0].getAttribute('val')).toBe('bar');
    expect(doc.getElementsByTagNameNS(C, 'catAx')[0].getElementsByTagNameNS(C, 'axPos')[0].getAttribute('val')).toBe('l');
  });

  it('adds, recolours and removes series', async () => {
    const o = await open(await workbookBytes());
    const id = firstSpec(o).id;
    o.session.update(id, s => ({
      ...s,
      series: [
        { ...s.series[0], color: '#112233' },
        { ...s.series[0], sourceIndex: null, nameRef: null, nameLiteral: 'Cost', values: seriesRef({ sheet: 'My Data', range: { startRow: 1, startColumn: 2, endRow: 3, endColumn: 2 } }), color: null },
      ],
    }));
    let doc = parseXml(await partText(await save(o), 'xl/charts/chart1.xml'));
    let sers = Array.from(doc.getElementsByTagNameNS(C, 'ser'));
    expect(sers).toHaveLength(2);
    expect(sers[0].getElementsByTagName('a:srgbClr')[0].getAttribute('val')).toBe('112233');
    expect(sers[1].getElementsByTagNameNS(C, 'idx')[0].getAttribute('val')).toBe('1');
    expect(sers[1].getElementsByTagNameNS(C, 'order')[0].getAttribute('val')).toBe('1');
    expect(sers[1].getElementsByTagNameNS(C, 'v')[0].textContent).toBe('Cost');
    o.session.update(id, s => ({ ...s, series: s.series.slice(1) }));
    doc = parseXml(await partText(await save(o), 'xl/charts/chart1.xml'));
    sers = Array.from(doc.getElementsByTagNameNS(C, 'ser'));
    expect(sers).toHaveLength(1);
    expect(sers[0].getElementsByTagNameNS(C, 'v')[0].textContent).toBe('Cost');
  });

  it('never rewrites a chart it cannot edit', async () => {
    const xml = CHART_XML().replace(/barChart/g, 'bar3DChart');
    const o = await open(await workbookBytes(xml));
    o.grid.write('My Data', 1, 1, [[999]]);
    o.session.update(firstSpec(o).id, s => ({ ...s, title: 'Nope' }));
    expect(o.drawings.sources).toHaveLength(0);
    expect(await partText(await save(o), 'xl/charts/chart1.xml')).toBe(xml);
  });

  it('makes a new chart on a sheet with no drawing, with every part in place', async () => {
    const o = await open(await workbookBytes());
    o.grid.active = 'Other';
    o.grid.chosen = { sheet: 'Other', range: { startRow: 0, startColumn: 0, endRow: 2, endColumn: 1 } };
    const spec = await o.session.addFromSelection();
    expect(spec?.series[0].values?.formula).toBe('Other!$B$2:$B$3');
    const zip = await save(o, 'new.xlsx');
    await expectConsistent(zip);
    const xml = await partText(zip, 'xl/charts/chartNew1.xml');
    expect(xml).toContain('<c:f>Other!$B$2:$B$3</c:f>');
    expect(xml).toContain('<c:v>2</c:v>');
    const doc = parseXml(xml);
    expect(doc.getElementsByTagNameNS(C, 'catAx')).toHaveLength(1);
    expect((await partText(zip, 'xl/worksheets/sheet2.xml')).includes('<drawing r:id=')).toBe(true);
    // The other sheet's chart is still there too.
    expect(zip.file('xl/charts/chart1.xml')).not.toBeNull();
    expect(Object.keys(zip.files).filter(p => /^xl\/drawings\/[^/]*\.xml$/.test(p))).toHaveLength(2);
  });

  it('refuses a selection with nothing to chart', async () => {
    const o = await open(await workbookBytes());
    o.grid.chosen = { sheet: 'My Data', range: { startRow: 0, startColumn: 0, endRow: 0, endColumn: 0 } };
    expect(await o.session.addFromSelection()).toBeNull();
    expect(o.edited).not.toHaveBeenCalled();
  });

  it('adds a chart beside the pictures of a sheet that had no kept drawing', async () => {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('Pics');
    ws.addRows([['Item', 'Value'], ['A', 1], ['B', 2]]);
    ws.addImage(wb.addImage({ base64: PNG, extension: 'png' }), { tl: { col: 5, row: 5 }, ext: { width: 40, height: 40 } } as never);
    const o = await open((await wb.xlsx.writeBuffer()) as ArrayBuffer);
    o.grid.chosen = { sheet: 'Pics', range: { startRow: 0, startColumn: 0, endRow: 2, endColumn: 1 } };
    await o.session.addFromSelection();
    const zip = await save(o);
    await expectConsistent(zip);
    const drawing = Object.keys(zip.files).find(p => /^xl\/drawings\/[^/]*\.xml$/.test(p))!;
    const xml = await partText(zip, drawing);
    expect(xml).toContain('<xdr:pic>');
    expect(xml).toContain('<c:chart');
  });

  it('deletes a chart with its anchor, relationship, parts and content types', async () => {
    const o = await open(await workbookBytes());
    o.session.remove(firstSpec(o).id);
    const zip = await save(o, 'deleted.xlsx');
    await expectConsistent(zip);
    expect(zip.file('xl/charts/chart1.xml')).toBeNull();
    expect(zip.file('xl/charts/style1.xml')).toBeNull();
    expect(await partText(zip, '[Content_Types].xml')).not.toContain('chart1.xml');
    const drawing = Object.keys(zip.files).find(p => /^xl\/drawings\/[^/]*\.xml$/.test(p))!;
    expect(await partText(zip, drawing)).not.toContain('<c:chart');
    expect(await partText(zip, drawing)).toContain('name="Picture 1"');
  });

  it('saves moved charts and pictures where they now are, and drops removed pictures', async () => {
    const o = await open(await workbookBytes());
    const chart = firstSpec(o).id;
    const picture = o.drawings.ids('My Data').find(i => i.startsWith('image-'))!;
    o.drawings.move('My Data', chart, 30, 40);
    o.drawings.move('My Data', picture, 50, 60);
    let zip = await save(o, 'moved.xlsx');
    const path = Object.keys(zip.files).find(p => /^xl\/drawings\/[^/]*\.xml$/.test(p))!;
    let xml = await partText(zip, path);
    expect(xml).toContain('<xdr:col>30</xdr:col>');
    expect(xml).toContain('<xdr:row>40</xdr:row>');
    expect(xml).toContain('<xdr:col>50</xdr:col>');
    expect(xml).toContain('<xdr:row>60</xdr:row>');
    expect(xml).toContain('<xdr:col>20</xdr:col>');
    o.drawings.remove('My Data', picture);
    zip = await save(o);
    xml = await partText(zip, path);
    expect(xml).not.toContain('name="Picture 1"');
    expect(xml).toContain('name="Box"');
  });

  it('saves a picture added on a sheet with a kept drawing', async () => {
    const o = await open(await workbookBytes());
    await o.drawings.insert('My Data', { id: 'pasted', source: `data:image/png;base64,${PNG}`, column: 2, row: 8, width: 30, height: 30 });
    const zip = await save(o, 'added-picture.xlsx');
    await expectConsistent(zip);
    expect(Object.keys(zip.files).some(p => /^xl\/media\/imageNew/.test(p))).toBe(true);
    const path = Object.keys(zip.files).find(p => /^xl\/drawings\/[^/]*\.xml$/.test(p))!;
    expect((await partText(zip, path)).match(/<xdr:pic>/g)).toHaveLength(2);
  });

  it('has no notice saying charts are view only or pictures stay put', async () => {
    const { report } = await xlsxToWorkbook(await workbookBytes(), { id: 'wb', name: 'b.xlsx' });
    const text = report.warnings.map(w => w.feature).join(' | ');
    expect(text).not.toContain('edit them in Excel');
    expect(text).not.toContain('saved as they were');
    expect(text).toContain('simpler style');
    const combo = await xlsxToWorkbook(await workbookBytes(CHART_XML().replace(/barChart/g, 'bar3DChart')), { id: 'wb', name: 'b.xlsx' });
    expect(combo.report.warnings.map(w => w.feature)).toContain('3D and combination charts are kept but not editable here');
  });

  it('writes a complete chart part for every kind', () => {
    const grid = new FakeSheetGrid({ S: [['Region', 'Qty'], ['North', 10], ['South', 25]] });
    const spec = specFromSelection('c', grid, 'S', { startRow: 0, startColumn: 0, endRow: 2, endColumn: 1 }, 'Chart')!;
    for (const kind of ['column', 'bar', 'line', 'area', 'pie', 'doughnut', 'scatter', 'radar'] as const) {
      const titled = { ...spec, kind, title: 'T & co', legend: 'r' as const };
      const doc = parseXml(newChartXml(titled, liveSeries(titled, grid)));
      expect(doc.getElementsByTagNameNS(C, 'ser'), kind).toHaveLength(1);
      expect(doc.getElementsByTagNameNS(C, 'legendPos')[0].getAttribute('val')).toBe('r');
      expect(doc.documentElement.textContent).toContain('T & co');
    }
  });
});

describe('SheetChartsPanelComponent', () => {
  async function render(o: Opened) {
    await TestBed.configureTestingModule({ imports: [SheetChartsPanelComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    const fixture = TestBed.createComponent(SheetChartsPanelComponent);
    fixture.componentRef.setInput('session', o.session);
    fixture.componentRef.setInput('grid', o.grid);
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture;
  }

  const change = async (fixture: { detectChanges(): void; whenStable(): Promise<unknown> }, el: HTMLInputElement | HTMLSelectElement, value: string) => {
    el.value = value;
    el.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  };

  it('lists charts and edits the title, type and legend of the one picked', async () => {
    const o = await open(await workbookBytes());
    const fixture = await render(o);
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('.charts-panel__item-name')?.textContent).toBe('Quantity');
    (root.querySelector('.charts-panel__item') as HTMLButtonElement).click();
    fixture.detectChanges();
    await fixture.whenStable();
    const fields = root.querySelectorAll<HTMLInputElement | HTMLSelectElement>('.charts-panel__field input[type=text], .charts-panel__field select');
    await change(fixture, fields[0], 'Renamed');
    expect(firstSpec(o).title).toBe('Renamed');
    await change(fixture, root.querySelectorAll('select')[0] as HTMLSelectElement, 'line');
    expect(firstSpec(o).kind).toBe('line');
    await change(fixture, root.querySelectorAll('select')[2] as HTMLSelectElement, 'none');
    expect(firstSpec(o).legend).toBe('none');
    expect(o.edited).toHaveBeenCalled();
  });

  it('checks typed references, and takes the selection for one', async () => {
    const o = await open(await workbookBytes());
    const fixture = await render(o);
    const c = fixture.componentInstance;
    c.select(firstSpec(o).id);
    c.setSeriesRef(0, 'values', 'Nowhere!A1:A3');
    expect(c.message()).toContain('not a range');
    expect(firstSpec(o).series[0].values?.formula).toBe("'My Data'!$B$2:$B$4");
    c.setSeriesRef(0, 'values', 'C2:C4');
    expect(c.message()).toBeNull();
    expect(firstSpec(o).series[0].values?.formula).toBe("'My Data'!$C$2:$C$4");
    o.grid.chosen = { sheet: 'Other', range: { startRow: 1, startColumn: 1, endRow: 2, endColumn: 1 } };
    c.useSelection(0, 'values');
    expect(firstSpec(o).series[0].values?.formula).toBe('Other!$B$2:$B$3');
    c.setSeriesRef(0, 'values', '');
    expect(c.message()).toContain('needs its values');
  });

  it('adds from the selection, says when it cannot, and deletes', async () => {
    const o = await open(await workbookBytes());
    const fixture = await render(o);
    const c = fixture.componentInstance;
    await c.addChart();
    expect(c.message()).toContain('Select the cells');
    o.grid.chosen = { sheet: 'Other', range: { startRow: 0, startColumn: 0, endRow: 2, endColumn: 1 } };
    await c.addChart();
    expect(c.charts()).toHaveLength(2);
    expect(c.selected()?.sheet).toBe('Other');
    c.addSeriesFromSelection();
    expect(c.selected()?.series).toHaveLength(2);
    c.removeSeries(1);
    expect(c.selected()?.series).toHaveLength(1);
    c.deleteSelected();
    expect(c.charts()).toHaveLength(1);
  });

  it('shows a chart it cannot edit as kept, with no fields', async () => {
    const o = await open(await workbookBytes(CHART_XML().replace(/barChart/g, 'bar3DChart')));
    const fixture = await render(o);
    const root = fixture.nativeElement as HTMLElement;
    (root.querySelector('.charts-panel__item') as HTMLButtonElement).click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(root.querySelector('.charts-panel__note')?.textContent).toContain('A 3D chart is kept');
    expect(root.querySelector('form')).toBeNull();
  });
});

/**
 * A developer harness, not a test of the suite: when OFFICE_SAMPLES_DIR names a folder with
 * sample.xlsx, sample.docx and sample.pptx, it runs the converters over them and writes what it
 * finds next to them, so fidelity work can be checked against real documents. Skipped otherwise.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { parse as parsePptx } from 'pptxtojson';
import { describe, expect, it } from 'vitest';

import { docxToDocument } from './docx/docx-import';
import { pptxToDeck } from './pptx/pptx-import';
import { OoxmlPackage } from './shared/ooxml';
import { xlsxToWorkbook } from './xlsx/xlsx-import';

const dir = process.env['OFFICE_SAMPLES_DIR'];
const have = (name: string) => !!dir && existsSync(join(dir, name));
// Through a Blob so the buffer belongs to the test realm JSZip checks against.
const bytes = (name: string) => new Blob([readFileSync(join(dir!, name))]).arrayBuffer();
const dump = (name: string, value: unknown) => writeFileSync(join(dir!, name), JSON.stringify(value, null, 1));

describe.skipIf(!dir)('real office files', () => {
  it.skipIf(!have('sample.xlsx'))('xlsx', async () => {
    const pkg = await OoxmlPackage.open(await bytes('sample.xlsx'));
    const parts = pkg.paths();
    const { workbook, report } = await xlsxToWorkbook(await bytes('sample.xlsx'), { id: 'wb', name: 'sample.xlsx' });
    const sheet = workbook.sheets[workbook.sheetOrder[0]];
    const styles = workbook.styles;
    const row = (r: number) =>
      Object.fromEntries(Object.entries(sheet.cellData[r] ?? {}).map(([c, cell]) => [c, { v: cell.v, s: cell.s ? styles[cell.s] : null }]));
    dump('xlsx-report.json', {
      report,
      parts: parts.filter(p => /chart|pivot|drawing|theme/.test(p)),
      themeXml: (await pkg.text('xl/theme/theme1.xml'))?.slice(0, 2000),
      styleCount: Object.keys(styles).length,
      row1: row(0),
      row2: row(1),
      row3: row(2),
      columnData: sheet.columnData,
      resources: workbook.resources.map(r => ({ name: r.name, size: r.data.length })),
    });
    expect(workbook.sheetOrder.length).toBeGreaterThan(0);
  });

  it.skipIf(!have('sample.docx'))('docx', async () => {
    const pkg = await OoxmlPackage.open(await bytes('sample.docx'));
    const docXml = (await pkg.text('word/document.xml')) ?? '';
    const { document, report } = await docxToDocument(await bytes('sample.docx'), { id: 'doc', name: 'sample.docx' });
    const tables = Object.values(document.tableSource ?? {}).map(t => ({
      columns: t.tableColumns.map(c => c.size.width.v),
      rows: t.tableRows.map(r => r.tableCells.map(c => ({ span: c.columnSpan, rowSpan: c.rowSpan, w: c.size?.width.v }))),
    }));
    dump('docx-report.json', {
      report,
      parts: pkg.paths(),
      tags: Object.fromEntries(
        [
          'mc:AlternateContent',
          'w:drawing',
          'wp:anchor',
          'wp:inline',
          'w:pict',
          'v:shape',
          'w:sdt',
          'w:tbl',
          'w:gridSpan',
          'w:tcW',
          'w:tblGrid',
          'w:headerReference',
          'w:background',
        ].map(t => [t, (docXml.match(new RegExp(`<${t}[ >/]`, 'g')) ?? []).length]),
      ),
      headerXmlSnippet: (await pkg.text('word/header1.xml'))?.slice(0, 3000),
      tables,
      drawings: Object.values(document.drawings ?? {}).map(d => ({
        id: d.drawingId,
        layout: d.layoutType,
        size: d.docTransform.size,
        src: d.source.slice(0, 40),
      })),
      headers: Object.values(document.headers ?? {}).map(h => ({
        id: h.headerId,
        stream: h.body.dataStream.slice(0, 200),
        drawings: Object.keys(h.drawings ?? {}),
      })),
      streamHead: document.body.dataStream.slice(0, 400),
      styleIds: Object.keys(document.styles ?? {}).slice(0, 40),
    });
    expect(document.body.dataStream.length).toBeGreaterThan(0);
  });

  it.skipIf(!have('sample.pptx'))('pptx', async () => {
    const json = await parsePptx(await bytes('sample.pptx'), { imageMode: 'base64' });
    const { deck, report } = await pptxToDeck(await bytes('sample.pptx'), { id: 'deck', name: 'sample.pptx' }, parsePptx);
    const strip = (o: unknown): unknown =>
      JSON.parse(JSON.stringify(o, (k, v) => (typeof v === 'string' && v.length > 300 ? `${v.slice(0, 300)}…(${v.length})` : v)));
    dump('pptx-report.json', {
      report,
      size: json.size,
      themeColors: json.themeColors,
      slide1Raw: strip(json.slides[0]),
      slide1Deck: strip(deck.slides[0]),
    });
    expect(deck.slides.length).toBeGreaterThan(0);
  });
});

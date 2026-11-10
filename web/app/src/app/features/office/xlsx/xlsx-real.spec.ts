/**
 * A developer harness, not a test of the suite: when OFFICE_SAMPLES_DIR names a folder with a
 * sample.xlsx, it round-trips the file through the converters, writes `xlsx-roundtrip.xlsx`
 * beside it and a JSON report of what the package held before and after, so preservation work
 * can be checked against a real workbook (open the result in Excel). Skipped otherwise.
 */
import ExcelJS from 'exceljs';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage } from '../shared/ooxml';
import { RESOURCE_DEFINED_NAMES, UniverDefinedName } from './xlsx-defined-names';
import { readResource } from './univer-sheet-types';
import { workbookToXlsx } from './xlsx-export';
import { xlsxToWorkbook } from './xlsx-import';

const dir = process.env['OFFICE_SAMPLES_DIR'];
const have = (name: string) => !!dir && existsSync(join(dir, name));
// Through a Blob so the buffer belongs to the test realm JSZip checks against.
const bytes = (name: string) => new Blob([readFileSync(join(dir!, name))]).arrayBuffer();

describe.skipIf(!dir || !have('sample.xlsx'))('real xlsx round trip', () => {
  it('keeps the parts of sample.xlsx through an edit', async () => {
    const original = await bytes('sample.xlsx');
    const before = await OoxmlPackage.open(original);
    const first = await xlsxToWorkbook(original, { id: 'wb', name: 'sample.xlsx' });
    const sheet = first.workbook.sheets[first.workbook.sheetOrder[0]];
    // The edit: a value in the first empty row.
    const lastRow = Math.max(...Object.keys(sheet.cellData).map(Number));
    sheet.cellData[lastRow + 1] = { 0: { v: 'Edited here', t: 1 } };
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    writeFileSync(join(dir!, 'xlsx-roundtrip.xlsx'), saved.bytes);
    const after = await OoxmlPackage.open(saved.bytes);
    const excel = new ExcelJS.Workbook();
    await excel.xlsx.load(saved.bytes as never);
    const second = await xlsxToWorkbook(saved.bytes, { id: 'wb', name: 'xlsx-roundtrip.xlsx' });
    const names = (wb: typeof first.workbook) =>
      Object.values(readResource<Record<string, UniverDefinedName>>(wb, RESOURCE_DEFINED_NAMES) ?? {}).map(
        n => `${n.name}=${n.formulaOrRefString}`,
      );
    const report = {
      importReport: first.report,
      exportReport: saved.report,
      partsBefore: before.paths().sort(),
      partsAfter: after.paths().sort(),
      preserved: first.preserved && {
        bySheet: first.preserved.bySheet,
        pivotCaches: first.preserved.pivotCaches,
        externalLinks: first.preserved.externalLinks,
        vbaProject: first.preserved.vbaProject,
        customXml: first.preserved.customXml,
        customProperties: first.preserved.customProperties,
        extras: first.preserved.extras,
      },
      definedNamesBefore: names(first.workbook),
      definedNamesAfter: names(second.workbook),
      workbookXmlAfter: await after.text('xl/workbook.xml'),
      contentTypesAfter: await after.text('[Content_Types].xml'),
      rootRelsAfter: await after.text('_rels/.rels'),
      sheet1RelsAfter: await after.text('xl/worksheets/_rels/sheet1.xml.rels'),
      sheet1TailAfter: (await after.text('xl/worksheets/sheet1.xml'))?.slice(-1500),
      excelSheets: excel.worksheets.map(ws => ({ name: ws.name, rows: ws.rowCount, pageSetup: ws.pageSetup, views: ws.views })),
      editedCell: second.workbook.sheets[second.workbook.sheetOrder[0]].cellData[lastRow + 1]?.[0],
    };
    writeFileSync(join(dir!, 'xlsx-roundtrip-report.json'), JSON.stringify(report, null, 1));
    expect(report.editedCell).toMatchObject({ v: 'Edited here' });
    expect(saved.report.dropped).toEqual([]);
  });
});

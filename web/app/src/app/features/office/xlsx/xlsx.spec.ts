import ExcelJS from 'exceljs';
import JSZip from 'jszip';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage } from '../shared/ooxml';

import {
  RESOURCE_CONDITIONAL_FORMATTING,
  RESOURCE_DATA_VALIDATION,
  RESOURCE_DRAWINGS,
  RESOURCE_FILTER,
  RESOURCE_NOTES,
  UniverCfRule,
  UniverDataValidationRule,
  UniverDrawingsForSheet,
  UniverFilter,
  UniverNote,
  UniverWorkbook,
  readResource,
  writeResource,
} from './univer-sheet-types';
import { DEFINED_NAME_WORKBOOK_SCOPE, RESOURCE_DEFINED_NAMES, UniverDefinedName } from './xlsx-defined-names';
import { sheetToCsv, workbookToExcel, workbookToXlsx } from './xlsx-export';
import { dateToSerial, delimitedTextToWorkbook, excelToWorkbook, parseDelimited, serialToDate, xlsxToWorkbook } from './xlsx-import';
import { HALIGN, VALIGN, WRAP } from './xlsx-styles';

// A 1x1 red PNG.
const PNG_BASE64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==';

function sampleWorkbook(): ExcelJS.Workbook {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('Sales', {
    properties: { tabColor: { argb: 'FFFF0000' } },
    views: [{ state: 'frozen', xSplit: 1, ySplit: 1 }],
  });
  ws.columns = [{ width: 20 }, { width: 12 }, { width: 12, hidden: true }, { width: 15 }];
  const header = ws.getRow(1);
  header.values = ['Region', 'Q1', 'Q2', 'Total'];
  header.font = { bold: true, color: { argb: 'FFFFFFFF' } };
  header.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: 'FF1F4E79' } };
  header.alignment = { horizontal: 'center', vertical: 'middle', wrapText: true };
  header.height = 30;
  ws.getRow(2).values = ['North', 100, 200];
  ws.getCell('D2').value = { formula: 'B2+C2', result: 300 };
  ws.getRow(3).values = ['South', 50.5, 75];
  ws.getCell('D3').value = { formula: 'B3+C3', result: 125.5 };
  ws.getCell('B3').numFmt = '#,##0.00';
  ws.getCell('A4').value = new Date(Date.UTC(2024, 0, 15));
  ws.getCell('A4').numFmt = 'yyyy-mm-dd';
  ws.getCell('B4').value = true;
  ws.getCell('C4').value = { error: '#DIV/0!' };
  ws.getCell('A5').value = { richText: [{ text: 'Bold ', font: { bold: true } }, { text: 'plain' }] };
  ws.getCell('A6').value = { text: 'WhatIff', hyperlink: 'https://whatiff.ai', tooltip: 'Home' };
  ws.getCell('B6').border = { top: { style: 'thin' }, bottom: { style: 'double', color: { argb: 'FF00FF00' } } };
  ws.getCell('A7').value = 'Noted';
  ws.getCell('A7').note = 'A note on A7';
  ws.getRow(8).hidden = true;
  ws.mergeCells('A9:B10');
  ws.getCell('A9').value = 'Merged';
  const validations = (ws as unknown as { dataValidations: { add(ref: string, dv: ExcelJS.DataValidation): void } }).dataValidations;
  validations.add('B2:B3', {
    type: 'whole',
    operator: 'between',
    formulae: [0, 1000],
    allowBlank: true,
    showErrorMessage: true,
    error: 'Out of range',
  });
  validations.add('A2:A3', { type: 'list', formulae: ['"North,South,East"'] });
  ws.addConditionalFormatting({
    ref: 'B2:C3',
    rules: [
      {
        type: 'cellIs',
        operator: 'greaterThan',
        formulae: [100],
        priority: 1,
        style: { fill: { type: 'pattern', pattern: 'solid', fgColor: { argb: 'FFFFC7CE' } } },
      },
    ],
  });
  ws.addConditionalFormatting({
    ref: 'D2:D3',
    rules: [
      { type: 'colorScale', priority: 2, cfvo: [{ type: 'min' }, { type: 'max' }], color: [{ argb: 'FFF8696B' }, { argb: 'FF63BE7B' }] },
    ],
  });
  ws.addConditionalFormatting({
    ref: 'B2:B3',
    rules: [
      {
        type: 'dataBar',
        priority: 3,
        cfvo: [{ type: 'min' }, { type: 'max' }],
        color: { argb: 'FF638EC6' },
      } as ExcelJS.ConditionalFormattingRule,
    ],
  });
  ws.addConditionalFormatting({
    ref: 'A2:A3',
    rules: [{ type: 'expression', priority: 4, formulae: ['$B2>60'], style: { font: { bold: true } } }],
  });
  ws.autoFilter = 'A1:D3';
  const img = wb.addImage({ base64: PNG_BASE64, extension: 'png' });
  ws.addImage(img, { tl: { col: 5, row: 1 }, br: { col: 7, row: 4 } } as ExcelJS.Anchor & { tl: ExcelJS.Anchor; br: ExcelJS.Anchor });
  const hidden = wb.addWorksheet('Hidden', { state: 'hidden' });
  hidden.getCell('A1').value = 'secret';
  return wb;
}

function cell(wb: UniverWorkbook, sheet: number, row: number, col: number) {
  const id = wb.sheetOrder[sheet];
  return wb.sheets[id].cellData[row]?.[col];
}

function style(wb: UniverWorkbook, sheet: number, row: number, col: number) {
  const c = cell(wb, sheet, row, col);
  return c?.s ? wb.styles[c.s] : undefined;
}

describe('xlsx import', () => {
  it('carries the sheets, values, formulas and types', () => {
    const { workbook, report } = excelToWorkbook(sampleWorkbook(), { id: 'wb', name: 'sample.xlsx' });
    expect(report.dropped).toEqual([]);
    expect(workbook.sheetOrder).toHaveLength(2);
    const sales = workbook.sheets[workbook.sheetOrder[0]];
    expect(sales.name).toBe('Sales');
    expect(sales.tabColor).toBe('#FF0000');
    expect(sales.hidden).toBe(0);
    expect(workbook.sheets[workbook.sheetOrder[1]].hidden).toBe(1);
    expect(cell(workbook, 0, 0, 0)).toMatchObject({ v: 'Region', t: 1 });
    expect(cell(workbook, 0, 1, 1)).toMatchObject({ v: 100, t: 2 });
    expect(cell(workbook, 0, 1, 3)).toMatchObject({ f: '=B2+C2', v: 300, t: 2 });
    expect(cell(workbook, 0, 2, 1)).toMatchObject({ v: 50.5, t: 2 });
    expect(style(workbook, 0, 2, 1)?.n?.pattern).toBe('#,##0.00');
    expect(cell(workbook, 0, 3, 0)).toMatchObject({ v: dateToSerial(new Date(Date.UTC(2024, 0, 15))), t: 2 });
    expect(style(workbook, 0, 3, 0)?.n?.pattern).toBe('yyyy-mm-dd');
    expect(cell(workbook, 0, 3, 1)).toMatchObject({ v: 1, t: 3 });
    expect(cell(workbook, 0, 3, 2)).toMatchObject({ v: '#DIV/0!', t: 1 });
  });

  it('carries styles, layout, merges, freeze and hidden rows and columns', () => {
    const { workbook } = excelToWorkbook(sampleWorkbook(), { id: 'wb', name: 'sample.xlsx' });
    const sales = workbook.sheets[workbook.sheetOrder[0]];
    const header = style(workbook, 0, 0, 0);
    expect(header).toMatchObject({
      bl: 1,
      cl: { rgb: '#FFFFFF' },
      bg: { rgb: '#1F4E79' },
      ht: HALIGN.center,
      vt: VALIGN.middle,
      tb: WRAP.wrap,
    });
    expect(style(workbook, 0, 0, 1)).toBe(header);
    expect(style(workbook, 0, 5, 1)?.bd).toMatchObject({ t: { s: 1 }, b: { s: 7, cl: { rgb: '#00FF00' } } });
    expect(sales.freeze).toEqual({ xSplit: 1, ySplit: 1, startRow: 1, startColumn: 1 });
    expect(sales.columnData[0]).toEqual({ w: 145 });
    expect(sales.columnData[2]).toEqual({ w: 89, hd: 1 });
    // The header row has its own font and fill, which Excel keeps as a row style.
    expect(sales.rowData[0]).toEqual({ h: 40, s: sales.cellData[0][0].s });
    expect(sales.rowData[7]).toEqual({ hd: 1 });
    expect(sales.mergeData).toEqual([{ startRow: 8, startColumn: 0, endRow: 9, endColumn: 1 }]);
  });

  it('carries rich text and hyperlinks as cell documents', () => {
    const { workbook } = excelToWorkbook(sampleWorkbook(), { id: 'wb', name: 'sample.xlsx' });
    const rich = cell(workbook, 0, 4, 0);
    expect(rich?.v).toBe('Bold plain');
    expect(rich?.p?.body.dataStream).toBe('Bold plain\r\n');
    expect(rich?.p?.body.textRuns).toEqual([{ st: 0, ed: 5, ts: { bl: 1 } }]);
    const link = cell(workbook, 0, 5, 0);
    expect(link?.v).toBe('WhatIff');
    expect(link?.p?.body.customRanges?.[0]).toMatchObject({
      startIndex: 0,
      endIndex: 6,
      rangeType: 0,
      properties: { url: 'https://whatiff.ai', tooltip: 'Home' },
    });
  });

  it('carries validation, conditional formatting, notes, images and the filter as plugin resources', async () => {
    const { workbook } = await xlsxToWorkbook(await sampleWorkbook().xlsx.writeBuffer() as ArrayBuffer, { id: 'wb', name: 'sample.xlsx' });
    const sheetId = workbook.sheetOrder[0];
    const dv = readResource<Record<string, UniverDataValidationRule[]>>(workbook, RESOURCE_DATA_VALIDATION)!;
    expect(dv[sheetId]).toHaveLength(2);
    expect(dv[sheetId].find(r => r.type === 'whole')).toMatchObject({
      operator: 'between',
      formula1: '0',
      formula2: '1000',
      allowBlank: true,
      error: 'Out of range',
      ranges: [{ startRow: 1, startColumn: 1, endRow: 2, endColumn: 1 }],
    });
    expect(dv[sheetId].find(r => r.type === 'list')).toMatchObject({ formula1: 'North,South,East' });
    const cf = readResource<Record<string, UniverCfRule[]>>(workbook, RESOURCE_CONDITIONAL_FORMATTING)!;
    const kinds = cf[sheetId].map(r => (r.rule.type === 'highlightCell' ? r.rule.subType : r.rule.type)).sort();
    expect(kinds).toEqual(['colorScale', 'dataBar', 'formula', 'number']);
    const number = cf[sheetId].find(r => r.rule.type === 'highlightCell' && r.rule.subType === 'number')!;
    expect(number.rule).toMatchObject({ operator: 'greaterThan', value: 100, style: { bg: { rgb: '#FFC7CE' } } });
    expect(cf[sheetId].find(r => r.rule.type === 'highlightCell' && r.rule.subType === 'formula')!.rule).toMatchObject({
      value: '=$B2>60',
    });
    const notes = readResource<Record<string, Record<number, Record<number, UniverNote>>>>(workbook, RESOURCE_NOTES)!;
    expect(notes[sheetId][6][0]).toMatchObject({ row: 6, col: 0, note: 'A note on A7' });
    const drawings = readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS)!;
    expect(drawings[sheetId].order).toHaveLength(1);
    const image = drawings[sheetId].data[drawings[sheetId].order[0]];
    expect(image.source).toBe(`data:image/png;base64,${PNG_BASE64}`);
    expect(image.sheetTransform.from).toEqual({ column: 5, columnOffset: 0, row: 1, rowOffset: 0 });
    expect(image.sheetTransform.to).toEqual({ column: 7, columnOffset: 0, row: 4, rowOffset: 0 });
    const filter = readResource<Record<string, UniverFilter>>(workbook, RESOURCE_FILTER)!;
    expect(filter[sheetId].ref).toEqual({ startRow: 0, startColumn: 0, endRow: 2, endColumn: 3 });
  });

  it('round-trips through bytes', async () => {
    const bytes = await sampleWorkbook().xlsx.writeBuffer();
    const first = await xlsxToWorkbook(bytes as ArrayBuffer, { id: 'wb', name: 'sample.xlsx' });
    const exported = await workbookToXlsx(first.workbook);
    expect(exported.report.dropped).toEqual([]);
    const second = await xlsxToWorkbook(exported.bytes, { id: 'wb', name: 'sample.xlsx' });
    const a = second.workbook;
    const b = first.workbook;
    expect(a.sheetOrder.map(id => a.sheets[id].name)).toEqual(['Sales', 'Hidden']);
    for (const [sheetIndex] of a.sheetOrder.entries()) {
      const sa = a.sheets[a.sheetOrder[sheetIndex]];
      const sb = b.sheets[b.sheetOrder[sheetIndex]];
      expect(sa.hidden).toBe(sb.hidden);
      expect(sa.tabColor).toBe(sb.tabColor);
      expect(sa.freeze).toEqual(sb.freeze);
      expect(sa.mergeData).toEqual(sb.mergeData);
      expect(sa.columnData).toEqual(sb.columnData);
      expect(sa.rowData).toEqual(sb.rowData);
      for (const [r, row] of Object.entries(sb.cellData)) {
        for (const [c, expected] of Object.entries(row)) {
          const actual = sa.cellData[Number(r)]?.[Number(c)];
          expect(actual?.v, `value at ${r},${c}`).toEqual(expected.v);
          expect(actual?.t, `type at ${r},${c}`).toEqual(expected.t);
          expect(actual?.f, `formula at ${r},${c}`).toEqual(expected.f);
          expect(actual?.p?.body.textRuns, `runs at ${r},${c}`).toEqual(expected.p?.body.textRuns);
          expect(actual?.p?.body.customRanges?.[0]?.properties, `link at ${r},${c}`).toEqual(
            expected.p?.body.customRanges?.[0]?.properties,
          );
          expect(actual?.s ? a.styles[actual.s] : undefined, `style at ${r},${c}`).toEqual(expected.s ? b.styles[expected.s] : undefined);
        }
      }
    }
    const sheetId = a.sheetOrder[0];
    const dv = readResource<Record<string, UniverDataValidationRule[]>>(a, RESOURCE_DATA_VALIDATION)!;
    expect(dv[sheetId].map(r => ({ ...r, uid: '' })).sort((x, y) => x.type.localeCompare(y.type))).toEqual(
      readResource<Record<string, UniverDataValidationRule[]>>(b, RESOURCE_DATA_VALIDATION)!
        [sheetId].map(r => ({ ...r, uid: '' }))
        .sort((x, y) => x.type.localeCompare(y.type)),
    );
    const cf = readResource<Record<string, UniverCfRule[]>>(a, RESOURCE_CONDITIONAL_FORMATTING)!;
    expect(cf[sheetId].map(r => r.rule)).toEqual(
      readResource<Record<string, UniverCfRule[]>>(b, RESOURCE_CONDITIONAL_FORMATTING)![sheetId].map(r => r.rule),
    );
    const notes = readResource<Record<string, Record<number, Record<number, UniverNote>>>>(a, RESOURCE_NOTES)!;
    expect(notes[sheetId][6][0].note).toBe('A note on A7');
    const drawings = readResource<Record<string, UniverDrawingsForSheet>>(a, RESOURCE_DRAWINGS)!;
    expect(drawings[sheetId].order).toHaveLength(1);
    expect(Object.values(drawings[sheetId].data)[0].sheetTransform).toEqual(
      Object.values(readResource<Record<string, UniverDrawingsForSheet>>(b, RESOURCE_DRAWINGS)![sheetId].data)[0].sheetTransform,
    );
    expect(readResource<Record<string, UniverFilter>>(a, RESOURCE_FILTER)![sheetId].ref).toEqual({
      startRow: 0,
      startColumn: 0,
      endRow: 2,
      endColumn: 3,
    });
  });

  it('carries defined names into the workbook resource and back, with the cached value of a formula over one', async () => {
    const wb = sampleWorkbook();
    wb.definedNames.add('Sales!$B$2:$B$3', 'Q1Sales');
    wb.getWorksheet('Sales')!.getCell('F1').value = { formula: 'SUM(Q1Sales)', result: 150.5 };
    const buffer = await wb.xlsx.writeBuffer();
    const zip = await JSZip.loadAsync(buffer);
    // A constant and a sheet-scoped formula name, which ExcelJS cannot carry.
    zip.file(
      'xl/workbook.xml',
      (await zip.file('xl/workbook.xml')!.async('string')).replace(
        '</definedNames>',
        '<definedName name="Rate" comment="Tax">0.2</definedName><definedName name="Last" localSheetId="0" hidden="1">INDEX(Sales!$A:$A,COUNTA(Sales!$A:$A))</definedName></definedNames>',
      ),
    );
    const first = await xlsxToWorkbook(await zip.generateAsync({ type: 'arraybuffer' }), { id: 'wb', name: 'names.xlsx' });
    expect(first.report.dropped).toEqual([]);
    const names = readResource<Record<string, UniverDefinedName>>(first.workbook, RESOURCE_DEFINED_NAMES)!;
    const byName = Object.fromEntries(Object.values(names).map(n => [n.name, n]));
    expect(byName['Q1Sales']).toMatchObject({ formulaOrRefString: 'Sales!$B$2:$B$3', localSheetId: DEFINED_NAME_WORKBOOK_SCOPE });
    expect(byName['Rate']).toMatchObject({ formulaOrRefString: '=0.2', comment: 'Tax' });
    expect(byName['Last']).toMatchObject({
      formulaOrRefString: '=INDEX(Sales!$A:$A,COUNTA(Sales!$A:$A))',
      hidden: true,
      localSheetId: 'sheet-1',
    });
    expect(first.workbook.sheets['sheet-1'].cellData[0][5]).toMatchObject({ f: '=SUM(Q1Sales)', v: 150.5 });
    // Edit one name as Univer would, then save: all three go back, with the edit.
    byName['Rate'].formulaOrRefString = '=0.25';
    writeResource(first.workbook, RESOURCE_DEFINED_NAMES, names);
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const workbookXml = (await (await OoxmlPackage.open(saved.bytes)).text('xl/workbook.xml'))!;
    expect(workbookXml).toContain('<definedName name="Q1Sales">Sales!$B$2:$B$3</definedName>');
    expect(workbookXml).toContain('<definedName name="Rate" comment="Tax">0.25</definedName>');
    expect(workbookXml).toContain(
      '<definedName name="Last" localSheetId="0" hidden="1">INDEX(Sales!$A:$A,COUNTA(Sales!$A:$A))</definedName>',
    );
    expect(workbookXml).toContain('fullCalcOnLoad="1"');
    const second = await xlsxToWorkbook(saved.bytes, { id: 'wb', name: 'names.xlsx' });
    expect(second.workbook.sheets['sheet-1'].cellData[0][5]).toMatchObject({ f: '=SUM(Q1Sales)', v: 150.5 });
    expect(
      Object.values(readResource<Record<string, UniverDefinedName>>(second.workbook, RESOURCE_DEFINED_NAMES)!)
        .map(n => n.name)
        .sort(),
    ).toEqual(['Last', 'Q1Sales', 'Rate']);
  });

  it('translates shared formulas for their dependents and keeps array formulas as array formulas', async () => {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('F');
    ws.getCell('A1').value = 1;
    ws.getCell('A2').value = 2;
    ws.getCell('B1').value = { formula: 'A1*2', result: 2, shareType: 'shared', ref: 'B1:B2' } as ExcelJS.CellFormulaValue;
    ws.getCell('B2').value = { sharedFormula: 'B1', result: 4 } as ExcelJS.CellSharedFormulaValue;
    ws.getCell('C1').value = { formula: 'A1:A2*10', result: 10, shareType: 'array', ref: 'C1:C2' } as ExcelJS.CellFormulaValue;
    ws.getCell('C2').value = 20;
    const first = await xlsxToWorkbook(await wb.xlsx.writeBuffer(), { id: 'wb', name: 'f.xlsx' });
    const cells = first.workbook.sheets['sheet-1'].cellData;
    expect(cells[0][1]).toMatchObject({ f: '=A1*2', v: 2 });
    expect(cells[1][1]).toMatchObject({ f: '=A2*2', v: 4 });
    expect(cells[0][2]).toMatchObject({ f: '=A1:A2*10', v: 10, ref: 'C1:C2', ft: 2 });
    expect(first.report.warnings.map(w => w.feature)).not.toContain('Shared formulas kept as values');
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const sheetXml = (await (await OoxmlPackage.open(saved.bytes)).text('xl/worksheets/sheet1.xml'))!;
    expect(sheetXml).toContain('<f t="array" ref="C1:C2">A1:A2*10</f>');
    expect(sheetXml).toMatch(/<c r="B2"[^>]*><f>A2\*2<\/f>/);
  });

  it('carries column and row default styles', async () => {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('L');
    ws.getColumn(2).style = { numFmt: '0.00%' };
    ws.addRows([
      [1, 0.5],
      [2, 0.25],
      [3, 0.125],
    ]);
    ws.getRow(3).font = { italic: true };
    const first = await xlsxToWorkbook(await wb.xlsx.writeBuffer(), { id: 'wb', name: 'l.xlsx' });
    const sheet = first.workbook.sheets['sheet-1'];
    expect(first.workbook.styles[sheet.columnData[1].s!]).toMatchObject({ n: { pattern: '0.00%' } });
    expect(first.workbook.styles[sheet.rowData[2].s!]).toMatchObject({ it: 1 });
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const second = await xlsxToWorkbook(saved.bytes, { id: 'wb', name: 'l.xlsx' });
    const again = second.workbook.sheets['sheet-1'];
    expect(second.workbook.styles[again.columnData[1].s!]).toMatchObject({ n: { pattern: '0.00%' } });
    expect(second.workbook.styles[again.rowData[2].s!]).toMatchObject({ it: 1 });
  });
});

describe('xlsx export', () => {
  it('writes dates for serial values with a date format and errors for error strings', () => {
    const { workbook } = excelToWorkbook(sampleWorkbook(), { id: 'wb', name: 'sample.xlsx' });
    const excel = workbookToExcel(workbook);
    const ws = excel.getWorksheet('Sales')!;
    expect(ws.getCell('A4').value).toEqual(new Date(Date.UTC(2024, 0, 15)));
    expect(ws.getCell('C4').value).toEqual({ error: '#DIV/0!' });
    expect(ws.getCell('B4').value).toBe(true);
    expect(ws.getCell('D2').value).toEqual({ formula: 'B2+C2', result: 300 });
    expect(ws.getCell('A6').value).toMatchObject({ text: 'WhatIff', hyperlink: 'https://whatiff.ai' });
  });

  it('serialises a sheet as CSV with quoting', () => {
    const { workbook } = delimitedTextToWorkbook('a,b\r\n1,"x, ""y"""\r\n', ',', { id: 'wb', name: 'data.csv' });
    const sheet = workbook.sheets[workbook.sheetOrder[0]];
    expect(sheet.name).toBe('data');
    expect(sheet.cellData[1][0]).toEqual({ v: 1, t: 2 });
    expect(sheet.cellData[1][1]).toEqual({ v: 'x, "y"', t: 1 });
    expect(sheetToCsv(sheet)).toBe('a,b\r\n1,"x, ""y"""\r\n');
  });
});

describe('date serials', () => {
  it('converts both ways', () => {
    expect(dateToSerial(new Date(Date.UTC(1900, 0, 1)))).toBe(2);
    expect(dateToSerial(new Date(Date.UTC(2024, 0, 15)))).toBe(45306);
    expect(serialToDate(45306).toISOString()).toBe('2024-01-15T00:00:00.000Z');
  });
});

describe('delimitedTextToWorkbook', () => {
  it('converts only numbers that read back exactly, so saving does not rewrite data', () => {
    const { workbook } = delimitedTextToWorkbook('00123,1.50,12345678901234567890,42,-1.5,1e3,Infinity\n', ',', { id: 'wb', name: 'a.csv' });
    const cells = workbook.sheets['sheet-1'].cellData[0];
    expect(Object.values(cells).map(c => [c.v, c.t])).toEqual([
      ['00123', 1], ['1.50', 1], ['12345678901234567890', 1], [42, 2], [-1.5, 2], ['1e3', 1], ['Infinity', 1],
    ]);
  });
});

describe('parseDelimited', () => {
  it('treats a quote inside an unquoted field as a character, so rows do not merge', () => {
    expect(parseDelimited('1,5" screen\n2,7" tab\n', ',')).toEqual([['1', '5" screen'], ['2', '7" tab']]);
  });

  it('handles LF rows, quotes and tabs', () => {
    expect(parseDelimited('a\tb\n"c\td"\te\n', '\t')).toEqual([
      ['a', 'b'],
      ['c\td', 'e'],
    ]);
    expect(parseDelimited('', ',')).toEqual([]);
  });
});

describe('styleKey', () => {
  it('distinguishes nested differences and ignores key order', async () => {
    const { styleKey } = await import('./xlsx-styles');
    expect(styleKey({ n: { pattern: 'a' } })).not.toBe(styleKey({ n: { pattern: 'b' } }));
    expect(styleKey({ bl: 1, fs: 12 })).toBe(styleKey({ fs: 12, bl: 1 }));
  });
});

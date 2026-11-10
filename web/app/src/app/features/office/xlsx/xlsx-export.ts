import { savedLinkUrl } from '../shared/safe-link';
import ExcelJS from 'exceljs';

import { rangeA1 } from '../shared/a1';
import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import {
  RESOURCE_DRAWINGS,
  RESOURCE_FILTER,
  RESOURCE_NOTES,
  UniverCell,
  UniverDrawingsForSheet,
  UniverFilter,
  UniverNote,
  UniverSheet,
  UniverWorkbook,
  pxToExcelWidth,
  pxToPoints,
  readResource,
} from './univer-sheet-types';
import { injectSlicerReferences } from './xlsx-slicers';
import { serialToDate } from './xlsx-import';
import { definedNamesFromWorkbook } from './xlsx-defined-names';
import { applySheetExtras, applyWorkbookExtras } from './xlsx-extras';
import { PreservedParts, XLSM_MIME, XLSX_MIME, hasMacros, hasPreservedParts, injectPreservedParts } from './xlsx-preserve';
import { writeSheetRules } from './xlsx-sheet-rules';
import { UniverStyle, colorToArgb, styleToExcel } from './xlsx-styles';
import { RESOURCE_THREAD_COMMENTS, UniverComment, cellThreads, legacyNoteText } from './xlsx-threaded-comments';

export interface XlsxExportResult {
  bytes: Uint8Array;
  report: FidelityReport;
}

/**
 * Writes a Univer workbook snapshot as an .xlsx; the mirror of xlsxToWorkbook. With `preserved`
 * (from the import), what the original held beyond Univer's model goes back into the file,
 * and the defined names Univer edited are written into the workbook part.
 */
export async function workbookToXlsx(workbook: UniverWorkbook, preserved?: PreservedParts | null): Promise<XlsxExportResult> {
  const fidelity = new FidelityCollector();
  const excel = workbookToExcel(workbook, fidelity, preserved ?? undefined);
  const buffer = await excel.xlsx.writeBuffer();
  let bytes: Uint8Array = new Uint8Array(buffer as ArrayBuffer);
  const definedNames = definedNamesFromWorkbook(workbook);
  if ((preserved && hasPreservedParts(preserved)) || definedNames.length) {
    bytes = await injectPreservedParts(
      bytes,
      preserved ?? { parts: {}, rels: {}, bySheet: {}, pivotCaches: [], externalLinks: [], customXml: [] },
      { definedNames },
    );
  }
  bytes = await writeSheetRules(bytes, workbook, preserved?.rules, fidelity, hasMacros(preserved) ? XLSM_MIME : XLSX_MIME);
  if (preserved?.slicers) bytes = await injectSlicerReferences(bytes, preserved.slicers, hasMacros(preserved) ? XLSM_MIME : XLSX_MIME);
  return { bytes, report: fidelity.report() };
}

export function workbookToExcel(
  workbook: UniverWorkbook,
  fidelity = new FidelityCollector(),
  preserved?: PreservedParts,
): ExcelJS.Workbook {
  const excel = new ExcelJS.Workbook();
  const threads = readResource<Record<string, UniverComment[]>>(workbook, RESOURCE_THREAD_COMMENTS) ?? {};
  const notes = readResource<Record<string, Record<number, Record<number, UniverNote>>>>(workbook, RESOURCE_NOTES) ?? {};
  const drawings = readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS) ?? {};
  const filters = readResource<Record<string, UniverFilter>>(workbook, RESOURCE_FILTER) ?? {};
  applyWorkbookExtras(excel, preserved?.extras);
  for (const sheetId of workbook.sheetOrder) {
    const sheet = workbook.sheets[sheetId];
    if (!sheet) continue;
    const ws = excel.addWorksheet(sheet.name || sheetId, sheetOptions(sheet));
    exportLayout(ws, sheet, workbook.styles);
    exportCells(ws, sheet, workbook.styles, fidelity);
    exportNotes(ws, notes[sheetId] ?? {});
    for (const thread of cellThreads(threads[sheetId] ?? [])) ws.getCell(thread.ref).note = legacyNoteText(thread);
    // A sheet with charts or shapes keeps its drawing part, which xlsx-chart-save.ts brings up to date.
    if (!preserved?.bySheet[sheet.name]?.drawing) exportImages(ws, excel, drawings[sheetId], sheet, fidelity);
    const filter = filters[sheetId];
    if (filter?.ref) ws.autoFilter = rangeA1(filter.ref);
    applySheetExtras(ws, preserved?.extras?.sheets[sheet.name]);
  }
  return excel;
}

function sheetOptions(sheet: UniverSheet): Partial<ExcelJS.AddWorksheetOptions> {
  const opts: Partial<ExcelJS.AddWorksheetOptions> = {
    state: sheet.hidden === 2 ? 'veryHidden' : sheet.hidden === 1 ? 'hidden' : 'visible',
  };
  const tab = colorToArgb(sheet.tabColor ? { rgb: sheet.tabColor } : null);
  if (tab) opts.properties = { tabColor: tab } as ExcelJS.WorksheetProperties;
  const view: Partial<ExcelJS.WorksheetView> = { showGridLines: sheet.showGridlines !== 0, rightToLeft: sheet.rightToLeft === 1 };
  if (sheet.zoomRatio && Math.round(sheet.zoomRatio * 100) !== 100) view.zoomScale = Math.round(sheet.zoomRatio * 100);
  if (sheet.freeze && (sheet.freeze.xSplit > 0 || sheet.freeze.ySplit > 0)) {
    Object.assign(view, { state: 'frozen', xSplit: sheet.freeze.xSplit, ySplit: sheet.freeze.ySplit });
  }
  opts.views = [view as ExcelJS.WorksheetView];
  return opts;
}

function exportCells(ws: ExcelJS.Worksheet, sheet: UniverSheet, styles: Record<string, UniverStyle>, fidelity: FidelityCollector): void {
  for (const [r, row] of Object.entries(sheet.cellData ?? {})) {
    for (const [c, cell] of Object.entries(row ?? {})) {
      if (!cell) continue;
      const target = ws.getCell(Number(r) + 1, Number(c) + 1);
      const style = typeof cell.s === 'string' ? styles[cell.s] : (cell.s as UniverStyle | null | undefined);
      if (style) target.style = styleToExcel(style);
      target.value = cellValue(cell, style, fidelity, sheet.name);
    }
  }
  for (const merge of sheet.mergeData ?? []) {
    try {
      ws.mergeCells(merge.startRow + 1, merge.startColumn + 1, merge.endRow + 1, merge.endColumn + 1);
    } catch {
      fidelity.warn('Overlapping merged cells', sheet.name);
    }
  }
}

function cellValue(cell: UniverCell, style: UniverStyle | null | undefined, fidelity: FidelityCollector, where: string): ExcelJS.CellValue {
  const link = cell.p?.body.customRanges?.find(r => r.rangeType === 0 && savedLinkUrl(r.properties) !== '');
  const text = cell.p ? cell.p.body.dataStream.replace(/[\r\n]+$/, '') : undefined;
  if (link) {
    return {
      text: text ?? String(cell.v ?? ''),
      hyperlink: savedLinkUrl(link.properties),
      tooltip: link.properties?.['tooltip'] as string | undefined,
    };
  }
  if (cell.p && cell.p.body.textRuns?.length) {
    const richText: ExcelJS.RichText[] = [];
    let cursor = 0;
    const body = text ?? '';
    for (const run of cell.p.body.textRuns) {
      if (run.st > cursor) richText.push({ text: body.slice(cursor, run.st) });
      const font = styleToExcel(run.ts).font;
      richText.push(font ? { text: body.slice(run.st, run.ed), font } : { text: body.slice(run.st, run.ed) });
      cursor = run.ed;
    }
    if (cursor < body.length) richText.push({ text: body.slice(cursor) });
    return { richText };
  }
  if (cell.p && text !== undefined && cell.v === undefined) return text;
  const plain = plainValue(cell, style);
  if (cell.f) {
    const formula = cell.f.replace(/^=/, '');
    const value: ExcelJS.CellFormulaValue & { shareType?: string; ref?: string } = {
      formula,
      result: plain === null ? undefined : (plain as ExcelJS.CellFormulaValue['result']),
    };
    if (cell.ref) {
      value.shareType = 'array';
      value.ref = cell.ref;
    }
    return value;
  }
  if (cell.si && !cell.f) fidelity.warn('Shared formulas written as values', where);
  return plain;
}

const DATE_FORMAT = /(^|[^\\])[ymdh]/i;
const EXCEL_ERRORS = /^#(DIV\/0!|N\/A|NAME\?|NULL!|NUM!|REF!|VALUE!)$/;

function plainValue(cell: UniverCell, style: UniverStyle | null | undefined): ExcelJS.CellValue {
  if (cell.v === undefined || cell.v === null) return null;
  if (cell.t === 3) return cell.v === 1 || cell.v === (true as never);
  if (typeof cell.v === 'string' && EXCEL_ERRORS.test(cell.v)) return { error: cell.v as ExcelJS.CellErrorValue['error'] };
  if (cell.t === 1 || cell.t === 4) return String(cell.v);
  if (typeof cell.v === 'number') {
    const pattern = style?.n?.pattern;
    if (pattern && DATE_FORMAT.test(pattern) && !/[#0]/.test(pattern.replace(/\[.*?\]/g, ''))) return serialToDate(cell.v);
    return cell.v;
  }
  return cell.v;
}

/** Column and row geometry and default styles; before the cells, so a column style never overwrites a cell's own. */
function exportLayout(ws: ExcelJS.Worksheet, sheet: UniverSheet, styles: Record<string, UniverStyle>): void {
  for (const [c, data] of Object.entries(sheet.columnData ?? {})) {
    if (!data) continue;
    const col = ws.getColumn(Number(c) + 1);
    if (data.w) col.width = pxToExcelWidth(data.w);
    if (data.hd) col.hidden = true;
    const style = typeof data.s === 'string' ? styles[data.s] : null;
    if (style) col.style = styleToExcel(style);
  }
  for (const [r, data] of Object.entries(sheet.rowData ?? {})) {
    if (!data) continue;
    const row = ws.getRow(Number(r) + 1);
    if (data.h) row.height = pxToPoints(data.h);
    if (data.hd) row.hidden = true;
    const style = typeof data.s === 'string' ? styles[data.s] : null;
    if (style) (row as unknown as { style: Partial<ExcelJS.Style> }).style = styleToExcel(style);
  }
}

function exportNotes(ws: ExcelJS.Worksheet, notes: Record<number, Record<number, UniverNote>>): void {
  for (const row of Object.values(notes)) {
    for (const note of Object.values(row ?? {})) {
      if (!note?.note) continue;
      const target = ws.getCell(note.row + 1, note.col + 1);
      target.note = note.note;
      // ExcelJS only writes cells that have a value or a style, and a note on an unwritten cell is
      // lost on read; an empty font is enough to make the cell exist and imports as no style.
      if (target.value === null && !Object.keys(target.style ?? {}).length) target.style = { font: {} };
    }
  }
}

function exportImages(
  ws: ExcelJS.Worksheet,
  excel: ExcelJS.Workbook,
  drawings: UniverDrawingsForSheet | undefined,
  sheet: UniverSheet,
  fidelity: FidelityCollector,
): void {
  if (!drawings) return;
  for (const id of drawings.order) {
    const image = drawings.data[id];
    if (!image) continue;
    // Charts drawn from their data (xlsx-import) are pictures of the real chart, which goes back as itself.
    if (id.startsWith('chart-') || id.startsWith('slicer-')) continue;
    if (image.drawingType !== 0 || image.imageSourceType !== 'BASE64' || !image.source.startsWith('data:')) {
      fidelity.drop('Shapes and linked images', sheet.name);
      continue;
    }
    const m = /^data:image\/(png|jpeg|jpg|gif);base64,(.*)$/s.exec(image.source);
    if (!m) {
      fidelity.drop('Images of an unsupported type', sheet.name);
      continue;
    }
    const extension = (m[1] === 'jpg' ? 'jpeg' : m[1]) as 'png' | 'jpeg' | 'gif';
    const imageId = excel.addImage({ base64: m[2], extension });
    const { from, to } = image.sheetTransform;
    const colFraction = (col: number, offset: number) => col + offset / (sheet.columnData?.[col]?.w ?? sheet.defaultColumnWidth);
    const rowFraction = (row: number, offset: number) => row + offset / (sheet.rowData?.[row]?.h ?? sheet.defaultRowHeight);
    ws.addImage(imageId, {
      tl: { col: colFraction(from.column, from.columnOffset), row: rowFraction(from.row, from.rowOffset) } as ExcelJS.Anchor,
      br: { col: colFraction(to.column, to.columnOffset), row: rowFraction(to.row, to.rowOffset) } as ExcelJS.Anchor,
    });
  }
}

/** A sheet as CSV (RFC 4180): values as Univer shows them, formulas by their cached results. */
export function sheetToCsv(sheet: UniverSheet, delimiter = ','): string {
  const rows: string[] = [];
  let maxRow = -1;
  let maxCol = -1;
  for (const [r, row] of Object.entries(sheet.cellData ?? {})) {
    for (const [c, cell] of Object.entries(row ?? {})) {
      if (cell && cell.v !== undefined && cell.v !== null) {
        maxRow = Math.max(maxRow, Number(r));
        maxCol = Math.max(maxCol, Number(c));
      }
    }
  }
  for (let r = 0; r <= maxRow; r++) {
    const fields: string[] = [];
    for (let c = 0; c <= maxCol; c++) {
      const cell = sheet.cellData?.[r]?.[c];
      const v = cell?.v;
      const text = v === undefined || v === null ? '' : cell?.t === 3 ? (v === 1 ? 'TRUE' : 'FALSE') : String(v);
      fields.push(/[",\r\n\t]/.test(text) || text.includes(delimiter) ? `"${text.replace(/"/g, '""')}"` : text);
    }
    rows.push(fields.join(delimiter));
  }
  return rows.join('\r\n') + (rows.length ? '\r\n' : '');
}

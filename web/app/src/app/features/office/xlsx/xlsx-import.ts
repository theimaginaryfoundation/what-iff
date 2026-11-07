import { linkProperties } from '../shared/safe-link';
import ExcelJS from 'exceljs';

import { cellA1, parseRangeA1 } from '../shared/a1';
import { chartSvgDataUrl } from '../shared/chart-svg';
import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import { OoxmlPackage, emuToPx, parseXml } from '../shared/ooxml';
import { addSlicerPictures, notePivotLimits } from '../sheets/slicers/slicer-pictures';
import { parseChartXml } from './xlsx-chart-xml';
import { ChartSpec, DRAWN_NOTICE, KEPT_FEATURES_NOTICE, NOT_EDITABLE_NOTICE, cachedSeries, chartDataFor } from '../sheets/charts/chart-spec';
import { chartSpecsFromPreserved } from './xlsx-chart-save';
import { XlsxDefinedName, definedNamesToWorkbook, parseDefinedNames } from './xlsx-defined-names';
import { collectExtras } from './xlsx-extras';
import { PreservedParts, collectPreservedParts } from './xlsx-preserve';
import {
  DEFAULT_COLUMN_WIDTH_PX,
  DEFAULT_ROW_HEIGHT_PX,
  RESOURCE_DRAWINGS,
  RESOURCE_FILTER,
  RESOURCE_NOTES,
  UniverCell,
  UniverCellDoc,
  UniverDrawingsForSheet,
  UniverNote,
  UniverRange,
  UniverSheet,
  UniverSheetImage,
  UniverTextRun,
  UniverWorkbook,
  excelWidthToPx,
  pointsToPx,
  writeResource,
} from './univer-sheet-types';
import { readSheetRules } from './xlsx-sheet-rules';
import { UniverStyle, colorFromArgb, styleFromExcel, styleKey } from './xlsx-styles';
import { ExcelThemeColors, parseThemeColors } from './xlsx-theme';

export interface XlsxImportOptions {
  /** The workbook unit id Univer will use. */
  id: string;
  name: string;
}

export interface XlsxImportResult {
  workbook: UniverWorkbook;
  report: FidelityReport;
  /** What the original holds that Univer has no model for, to write back on save (xlsx-preserve.ts, xlsx-extras.ts). */
  preserved?: PreservedParts;
}

export const UNIVER_APP_VERSION = '1.3.1';
/** Univer's FormulaType.ARRAY. */
export const FORMULA_TYPE_ARRAY = 2;
/** Excel's epoch for date serials (the 1900 date system). */
const EXCEL_EPOCH_MS = Date.UTC(1899, 11, 30);
const MS_PER_DAY = 86_400_000;

/**
 * Reads an .xlsx into a Univer workbook snapshot (ADR 0x043, office suite): values, formulas and
 * their cached results, number formats, fonts, fills, borders, alignment, merges, column widths,
 * row heights, hidden rows and columns, freeze panes, sheet order, tab colours, hidden sheets,
 * rich text, hyperlinks, data validation, conditional formatting, notes, images and the
 * auto-filter. Charts, pivot tables, macros and defined names are reported as dropped.
 */
export async function xlsxToWorkbook(bytes: ArrayBuffer | Uint8Array, opts: XlsxImportOptions): Promise<XlsxImportResult> {
  const excel = new ExcelJS.Workbook();
  await excel.xlsx.load(bytes as never);
  const result = excelToWorkbook(excel, opts);
  // Charts, pivot tables, tables, macros and the rest: ExcelJS reads none of them, so they come
  // from the package itself; what ExcelJS reads but Univer has no model for rides along as extras.
  let preserved: PreservedParts;
  let definedNames: XlsxDefinedName[] = [];
  try {
    const pkg = await OoxmlPackage.open(bytes);
    preserved = await collectPreservedParts(pkg);
    const wb = await pkg.xml('xl/workbook.xml');
    if (wb) definedNames = parseDefinedNames(wb);
  } catch {
    preserved = { parts: {}, rels: {}, bySheet: {}, pivotCaches: [], externalLinks: [], customXml: [] };
  }
  preserved.extras = collectExtras(excel);
  const fidelity = new FidelityCollector();
  for (const item of result.report.dropped) for (let i = 0; i < (item.count ?? 1); i++) fidelity.drop(item.feature, item.where);
  for (const item of result.report.warnings) for (let i = 0; i < (item.count ?? 1); i++) fidelity.warn(item.feature, item.where);
  // Conditional formatting, validation and threaded comments come from the package as well: ExcelJS
  // reads them only in part (xlsx-sheet-rules.ts).
  preserved.rules = await readSheetRules(
    await OoxmlPackage.open(bytes),
    result.workbook,
    parseThemeColors((excel.model as unknown as { themes?: Record<string, string> }).themes?.['theme1']),
    fidelity,
  );
  definedNamesToWorkbook(definedNames, result.workbook);
  const chartTheme = parseThemeColors((excel.model as unknown as { themes?: Record<string, string> }).themes?.['theme1']);
  const chartSpecs = chartSpecsFromPreserved(preserved, result.workbook, chartTheme);
  addPreservedCharts(result.workbook, preserved, excel, chartSpecs);
  addSlicerPictures(result.workbook, preserved);
  notePreserved(preserved, fidelity);
  for (const spec of chartSpecs) {
    if (!spec.editable) fidelity.warn(NOT_EDITABLE_NOTICE, spec.sheet);
    if (spec.limits.includes('Trendlines and error bars')) fidelity.warn(KEPT_FEATURES_NOTICE, spec.sheet);
  }
  return { ...result, report: fidelity.report(), preserved };
}

/** What is carried but not shown or editable, and what is not carried. */
function notePreserved(preserved: PreservedParts, fidelity: FidelityCollector): void {
  for (const [name, entry] of Object.entries(preserved.bySheet)) {
    if (entry.drawing?.charts.length) fidelity.warn(DRAWN_NOTICE, name);
    if (entry.drawing?.shapes) fidelity.warn('Shapes and text boxes are kept in the file but not shown here', name);
    if (entry.tables.length)
      fidelity.warn('Tables are kept in the file but not editable here; edits to their headers do not update them', name);
    if (entry.exts.length) fidelity.warn('Sparklines are kept in the file but not shown here', name);
  }
  notePivotLimits(preserved, fidelity);
  if (Object.keys(preserved.slicers?.sheetTimelines ?? {}).length) fidelity.warn('Timelines are kept in the file but not shown here; edit them in Excel');
  if (preserved.externalLinks.length) fidelity.warn('Links to other workbooks are kept in the file');
  if (preserved.vbaProject) fidelity.warn('Macros are kept in the file but cannot run here');
}

/** Draws each kept chart from its cached data as a picture at its anchor. */
function addPreservedCharts(workbook: UniverWorkbook, preserved: PreservedParts, excel: ExcelJS.Workbook, specs: readonly ChartSpec[]): void {
  if (!Object.values(preserved.bySheet).some(s => s.drawing?.charts.length)) return;
  const theme = parseThemeColors((excel.model as unknown as { themes?: Record<string, string> }).themes?.['theme1']);
  const drawings = readResourceMap<UniverDrawingsForSheet>(workbook, RESOURCE_DRAWINGS);
  for (const sheetId of workbook.sheetOrder) {
    const sheet = workbook.sheets[sheetId];
    const entry = preserved.bySheet[sheet.name];
    if (!entry?.drawing?.charts.length) continue;
    const colLeft = (col: number) => {
      let x = 0;
      for (let i = 0; i < col; i++) x += sheet.columnData[i]?.w ?? sheet.defaultColumnWidth;
      return x;
    };
    const rowTop = (row: number) => {
      let y = 0;
      for (let i = 0; i < row; i++) y += sheet.rowData[i]?.h ?? sheet.defaultRowHeight;
      return y;
    };
    const forSheet = (drawings[sheetId] ??= { data: {}, order: [] });
    entry.drawing.charts.forEach((chart, index) => {
      const part = preserved.parts[chart.chartPath];
      if (!part) return;
      let data: ReturnType<typeof parseChartXml> = null;
      try {
        data = parseChartXml(parseXml(new TextDecoder().decode(part.bytes)), theme);
      } catch {
        data = null;
      }
      const spec = specs.find(c => c.id === `chart-${sheetId}-${index}`);
      if (spec?.editable) data = chartDataFor(spec, cachedSeries(spec), theme);
      const left = colLeft(chart.from.col) + emuToPx(chart.from.colOff);
      const top = rowTop(chart.from.row) + emuToPx(chart.from.rowOff);
      const width = Math.max(40, colLeft(chart.to.col) + emuToPx(chart.to.colOff) - left);
      const height = Math.max(30, rowTop(chart.to.row) + emuToPx(chart.to.rowOff) - top);
      const source = data ? chartSvgDataUrl(data, { width, height, background: '#FFFFFF' }) : placeholderSvg(width, height, chart.name);
      const drawingId = `chart-${sheetId}-${index}`;
      forSheet.data[drawingId] = {
        unitId: workbook.id,
        subUnitId: sheetId,
        drawingId,
        drawingType: 0,
        imageSourceType: 'BASE64',
        source,
        transform: { left, top, width, height, angle: 0 },
        sheetTransform: {
          from: {
            column: chart.from.col,
            columnOffset: emuToPx(chart.from.colOff),
            row: chart.from.row,
            rowOffset: emuToPx(chart.from.rowOff),
          },
          to: { column: chart.to.col, columnOffset: emuToPx(chart.to.colOff), row: chart.to.row, rowOffset: emuToPx(chart.to.rowOff) },
          angle: 0,
        },
        anchorType: '1',
      };
      forSheet.order.push(drawingId);
    });
  }
  if (Object.keys(drawings).length) writeResource(workbook, RESOURCE_DRAWINGS, drawings);
}

function placeholderSvg(width: number, height: number, name: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><rect width="100%" height="100%" fill="#FFFFFF" stroke="#BFBFBF"/><text x="50%" y="50%" text-anchor="middle" font-family="Arial" font-size="12" fill="#808080">${name.replace(/[<>&]/g, '')}</text></svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

function readResourceMap<T>(workbook: UniverWorkbook, name: string): Record<string, T> {
  const raw = workbook.resources.find(r => r.name === name)?.data;
  if (!raw) return {};
  try {
    return JSON.parse(raw) as Record<string, T>;
  } catch {
    return {};
  }
}

export function excelToWorkbook(excel: ExcelJS.Workbook, opts: XlsxImportOptions): XlsxImportResult {
  const fidelity = new FidelityCollector();
  const theme = parseThemeColors((excel.model as unknown as { themes?: Record<string, string> }).themes?.['theme1']);
  const styles = new StyleTable(theme);
  const workbook: UniverWorkbook = {
    id: opts.id,
    name: opts.name,
    appVersion: UNIVER_APP_VERSION,
    locale: 'enUS',
    sheetOrder: [],
    sheets: {},
    styles: {},
    resources: [],
  };
  const notes: Record<string, Record<number, Record<number, UniverNote>>> = {};
  const drawings: Record<string, UniverDrawingsForSheet> = {};
  const filters: Record<string, { ref: UniverRange }> = {};

  excel.eachSheet(ws => {
    const id = `sheet-${ws.id}`;
    workbook.sheetOrder.push(id);
    const sheet = importSheet(ws, id, styles, fidelity);
    workbook.sheets[id] = sheet;
    const sheetNotes = importNotes(ws);
    if (Object.keys(sheetNotes).length) notes[id] = sheetNotes;
    const images = importImages(ws, excel, opts.id, id, sheet, fidelity);
    if (images.order.length) drawings[id] = images;
    const filter = importFilter(ws);
    if (filter) filters[id] = filter;
  });
  workbook.styles = styles.table;
  if (Object.keys(notes).length) writeResource(workbook, RESOURCE_NOTES, notes);
  if (Object.keys(drawings).length) writeResource(workbook, RESOURCE_DRAWINGS, drawings);
  if (Object.keys(filters).length) writeResource(workbook, RESOURCE_FILTER, filters);
  return { workbook, report: fidelity.report() };
}

/** Dedupes cell styles into the workbook's style table. */
class StyleTable {
  readonly table: Record<string, UniverStyle> = {};
  private readonly ids = new Map<string, string>();
  private next = 0;

  constructor(readonly theme: ExcelThemeColors) {}

  /** The Univer style for an ExcelJS style, with theme and indexed colours resolved. */
  fromExcel(style: Partial<ExcelJS.Style> | undefined): UniverStyle | null {
    return styleFromExcel(style, this.theme);
  }

  idFor(style: UniverStyle | null): string | undefined {
    if (!style) return undefined;
    const key = styleKey(style);
    let id = this.ids.get(key);
    if (!id) {
      id = `s${this.next++}`;
      this.ids.set(key, id);
      this.table[id] = style;
    }
    return id;
  }
}

function importSheet(ws: ExcelJS.Worksheet, id: string, styles: StyleTable, fidelity: FidelityCollector): UniverSheet {
  const sheet: UniverSheet = {
    id,
    name: ws.name,
    tabColor: colorFromArgb(ws.properties?.tabColor, styles.theme)?.rgb ?? '',
    hidden: ws.state === 'veryHidden' ? 2 : ws.state === 'hidden' ? 1 : 0,
    freeze: { xSplit: 0, ySplit: 0, startRow: -1, startColumn: -1 },
    rowCount: Math.max(ws.rowCount + 100, 1000),
    columnCount: Math.max(ws.columnCount + 10, 26),
    zoomRatio: 1,
    scrollTop: 0,
    scrollLeft: 0,
    defaultColumnWidth: DEFAULT_COLUMN_WIDTH_PX,
    defaultRowHeight: DEFAULT_ROW_HEIGHT_PX,
    mergeData: [],
    cellData: {},
    rowData: {},
    columnData: {},
    rowHeader: { width: 46 },
    columnHeader: { height: 20 },
    showGridlines: ws.views?.[0]?.showGridLines === false ? 0 : 1,
    rightToLeft: ws.views?.[0]?.rightToLeft ? 1 : 0,
  };
  const zoom = (ws.views?.[0] as { zoomScale?: number } | undefined)?.zoomScale;
  if (zoom && zoom > 0) sheet.zoomRatio = zoom / 100;
  const view = ws.views?.find(v => v.state === 'frozen') as (Partial<ExcelJS.WorksheetViewFrozen> & { state: 'frozen' }) | undefined;
  if (view) {
    const x = view.xSplit ?? 0;
    const y = view.ySplit ?? 0;
    sheet.freeze = { xSplit: x, ySplit: y, startRow: y, startColumn: x };
  }
  ws.columns?.forEach((col, i) => {
    if (!col) return;
    const data: { w?: number; hd?: 0 | 1; s?: string } = {};
    if (col.width) data.w = excelWidthToPx(col.width);
    if (col.hidden) data.hd = 1;
    const style = styles.idFor(styles.fromExcel(col.style));
    if (style) data.s = style;
    if (Object.keys(data).length) sheet.columnData[i] = data;
  });
  ws.eachRow({ includeEmpty: true }, (row, r) => {
    const data: { h?: number; hd?: 0 | 1; s?: string } = {};
    if (row.height) data.h = pointsToPx(row.height);
    if (row.hidden) data.hd = 1;
    const style = styles.idFor(styles.fromExcel((row as unknown as { style?: Partial<ExcelJS.Style> }).style));
    if (style) data.s = style;
    if (Object.keys(data).length) sheet.rowData[r - 1] = data;
    row.eachCell({ includeEmpty: true }, (cell, c) => {
      const value = importCell(cell, r - 1, c - 1, styles, fidelity, ws.name);
      if (value) (sheet.cellData[r - 1] ??= {})[c - 1] = value;
    });
  });
  const merges = ((ws as unknown as { model?: { merges?: string[] } }).model?.merges ?? []) as string[];
  for (const ref of merges) {
    const range = parseRangeA1(ref);
    if (range) sheet.mergeData.push(range);
  }
  return sheet;
}

function importCell(
  cell: ExcelJS.Cell,
  row: number,
  col: number,
  styles: StyleTable,
  fidelity: FidelityCollector,
  where: string,
): UniverCell | null {
  const out: UniverCell = {};
  const styleId = styles.idFor(styles.fromExcel(cell.style));
  if (styleId) out.s = styleId;
  const raw = cell.value;
  if (raw === null || raw === undefined) {
    return styleId ? out : null;
  }
  if (typeof raw === 'object' && raw !== null && ('formula' in raw || 'sharedFormula' in raw)) {
    const f = raw as ExcelJS.CellFormulaValue & ExcelJS.CellSharedFormulaValue & { shareType?: string; ref?: string };
    // ExcelJS translates a shared formula from its master for the dependent cells.
    const formula = f.formula ?? cell.formula;
    if (formula) out.f = `=${formula}`;
    else fidelity.warn('Shared formulas kept as values', where);
    if (f.shareType === 'array' && f.ref) {
      out.ref = f.ref;
      out.ft = FORMULA_TYPE_ARRAY;
    }
    assignValue(out, f.result as ExcelJS.CellValue, cell, styles);
    return out;
  }
  if (typeof raw === 'object' && raw !== null && 'richText' in raw) {
    const runs = (raw as ExcelJS.CellRichTextValue).richText;
    out.p = richTextDoc(
      row,
      col,
      runs.map(r => ({ text: r.text, style: styles.fromExcel({ font: r.font as ExcelJS.Font }) })),
    );
    out.v = runs.map(r => r.text).join('');
    out.t = 1;
    return out;
  }
  if (typeof raw === 'object' && raw !== null && 'hyperlink' in raw) {
    const link = raw as ExcelJS.CellHyperlinkValue;
    const text = typeof link.text === 'string' ? link.text : String(link.text ?? link.hyperlink);
    if (link.hyperlink) out.p = hyperlinkDoc(row, col, text, link.hyperlink, link.tooltip);
    out.v = text;
    out.t = 1;
    return out;
  }
  if (typeof raw === 'object' && raw !== null && 'error' in raw) {
    out.v = (raw as ExcelJS.CellErrorValue).error;
    out.t = 1;
    return out;
  }
  assignValue(out, raw, cell, styles);
  return out;
}

function assignValue(out: UniverCell, value: ExcelJS.CellValue | undefined, cell: ExcelJS.Cell, styles: StyleTable): void {
  if (value === null || value === undefined) return;
  if (value instanceof Date) {
    out.v = dateToSerial(value);
    out.t = 2;
    if (!cell.numFmt) {
      // Keep it a date on screen: Univer shows serials as numbers without a format.
      out.s = styles.idFor({ ...(cell.style ? styles.fromExcel(cell.style) : {}), n: { pattern: 'yyyy-mm-dd' } });
    }
    return;
  }
  if (typeof value === 'number') {
    out.v = value;
    out.t = 2;
  } else if (typeof value === 'boolean') {
    out.v = value ? 1 : 0;
    out.t = 3;
  } else if (typeof value === 'object' && value !== null && 'error' in value) {
    out.v = (value as ExcelJS.CellErrorValue).error;
    out.t = 1;
  } else {
    out.v = String(value);
    out.t = 1;
  }
}

export function dateToSerial(date: Date): number {
  return Math.round(((date.getTime() - EXCEL_EPOCH_MS) / MS_PER_DAY) * 1e6) / 1e6;
}

export function serialToDate(serial: number): Date {
  return new Date(EXCEL_EPOCH_MS + serial * MS_PER_DAY);
}

function richTextDoc(row: number, col: number, runs: { text: string; style: UniverStyle | null }[]): UniverCellDoc {
  const text = runs.map(r => r.text).join('');
  const textRuns: UniverTextRun[] = [];
  let offset = 0;
  for (const run of runs) {
    if (run.style) textRuns.push({ st: offset, ed: offset + run.text.length, ts: run.style });
    offset += run.text.length;
  }
  return cellDoc(row, col, text, textRuns);
}

function hyperlinkDoc(row: number, col: number, text: string, url: string, tooltip?: string): UniverCellDoc {
  const doc = cellDoc(row, col, text, []);
  doc.body.customRanges = [
    { startIndex: 0, endIndex: Math.max(0, text.length - 1), rangeId: `link-${row}-${col}`, rangeType: 0, properties: { ...linkProperties(url), tooltip } },
  ];
  return doc;
}

export function cellDoc(row: number, col: number, text: string, textRuns: UniverTextRun[]): UniverCellDoc {
  return {
    id: `cell-${row}-${col}`,
    body: {
      dataStream: `${text}\r\n`,
      textRuns,
      paragraphs: [{ startIndex: text.length }],
      sectionBreaks: [{ startIndex: text.length + 1 }],
    },
    documentStyle: {},
  };
}

// --- notes, images, filter ------------------------------------------------------------------

function importNotes(ws: ExcelJS.Worksheet): Record<number, Record<number, UniverNote>> {
  const out: Record<number, Record<number, UniverNote>> = {};
  ws.eachRow({ includeEmpty: true }, (row, r) => {
    row.eachCell({ includeEmpty: true }, (cell, c) => {
      const note = cell.note;
      if (!note) return;
      const text = typeof note === 'string' ? note : (note.texts ?? []).map(t => t.text).join('');
      if (!text) return;
      (out[r - 1] ??= {})[c - 1] = { id: `note-${r - 1}-${c - 1}`, row: r - 1, col: c - 1, width: 200, height: 80, note: text };
    });
  });
  return out;
}

function importImages(
  ws: ExcelJS.Worksheet,
  excel: ExcelJS.Workbook,
  unitId: string,
  subUnitId: string,
  sheet: UniverSheet,
  fidelity: FidelityCollector,
): UniverDrawingsForSheet {
  const out: UniverDrawingsForSheet = { data: {}, order: [] };
  let images: ReturnType<ExcelJS.Worksheet['getImages']> = [];
  try {
    images = ws.getImages();
  } catch {
    return out;
  }
  const colLeft = (col: number) => {
    let x = 0;
    for (let i = 0; i < col; i++) x += sheet.columnData[i]?.w ?? sheet.defaultColumnWidth;
    return x;
  };
  const rowTop = (row: number) => {
    let y = 0;
    for (let i = 0; i < row; i++) y += sheet.rowData[i]?.h ?? sheet.defaultRowHeight;
    return y;
  };
  images.forEach((img, index) => {
    const source = excel.getImage(Number(img.imageId));
    const base64 = source?.base64 ?? (source?.buffer ? bufferToBase64(source.buffer) : null);
    if (!base64) {
      fidelity.drop('Images without data', ws.name);
      return;
    }
    const mime = source.extension === 'jpeg' ? 'jpeg' : source.extension === 'gif' ? 'gif' : 'png';
    const tl = img.range.tl;
    const fromCol = Math.floor(tl.col);
    const fromRow = Math.floor(tl.row);
    const fromColOff = (tl.col - fromCol) * (sheet.columnData[fromCol]?.w ?? sheet.defaultColumnWidth);
    const fromRowOff = (tl.row - fromRow) * (sheet.rowData[fromRow]?.h ?? sheet.defaultRowHeight);
    let width: number;
    let height: number;
    let to: UniverSheetImage['sheetTransform']['to'];
    const ext = (img.range as { ext?: { width: number; height: number } }).ext;
    if (img.range.br) {
      const br = img.range.br;
      const toCol = Math.floor(br.col);
      const toRow = Math.floor(br.row);
      const toColOff = (br.col - toCol) * (sheet.columnData[toCol]?.w ?? sheet.defaultColumnWidth);
      const toRowOff = (br.row - toRow) * (sheet.rowData[toRow]?.h ?? sheet.defaultRowHeight);
      width = colLeft(toCol) + toColOff - (colLeft(fromCol) + fromColOff);
      height = rowTop(toRow) + toRowOff - (rowTop(fromRow) + fromRowOff);
      to = { column: toCol, columnOffset: toColOff, row: toRow, rowOffset: toRowOff };
    } else {
      width = ext?.width ?? 200;
      height = ext?.height ?? 150;
      to = { column: fromCol, columnOffset: fromColOff + width, row: fromRow, rowOffset: fromRowOff + height };
    }
    const drawingId = `image-${subUnitId}-${index}`;
    const drawing: UniverSheetImage = {
      unitId,
      subUnitId,
      drawingId,
      drawingType: 0,
      imageSourceType: 'BASE64',
      source: base64.startsWith('data:') ? base64 : `data:image/${mime};base64,${base64}`,
      transform: {
        left: colLeft(fromCol) + fromColOff,
        top: rowTop(fromRow) + fromRowOff,
        width: Math.max(1, width),
        height: Math.max(1, height),
        angle: 0,
      },
      sheetTransform: { from: { column: fromCol, columnOffset: fromColOff, row: fromRow, rowOffset: fromRowOff }, to, angle: 0 },
      anchorType: '1',
    };
    out.data[drawingId] = drawing;
    out.order.push(drawingId);
  });
  return out;
}

function bufferToBase64(buffer: ArrayBuffer | Uint8Array | { toString(encoding: string): string }): string {
  if (
    typeof (buffer as { toString(encoding: string): string }).toString === 'function' &&
    !(buffer instanceof ArrayBuffer) &&
    !(buffer instanceof Uint8Array)
  ) {
    return (buffer as { toString(encoding: string): string }).toString('base64');
  }
  const bytes = buffer instanceof Uint8Array ? buffer : new Uint8Array(buffer as ArrayBuffer);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

function importFilter(ws: ExcelJS.Worksheet): { ref: UniverRange } | null {
  const raw = (
    ws as unknown as {
      autoFilter?: string | { from: string | { row: number; column: number }; to: string | { row: number; column: number } };
    }
  ).autoFilter;
  if (!raw) return null;
  if (typeof raw === 'string') {
    const range = parseRangeA1(raw);
    return range ? { ref: range } : null;
  }
  const from = typeof raw.from === 'string' ? raw.from : cellA1(raw.from.row - 1, raw.from.column - 1);
  const to = typeof raw.to === 'string' ? raw.to : cellA1(raw.to.row - 1, raw.to.column - 1);
  const range = parseRangeA1(`${from}:${to}`);
  return range ? { ref: range } : null;
}

/** A CSV or TSV becomes a one-sheet workbook of strings and numbers. */
export function delimitedTextToWorkbook(text: string, delimiter: ',' | '\t', opts: XlsxImportOptions): XlsxImportResult {
  const rows = parseDelimited(text, delimiter);
  const id = 'sheet-1';
  const sheet: UniverSheet = {
    id,
    name: opts.name.replace(/\.[^.]+$/, '') || 'Sheet1',
    tabColor: '',
    hidden: 0,
    freeze: { xSplit: 0, ySplit: 0, startRow: -1, startColumn: -1 },
    rowCount: Math.max(rows.length + 100, 1000),
    columnCount: Math.max(Math.max(0, ...rows.map(r => r.length)) + 10, 26),
    zoomRatio: 1,
    scrollTop: 0,
    scrollLeft: 0,
    defaultColumnWidth: DEFAULT_COLUMN_WIDTH_PX,
    defaultRowHeight: DEFAULT_ROW_HEIGHT_PX,
    mergeData: [],
    cellData: {},
    rowData: {},
    columnData: {},
    rowHeader: { width: 46 },
    columnHeader: { height: 20 },
    showGridlines: 1,
    rightToLeft: 0,
  };
  rows.forEach((cells, r) => {
    cells.forEach((value, c) => {
      if (value === '') return;
      // Only when the number reads back the same: 00123, 1.50 and long IDs are data, and saving
      // the file would otherwise rewrite them.
      const parsed = Number(value);
      const asNumber = Number.isFinite(parsed) && String(parsed) === value ? parsed : null;
      (sheet.cellData[r] ??= {})[c] = asNumber !== null ? { v: asNumber, t: 2 } : { v: value, t: 1 };
    });
  });
  return {
    workbook: {
      id: opts.id,
      name: opts.name,
      appVersion: UNIVER_APP_VERSION,
      locale: 'enUS',
      sheetOrder: [id],
      sheets: { [id]: sheet },
      styles: {},
      resources: [],
    },
    report: { dropped: [], warnings: [] },
  };
}

/** RFC 4180 parsing: quoted fields, doubled quotes, CRLF or LF rows. */
export function parseDelimited(text: string, delimiter: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else quoted = false;
      } else field += ch;
      continue;
    }
    // A quote opens a quoted field only at its start; elsewhere (5" screen) it is just a character.
    if (ch === '"' && field === '') quoted = true;
    else if (ch === delimiter) {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += ch;
  }
  if (field !== '' || row.length) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

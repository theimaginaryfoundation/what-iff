import type ExcelJS from 'exceljs';

/**
 * What ExcelJS reads and writes but Univer has no model for: page setup, headers and footers,
 * sheet views beyond the frozen panes, sheet protection, outline (group) levels, workbook
 * properties, document properties and the theme. Collected from the loaded workbook on import
 * and applied to the one being written on export, so they ride through an edit untouched.
 */

export interface XlsxOutline {
  level: number;
  collapsed?: boolean;
}

export interface XlsxSheetExtras {
  pageSetup?: Partial<ExcelJS.PageSetup>;
  headerFooter?: Partial<ExcelJS.HeaderFooter>;
  /** The first sheet view without its frozen-pane and gridline fields, which Univer owns. */
  view?: Record<string, unknown>;
  sheetProtection?: Record<string, unknown> | null;
  properties?: Partial<ExcelJS.WorksheetProperties>;
  /** Zero-based row index to outline level. */
  rowOutline?: Record<number, XlsxOutline>;
  columnOutline?: Record<number, XlsxOutline>;
}

export interface XlsxExtras {
  /** By sheet name. */
  sheets: Record<string, XlsxSheetExtras>;
  properties?: Partial<ExcelJS.WorkbookProperties>;
  views?: Partial<ExcelJS.WorkbookView>[];
  calcProperties?: Record<string, unknown>;
  /** docProps/core.xml fields as ExcelJS exposes them. */
  document?: Record<string, unknown>;
  /** The theme parts as XML, keyed the way ExcelJS keeps them (theme1). */
  themes?: Record<string, string>;
}

const VIEW_FIELDS_UNIVER_OWNS = new Set(['state', 'xSplit', 'ySplit', 'topLeftCell', 'showGridLines', 'rightToLeft', 'zoomScale']);
const DOCUMENT_FIELDS = [
  'creator',
  'lastModifiedBy',
  'lastPrinted',
  'created',
  'modified',
  'title',
  'subject',
  'description',
  'keywords',
  'category',
  'company',
  'manager',
] as const;

export function collectExtras(excel: ExcelJS.Workbook): XlsxExtras {
  const out: XlsxExtras = { sheets: {} };
  excel.eachSheet(ws => {
    out.sheets[ws.name] = sheetExtras(ws);
  });
  const model = excel.model as unknown as { themes?: Record<string, string> };
  if (model.themes && Object.keys(model.themes).length) out.themes = { ...model.themes };
  if (excel.properties && Object.keys(excel.properties).length) out.properties = { ...excel.properties };
  if (excel.views?.length) out.views = excel.views.map(v => ({ ...v }));
  if (excel.calcProperties && Object.keys(excel.calcProperties).length) out.calcProperties = { ...excel.calcProperties };
  const document: Record<string, unknown> = {};
  for (const field of DOCUMENT_FIELDS) {
    const value = (excel as unknown as Record<string, unknown>)[field];
    if (value !== undefined && value !== null && value !== '') document[field] = value;
  }
  if (Object.keys(document).length) out.document = document;
  return out;
}

function sheetExtras(ws: ExcelJS.Worksheet): XlsxSheetExtras {
  const out: XlsxSheetExtras = {};
  if (ws.pageSetup && Object.keys(ws.pageSetup).length) out.pageSetup = clone(ws.pageSetup);
  if (ws.headerFooter && Object.keys(ws.headerFooter).length) out.headerFooter = clone(ws.headerFooter);
  const view = ws.views?.[0] as Record<string, unknown> | undefined;
  if (view) {
    const rest: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(view)) if (!VIEW_FIELDS_UNIVER_OWNS.has(k) && v !== undefined) rest[k] = v;
    if (Object.keys(rest).length) out.view = rest;
  }
  const protection = (ws as unknown as { sheetProtection?: Record<string, unknown> | null }).sheetProtection;
  if (protection) out.sheetProtection = clone(protection);
  const props = ws.properties as Partial<ExcelJS.WorksheetProperties> & { dyDescent?: number; defaultColWidth?: number };
  const kept: Record<string, unknown> = {};
  for (const key of ['outlineLevelCol', 'outlineLevelRow', 'defaultRowHeight', 'defaultColWidth', 'dyDescent'] as const) {
    if (props?.[key] !== undefined) kept[key] = props[key];
  }
  if (Object.keys(kept).length) out.properties = kept as Partial<ExcelJS.WorksheetProperties>;
  const rows: Record<number, XlsxOutline> = {};
  ws.eachRow({ includeEmpty: true }, (row, r) => {
    if (row.outlineLevel) rows[r - 1] = { level: row.outlineLevel, collapsed: row.collapsed || undefined };
  });
  if (Object.keys(rows).length) out.rowOutline = rows;
  const cols: Record<number, XlsxOutline> = {};
  ws.columns?.forEach((col, i) => {
    if (col?.outlineLevel) cols[i] = { level: col.outlineLevel, collapsed: col.collapsed || undefined };
  });
  if (Object.keys(cols).length) out.columnOutline = cols;
  return out;
}

/** Applies the workbook-level extras; sheet extras go through `applySheetExtras` as each sheet is added. */
export function applyWorkbookExtras(excel: ExcelJS.Workbook, extras: XlsxExtras | undefined): void {
  if (extras?.themes) (excel as unknown as { _themes?: Record<string, string> })._themes = { ...extras.themes };
  if (extras?.properties) excel.properties = { ...excel.properties, ...extras.properties } as ExcelJS.WorkbookProperties;
  if (extras?.views?.length) excel.views = extras.views.map(v => ({ ...v })) as ExcelJS.WorkbookView[];
  for (const [field, value] of Object.entries(extras?.document ?? {})) (excel as unknown as Record<string, unknown>)[field] = value;
  // Cached results may be stale after an edit here; Excel recomputes everything on open.
  excel.calcProperties = { ...(extras?.calcProperties ?? {}), fullCalcOnLoad: true } as ExcelJS.CalculationProperties;
}

/** The sheet-level extras, applied after the cells so outline levels land on the rows and columns that exist. */
export function applySheetExtras(ws: ExcelJS.Worksheet, extras: XlsxSheetExtras | undefined): void {
  if (!extras) return;
  if (extras.pageSetup) ws.pageSetup = { ...ws.pageSetup, ...clone(extras.pageSetup) };
  if (extras.headerFooter) ws.headerFooter = { ...ws.headerFooter, ...clone(extras.headerFooter) };
  if (extras.view) {
    const views = ws.views?.length ? ws.views : [{} as ExcelJS.WorksheetView];
    views[0] = { ...clone(extras.view), ...views[0] } as ExcelJS.WorksheetView;
    ws.views = views;
  }
  if (extras.sheetProtection) (ws as unknown as { sheetProtection: unknown }).sheetProtection = clone(extras.sheetProtection);
  if (extras.properties) Object.assign(ws.properties, clone(extras.properties));
  for (const [r, outline] of Object.entries(extras.rowOutline ?? {})) {
    const row = ws.getRow(Number(r) + 1);
    row.outlineLevel = outline.level;
  }
  for (const [c, outline] of Object.entries(extras.columnOutline ?? {})) {
    const col = ws.getColumn(Number(c) + 1);
    col.outlineLevel = outline.level;
  }
}

function clone<T>(value: T): T {
  return JSON.parse(
    JSON.stringify(value, (_k, v) => (v instanceof Date ? { __date: v.toISOString() } : v)),
    (_k, v) => (v && typeof v === 'object' && typeof v.__date === 'string' ? new Date(v.__date) : v),
  ) as T;
}

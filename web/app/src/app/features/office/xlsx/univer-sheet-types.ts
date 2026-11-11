/**
 * The slice of Univer's snapshot the xlsx converters read and write, typed here so the converters
 * are plain TypeScript with no runtime dependency on Univer (the editor loads Univer lazily; the
 * converters run in tests and could run in a worker). Field names follow Univer's IWorkbookData.
 */
import type { UniverStyle } from './xlsx-styles';

export interface UniverRange {
  startRow: number;
  startColumn: number;
  endRow: number;
  endColumn: number;
}

export interface UniverTextRun {
  st: number;
  ed: number;
  ts?: UniverStyle;
}

export interface UniverCustomRange {
  startIndex: number;
  endIndex: number;
  rangeId: string;
  /** CustomRangeType.HYPERLINK is 0. */
  rangeType: number;
  properties?: Record<string, unknown>;
}

/** A cell's rich text: Univer's IDocumentData, as much of it as a cell uses. */
export interface UniverCellDoc {
  id: string;
  body: {
    dataStream: string;
    textRuns?: UniverTextRun[];
    paragraphs?: { startIndex: number }[];
    sectionBreaks?: { startIndex: number }[];
    customRanges?: UniverCustomRange[];
  };
  documentStyle: Record<string, unknown>;
}

export interface UniverCell {
  /** The value: a number, a string, or 1 / 0 for booleans. */
  v?: number | string | null;
  /** CellValueType: 1 string, 2 number, 3 boolean, 4 forced string. */
  t?: 1 | 2 | 3 | 4;
  /** A style id into the workbook's styles table. */
  s?: string | null;
  /** The formula, with its leading "=". */
  f?: string | null;
  /** The shared-formula id this cell belongs to. */
  si?: string | null;
  /** FormulaType: 0 normal, 1 shared, 2 array. */
  ft?: number | null;
  /** The range an array formula fills, as A1. */
  ref?: string | null;
  /** Rich text (and hyperlinks), as a document. */
  p?: UniverCellDoc | null;
  /** Data of our own that moves with the cell (a comment thread's id, sheet-comments.ts). */
  custom?: Record<string, unknown>;
}

export interface UniverSheet {
  id: string;
  name: string;
  tabColor: string;
  hidden: 0 | 1 | 2;
  freeze: { xSplit: number; ySplit: number; startRow: number; startColumn: number };
  rowCount: number;
  columnCount: number;
  zoomRatio: number;
  scrollTop: number;
  scrollLeft: number;
  defaultColumnWidth: number;
  defaultRowHeight: number;
  mergeData: UniverRange[];
  cellData: Record<number, Record<number, UniverCell>>;
  rowData: Record<number, { h?: number; hd?: 0 | 1; ah?: number; ia?: 0 | 1; s?: string | null }>;
  columnData: Record<number, { w?: number; hd?: 0 | 1; s?: string | null }>;
  rowHeader: { width: number; hidden?: 0 | 1 };
  columnHeader: { height: number; hidden?: 0 | 1 };
  showGridlines: 0 | 1;
  rightToLeft: 0 | 1;
}

export interface UniverResource {
  name: string;
  data: string;
}

export interface UniverWorkbook {
  id: string;
  name: string;
  appVersion: string;
  locale: string;
  sheetOrder: string[];
  sheets: Record<string, UniverSheet>;
  styles: Record<string, UniverStyle>;
  resources: UniverResource[];
}

// Plugin resources (each stored as JSON in UniverWorkbook.resources under the plugin's name).

export const RESOURCE_DATA_VALIDATION = 'SHEET_DATA_VALIDATION_PLUGIN';
export const RESOURCE_CONDITIONAL_FORMATTING = 'SHEET_CONDITIONAL_FORMATTING_PLUGIN';
export const RESOURCE_NOTES = 'SHEET_NOTE_PLUGIN';
export const RESOURCE_DRAWINGS = 'SHEET_DRAWING_PLUGIN';
export const RESOURCE_FILTER = 'SHEET_FILTER_PLUGIN';

export interface UniverDataValidationRule {
  uid: string;
  type: string;
  ranges: UniverRange[];
  formula1?: string;
  formula2?: string;
  operator?: string;
  allowBlank?: boolean;
  showErrorMessage?: boolean;
  error?: string;
  errorTitle?: string;
  errorStyle?: number;
  showDropDown?: boolean;
  showInputMessage?: boolean;
  prompt?: string;
  promptTitle?: string;
}

export interface UniverCfValue {
  type: 'num' | 'min' | 'max' | 'percent' | 'percentile' | 'formula';
  value?: number | string;
}

export type UniverCfRuleConfig =
  | { type: 'highlightCell'; subType: 'number'; operator: string; value?: number | [number, number]; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'text'; operator: string; value?: string; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'formula'; value: string; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'timePeriod'; operator: string; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'rank'; isBottom: boolean; isPercent: boolean; value: number; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'average'; operator: string; style: UniverStyle }
  | { type: 'highlightCell'; subType: 'uniqueValues' | 'duplicateValues'; style: UniverStyle }
  | { type: 'colorScale'; config: { index: number; color: string; value: UniverCfValue }[] }
  | {
      type: 'dataBar';
      isShowValue: boolean;
      config: { min: UniverCfValue; max: UniverCfValue; isGradient: boolean; positiveColor: string; nativeColor: string };
    }
  | { type: 'iconSet'; isShowValue: boolean; config: { operator: string; value: UniverCfValue; iconType: string; iconId: string }[] };

export interface UniverCfRule {
  cfId: string;
  ranges: UniverRange[];
  stopIfTrue: boolean;
  rule: UniverCfRuleConfig;
}

export interface UniverNote {
  id: string;
  row: number;
  col: number;
  width: number;
  height: number;
  note: string;
  show?: boolean;
}

export interface UniverSheetImage {
  unitId: string;
  subUnitId: string;
  drawingId: string;
  /** DrawingTypeEnum.DRAWING_IMAGE is 0. */
  drawingType: number;
  imageSourceType: 'BASE64' | 'URL';
  source: string;
  transform: { left: number; top: number; width: number; height: number; angle: number };
  sheetTransform: {
    from: { column: number; columnOffset: number; row: number; rowOffset: number };
    to: { column: number; columnOffset: number; row: number; rowOffset: number };
    angle: number;
  };
  /** SheetDrawingAnchorType.Both is "1". */
  anchorType: string;
}

export interface UniverDrawingsForSheet {
  data: Record<string, UniverSheetImage>;
  order: string[];
}

export interface UniverFilter {
  ref: UniverRange;
}

export function readResource<T>(workbook: UniverWorkbook, name: string): T | null {
  const raw = workbook.resources.find(r => r.name === name)?.data;
  if (!raw) return null;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

export function writeResource(workbook: UniverWorkbook, name: string, value: unknown): void {
  const data = JSON.stringify(value);
  const existing = workbook.resources.find(r => r.name === name);
  if (existing) existing.data = data;
  else workbook.resources.push({ name, data });
}

/** Univer's default cell geometry, used to place images and size rows that have no height. */
export const DEFAULT_COLUMN_WIDTH_PX = 88;
export const DEFAULT_ROW_HEIGHT_PX = 24;

/** Excel column widths are in characters of the default font; a close screen conversion. */
export function excelWidthToPx(chars: number): number {
  return Math.round(chars * 7 + 5);
}

export function pxToExcelWidth(px: number): number {
  return Math.max(0, Math.round(((px - 5) / 7) * 100) / 100);
}

/** Row heights are points in Excel and 96-dpi pixels in Univer. */
export function pointsToPx(points: number): number {
  return Math.round((points * 96) / 72);
}

export function pxToPoints(px: number): number {
  return Math.round(((px * 72) / 96) * 100) / 100;
}

import type ExcelJS from 'exceljs';

import { ExcelThemeColors, parseThemeColors, resolveExcelColor } from './xlsx-theme';

/**
 * Cell style mapping between ExcelJS (the OOXML view of a cell) and Univer's IStyleData, both
 * ways. Numbers for enums are Univer's own values (HorizontalAlign, VerticalAlign, WrapStrategy,
 * BorderStyleTypes, TextDecoration, BooleanNumber), written literally so this module has no
 * runtime dependency on Univer.
 */

export interface UniverColor {
  rgb?: string | null;
}

export interface UniverDecoration {
  s: 0 | 1;
  t?: number;
  cl?: UniverColor;
}

export interface UniverBorderSide {
  s: number;
  cl: UniverColor;
}

export interface UniverStyle {
  ff?: string | null;
  fs?: number;
  it?: 0 | 1;
  bl?: 0 | 1;
  ul?: UniverDecoration;
  st?: UniverDecoration;
  cl?: UniverColor | null;
  bg?: UniverColor | null;
  va?: number | null;
  n?: { pattern: string } | null;
  bd?: {
    t?: UniverBorderSide | null;
    r?: UniverBorderSide | null;
    b?: UniverBorderSide | null;
    l?: UniverBorderSide | null;
    tl_br?: UniverBorderSide | null;
    bl_tr?: UniverBorderSide | null;
  } | null;
  ht?: number | null;
  vt?: number | null;
  tb?: number | null;
  tr?: { a: number; v?: 0 | 1 } | null;
  pd?: { l?: number } | null;
}

export const HALIGN = { left: 1, center: 2, right: 3, justify: 4, distributed: 6 } as const;
export const VALIGN = { top: 1, middle: 2, bottom: 3 } as const;
export const WRAP = { overflow: 1, clip: 2, wrap: 3 } as const;
export const UNDERLINE_DOUBLE = 10;
export const UNDERLINE_SINGLE = 12;
export const BASELINE = { normal: 0, subscript: 1, superscript: 2 } as const;

const BORDER_TO_UNIVER: Record<string, number> = {
  thin: 1,
  hair: 2,
  dotted: 3,
  dashed: 4,
  dashDot: 5,
  dashDotDot: 6,
  double: 7,
  medium: 8,
  mediumDashed: 9,
  mediumDashDot: 10,
  mediumDashDotDot: 11,
  slantDashDot: 12,
  thick: 13,
};
const BORDER_FROM_UNIVER: Record<number, ExcelJS.BorderStyle> = Object.fromEntries(
  Object.entries(BORDER_TO_UNIVER).map(([k, v]) => [v, k as ExcelJS.BorderStyle]),
);

/** The Office defaults, for callers that have no workbook theme to hand. */
export const DEFAULT_THEME: ExcelThemeColors = parseThemeColors(null);

/** "#RRGGBB" for an ExcelJS colour: literal ARGB, a theme reference with its tint, or an indexed colour. */
export function colorFromArgb(color: Partial<ExcelJS.Color> | undefined, theme: ExcelThemeColors = DEFAULT_THEME): UniverColor | null {
  const rgb = resolveExcelColor(color as { argb?: string; theme?: number; tint?: number; indexed?: number } | undefined, theme);
  return rgb ? { rgb } : null;
}

export function colorToArgb(color: UniverColor | null | undefined): Partial<ExcelJS.Color> | undefined {
  const rgb = color?.rgb;
  if (!rgb) return undefined;
  const hex = rgb.replace('#', '').toUpperCase();
  if (hex.length === 6) return { argb: `FF${hex}` };
  if (hex.length === 8) return { argb: hex };
  return undefined;
}

export function styleFromExcel(style: Partial<ExcelJS.Style> | undefined, theme: ExcelThemeColors = DEFAULT_THEME): UniverStyle | null {
  if (!style) return null;
  const out: UniverStyle = {};
  const font = style.font;
  if (font) {
    if (font.name) out.ff = font.name;
    if (font.size) out.fs = font.size;
    if (font.bold) out.bl = 1;
    if (font.italic) out.it = 1;
    if (font.underline)
      out.ul = { s: 1, t: font.underline === 'double' || font.underline === 'doubleAccounting' ? UNDERLINE_DOUBLE : UNDERLINE_SINGLE };
    if (font.strike) out.st = { s: 1 };
    const cl = colorFromArgb(font.color, theme);
    if (cl) out.cl = cl;
    if (font.vertAlign === 'superscript') out.va = BASELINE.superscript;
    if (font.vertAlign === 'subscript') out.va = BASELINE.subscript;
  }
  const fill = style.fill;
  if (fill && fill.type === 'pattern' && fill.pattern !== 'none') {
    const bg = colorFromArgb(fill.fgColor, theme) ?? colorFromArgb(fill.bgColor, theme);
    if (bg) out.bg = bg;
  }
  if (style.numFmt) out.n = { pattern: style.numFmt };
  const al = style.alignment;
  if (al) {
    if (al.horizontal && al.horizontal in HALIGN) out.ht = HALIGN[al.horizontal as keyof typeof HALIGN];
    if (al.vertical && al.vertical in VALIGN) out.vt = VALIGN[al.vertical as keyof typeof VALIGN];
    if (al.wrapText) out.tb = WRAP.wrap;
    if (typeof al.textRotation === 'number' && al.textRotation !== 0) out.tr = { a: al.textRotation };
    if (al.textRotation === 'vertical') out.tr = { a: 0, v: 1 };
    if (al.indent) out.pd = { l: al.indent * 8 };
  }
  const bd = style.border;
  if (bd) {
    const side = (b: Partial<ExcelJS.Border> | undefined): UniverBorderSide | null => {
      if (!b?.style) return null;
      return { s: BORDER_TO_UNIVER[b.style] ?? 1, cl: colorFromArgb(b.color, theme) ?? { rgb: '#000000' } };
    };
    const t = side(bd.top),
      r = side(bd.right),
      b = side(bd.bottom),
      l = side(bd.left);
    const diag = bd.diagonal;
    const tl_br = diag?.down ? side(diag) : null;
    const bl_tr = diag?.up ? side(diag) : null;
    if (t || r || b || l || tl_br || bl_tr) out.bd = { t, r, b, l, tl_br, bl_tr };
  }
  return Object.keys(out).length ? out : null;
}

export function styleToExcel(style: UniverStyle | null | undefined): Partial<ExcelJS.Style> {
  const out: Partial<ExcelJS.Style> = {};
  if (!style) return out;
  const font: Partial<ExcelJS.Font> = {};
  if (style.ff) font.name = style.ff;
  if (style.fs) font.size = style.fs;
  if (style.bl) font.bold = true;
  if (style.it) font.italic = true;
  if (style.ul?.s) font.underline = style.ul.t === UNDERLINE_DOUBLE ? 'double' : true;
  if (style.st?.s) font.strike = true;
  const cl = colorToArgb(style.cl);
  if (cl) font.color = cl;
  if (style.va === BASELINE.superscript) font.vertAlign = 'superscript';
  if (style.va === BASELINE.subscript) font.vertAlign = 'subscript';
  if (Object.keys(font).length) out.font = font;
  const bg = colorToArgb(style.bg);
  if (bg) out.fill = { type: 'pattern', pattern: 'solid', fgColor: bg };
  if (style.n?.pattern) out.numFmt = style.n.pattern;
  const alignment: Partial<ExcelJS.Alignment> = {};
  const h = Object.entries(HALIGN).find(([, v]) => v === style.ht)?.[0];
  if (h) alignment.horizontal = h as ExcelJS.Alignment['horizontal'];
  const v = Object.entries(VALIGN).find(([, val]) => val === style.vt)?.[0];
  if (v) alignment.vertical = v as ExcelJS.Alignment['vertical'];
  if (style.tb === WRAP.wrap) alignment.wrapText = true;
  if (style.tr?.v) alignment.textRotation = 'vertical';
  else if (style.tr?.a) alignment.textRotation = style.tr.a;
  if (style.pd?.l) alignment.indent = Math.round(style.pd.l / 8);
  if (Object.keys(alignment).length) out.alignment = alignment;
  if (style.bd) {
    const side = (s: UniverBorderSide | null | undefined): Partial<ExcelJS.Border> | undefined =>
      s ? { style: BORDER_FROM_UNIVER[s.s] ?? 'thin', color: colorToArgb(s.cl) ?? { argb: 'FF000000' } } : undefined;
    const border: Partial<ExcelJS.Borders> = {};
    const t = side(style.bd.t),
      r = side(style.bd.r),
      b = side(style.bd.b),
      l = side(style.bd.l);
    if (t) border.top = t;
    if (r) border.right = r;
    if (b) border.bottom = b;
    if (l) border.left = l;
    const diag = side(style.bd.tl_br) ?? side(style.bd.bl_tr);
    if (diag) border.diagonal = { ...diag, down: !!style.bd.tl_br, up: !!style.bd.bl_tr };
    if (Object.keys(border).length) out.border = border;
  }
  return out;
}

/** A stable key for a style, so identical styles share one entry in the workbook's style table. */
export function styleKey(style: UniverStyle): string {
  return canonical(style);
}

function canonical(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'undefined';
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(',')}}`;
}

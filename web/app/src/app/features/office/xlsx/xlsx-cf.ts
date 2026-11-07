import type ExcelJS from 'exceljs';

import { cellA1, parseSqref, rangeA1 } from '../shared/a1';
import { escapeXml } from '../shared/ooxml';
import { FidelityCollector } from '../shared/fidelity';
import { UniverCfRule, UniverCfRuleConfig, UniverCfValue, UniverRange } from './univer-sheet-types';
import { UniverStyle, styleFromExcel, styleToExcel } from './xlsx-styles';
import { ExcelColorRef, ExcelThemeColors, resolveExcelColor } from './xlsx-theme';

/**
 * Conditional formatting read from and written to SpreadsheetML directly (ADR 0x043): ExcelJS's
 * writer has no priorities beyond what it is told, drops several rule types, and cannot say what
 * the x14 extension says. Regular rules and x14 rules merge into one list in priority order; index
 * 0 is the highest priority in Univer as well as in Excel's rule manager. What Univer has no field
 * for rides beside the rule, keyed by its id (CfExtras), so an edit in Univer's panel keeps it.
 */

export const X14_NS = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
export const XM_NS = 'http://schemas.microsoft.com/office/excel/2006/main';
export const CF_EXT_URI = '{78C0D931-6437-407d-A8EE-F0AAD7539E65}';
const DATABAR_ID_EXT_URI = '{B025F937-C7B1-47D3-B67F-A62EFF666E3E}';

/** Icon sets the regular schema does not know, which only the x14 extension can carry. */
const X14_ONLY_ICON_SETS = new Set(['3Stars', '3Triangles', '5Boxes']);
const EMPTY_ICON_TYPE = 'EMPTY_ICON_TYPE';
const NO_ICONS = 'NoIcons';
const CF_NUMBER_OPERATORS = new Set(['equal', 'notEqual', 'greaterThan', 'greaterThanOrEqual', 'lessThan', 'lessThanOrEqual', 'between', 'notBetween']);
const COMPARE_SYMBOL: Record<string, string> = {
  equal: '=',
  notEqual: '<>',
  greaterThan: '>',
  greaterThanOrEqual: '>=',
  lessThan: '<',
  lessThanOrEqual: '<=',
};
const TEXT_TYPES: Record<string, string> = {
  containsText: 'containsText',
  notContainsText: 'notContainsText',
  beginsWith: 'beginsWith',
  endsWith: 'endsWith',
  containsBlanks: 'containsBlanks',
  notContainsBlanks: 'notContainsBlanks',
  containsErrors: 'containsErrors',
  notContainsErrors: 'notContainsErrors',
};
const TEXT_XML_OPERATOR: Record<string, string> = { containsText: 'containsText', notContainsText: 'notContains', beginsWith: 'beginsWith', endsWith: 'endsWith' };

/** Data bar settings Univer has no field for. */
export interface CfDataBarExtras {
  attrs: Record<string, string>;
  /** Colour child elements, "#RRGGBB". */
  borderColor?: string;
  negativeBorderColor?: string;
  axisColor?: string;
  autoMin?: boolean;
  autoMax?: boolean;
}

export interface CfExtras {
  dataBar?: CfDataBarExtras;
  /** The rule belongs to a pivot table's area. */
  pivot?: boolean;
}

export interface ParsedConditionalFormatting {
  rules: UniverCfRule[];
  extras: Record<string, CfExtras>;
}

// --- reading -------------------------------------------------------------------------------------

function kids(parent: Element | null | undefined, localName: string): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter((n): n is Element => n.nodeType === 1 && (n as Element).localName === localName);
}

function kid(parent: Element | null | undefined, localName: string): Element | null {
  return kids(parent, localName)[0] ?? null;
}

function isOn(el: Element, name: string, fallback = false): boolean {
  const value = el.getAttribute(name);
  if (value === null) return fallback;
  return value === '1' || value === 'true';
}

function readColorRef(el: Element | null): ExcelColorRef | undefined {
  if (!el) return undefined;
  const out: Record<string, unknown> = {};
  const rgb = el.getAttribute('rgb');
  if (rgb) out['argb'] = rgb.length === 6 ? `FF${rgb}` : rgb;
  const theme = el.getAttribute('theme');
  if (theme !== null) out['theme'] = Number(theme);
  const indexed = el.getAttribute('indexed');
  if (indexed !== null) out['indexed'] = Number(indexed);
  const tint = el.getAttribute('tint');
  if (tint !== null) out['tint'] = Number(tint);
  return Object.keys(out).length ? (out as ExcelColorRef) : undefined;
}

function readColor(el: Element | null, theme: ExcelThemeColors): string | undefined {
  const ref = readColorRef(el);
  const hex = ref ? resolveExcelColor(ref, theme) : undefined;
  return hex ?? undefined;
}

/** A dxf as the shape styleFromExcel takes, so one path handles colours, borders and fonts. */
export function readDxf(dxf: Element | null, theme: ExcelThemeColors): UniverStyle {
  if (!dxf) return {};
  const style: Partial<ExcelJS.Style> = {};
  const font = kid(dxf, 'font');
  if (font) {
    const f: Record<string, unknown> = {};
    const on = (name: string) => {
      const el = kid(font, name);
      return !!el && el.getAttribute('val') !== '0' && el.getAttribute('val') !== 'false';
    };
    if (on('b')) f['bold'] = true;
    if (on('i')) f['italic'] = true;
    if (on('strike')) f['strike'] = true;
    const u = kid(font, 'u');
    if (u && u.getAttribute('val') !== 'none') f['underline'] = u.getAttribute('val') ?? 'single';
    const color = readColorRef(kid(font, 'color'));
    if (color) f['color'] = color;
    style.font = f as unknown as ExcelJS.Font;
  }
  const pattern = kid(kid(dxf, 'fill'), 'patternFill');
  if (pattern && pattern.getAttribute('patternType') !== 'none') {
    // In a dxf a solid fill's colour is its bgColor, the opposite of a cell's fill.
    const color = readColorRef(kid(pattern, 'bgColor')) ?? readColorRef(kid(pattern, 'fgColor'));
    if (color) style.fill = { type: 'pattern', pattern: 'solid', fgColor: color } as ExcelJS.Fill;
  }
  const numFmt = kid(dxf, 'numFmt')?.getAttribute('formatCode');
  if (numFmt) style.numFmt = numFmt;
  const border = kid(dxf, 'border');
  if (border) {
    const b: Record<string, unknown> = {};
    for (const side of ['left', 'right', 'top', 'bottom']) {
      const el = kid(border, side);
      const s = el?.getAttribute('style');
      if (el && s) b[side] = { style: s, color: readColorRef(kid(el, 'color')) };
    }
    if (Object.keys(b).length) style.border = b as unknown as ExcelJS.Borders;
  }
  return styleFromExcel(style, theme) ?? {};
}

/** A rule's formulas: <formula> in the regular schema, <xm:f> in x14. */
function formulas(rule: Element): string[] {
  return [...kids(rule, 'formula'), ...kids(rule, 'f')].map(el => el.textContent ?? '');
}

function readCfvo(el: Element): { value: UniverCfValue; auto?: 'min' | 'max'; gte: boolean } {
  const type = el.getAttribute('type') ?? 'num';
  const raw = el.getAttribute('val') ?? kid(el, 'f')?.textContent ?? undefined;
  const gte = isOn(el, 'gte', true);
  if (type === 'autoMin') return { value: { type: 'min' }, auto: 'min', gte };
  if (type === 'autoMax') return { value: { type: 'max' }, auto: 'max', gte };
  if (type === 'min' || type === 'max') return { value: { type }, gte };
  if (type === 'formula') return { value: { type: 'formula', value: `=${String(raw ?? '').replace(/^=/, '')}` }, gte };
  const numeric = raw === undefined ? 0 : Number(raw);
  if (Number.isNaN(numeric)) return { value: { type: 'formula', value: `=${raw}` }, gte };
  if (type === 'percent' || type === 'percentile') return { value: { type, value: numeric }, gte };
  return { value: { type: 'num', value: numeric }, gte };
}

function parseIconSet(el: Element): UniverCfRuleConfig | null {
  const set = el.getAttribute('iconSet') ?? '3TrafficLights1';
  const cfvos = kids(el, 'cfvo').map(readCfvo);
  const n = cfvos.length;
  if (!n) return null;
  const reverse = isOn(el, 'reverse');
  const custom = kids(el, 'cfIcon');
  // Excel lists thresholds from the lowest icon up; Univer from the top down, with the last a catch-all.
  const config = cfvos
    .map((_, k) => {
      const i = n - 1 - k;
      const threshold = k < n - 1 ? cfvos[i] : cfvos[1] ?? cfvos[i];
      const operator = k < n - 1 ? (threshold.gte ? 'greaterThanOrEqual' : 'greaterThan') : threshold.gte ? 'lessThan' : 'lessThanOrEqual';
      const icon = custom[i];
      if (icon) {
        const iconSet = icon.getAttribute('iconSet') ?? set;
        const count = Number(/^\d/.test(iconSet) ? iconSet[0] : n);
        const excelId = Number(icon.getAttribute('iconId') ?? 0);
        return iconSet === NO_ICONS
          ? { operator, value: threshold.value, iconType: EMPTY_ICON_TYPE, iconId: '0' }
          : { operator, value: threshold.value, iconType: iconSet, iconId: String(count - 1 - excelId) };
      }
      return { operator, value: threshold.value, iconType: set, iconId: String(reverse ? n - 1 - k : k) };
    });
  return { type: 'iconSet', isShowValue: isOn(el, 'showValue', true), config };
}

function parseColorScale(el: Element, theme: ExcelThemeColors): UniverCfRuleConfig {
  const colors = kids(el, 'color');
  return {
    type: 'colorScale',
    config: kids(el, 'cfvo').map((c, index) => ({ index, color: readColor(colors[index] ?? null, theme) ?? '#FFFFFF', value: readCfvo(c).value })),
  };
}

/** The text literal of a cellIs formula, when it is one. */
function stringLiteral(formula: string | undefined): string | null {
  const m = formula === undefined ? null : /^"((?:[^"]|"")*)"$/.exec(formula.trim());
  return m ? m[1].replace(/""/g, '"') : null;
}

function averageFormula(rule: Element, ranges: UniverRange[]): string {
  const cell = cellA1(ranges[0].startRow, ranges[0].startColumn);
  const area = ranges.map(r => absolute(r)).join(',');
  const above = isOn(rule, 'aboveAverage', true);
  const deviations = Number(rule.getAttribute('stdDev') ?? 0);
  const limit = `AVERAGE(${area})${above ? '+' : '-'}${deviations}*STDEV(${area})`;
  const symbol = (above ? '>' : '<') + (isOn(rule, 'equalAverage') ? '=' : '');
  return `=${cell}${symbol}${limit}`;
}

function absolute(r: UniverRange): string {
  return rangeA1(r).replace(/([A-Z]+)(\d+)/g, '$$$1$$$2');
}

function parseHighlight(rule: Element, ranges: UniverRange[], style: UniverStyle, fidelity: FidelityCollector, where: string): UniverCfRuleConfig | null {
  const type = rule.getAttribute('type') ?? '';
  const fs = formulas(rule);
  const operator = rule.getAttribute('operator') ?? 'equal';
  const cell = cellA1(ranges[0].startRow, ranges[0].startColumn);
  switch (type) {
    case 'cellIs': {
      const text = stringLiteral(fs[0]);
      if (text !== null && (operator === 'equal' || operator === 'notEqual')) return { type: 'highlightCell', subType: 'text', operator, value: text, style };
      const op = CF_NUMBER_OPERATORS.has(operator) ? operator : 'equal';
      const values = fs.map(Number);
      if (values.length && !values.some(Number.isNaN)) {
        if (op === 'between' || op === 'notBetween') return { type: 'highlightCell', subType: 'number', operator: op, value: [values[0], values[1] ?? values[0]], style };
        return { type: 'highlightCell', subType: 'number', operator: op, value: values[0], style };
      }
      // Univer compares with numbers only; a formula rule says the same thing about the same cells.
      const value =
        op === 'between' || op === 'notBetween'
          ? `=${op === 'notBetween' ? 'NOT(' : ''}AND(${cell}>=${fs[0]},${cell}<=${fs[1]})${op === 'notBetween' ? ')' : ''}`
          : `=${cell}${COMPARE_SYMBOL[op]}${fs[0]}`;
      return { type: 'highlightCell', subType: 'formula', value, style };
    }
    case 'expression':
      return { type: 'highlightCell', subType: 'formula', value: `=${(fs[0] ?? '').replace(/^=/, '')}`, style };
    case 'timePeriod':
      return { type: 'highlightCell', subType: 'timePeriod', operator: rule.getAttribute('timePeriod') ?? 'today', style };
    case 'top10':
      return {
        type: 'highlightCell',
        subType: 'rank',
        isBottom: isOn(rule, 'bottom'),
        isPercent: isOn(rule, 'percent'),
        value: Number(rule.getAttribute('rank') ?? 10),
        style,
      };
    case 'aboveAverage': {
      if (rule.getAttribute('stdDev')) return { type: 'highlightCell', subType: 'formula', value: averageFormula(rule, ranges), style };
      const above = isOn(rule, 'aboveAverage', true);
      const equal = isOn(rule, 'equalAverage');
      return { type: 'highlightCell', subType: 'average', operator: above ? (equal ? 'greaterThanOrEqual' : 'greaterThan') : equal ? 'lessThanOrEqual' : 'lessThan', style };
    }
    case 'duplicateValues':
    case 'uniqueValues':
      return { type: 'highlightCell', subType: type, style };
    default:
      if (TEXT_TYPES[type]) {
        // Excel always writes text=""; other writers leave it to the formula.
        const fromFormula = /"((?:[^"]|"")*)"/.exec(fs[0] ?? '')?.[1]?.replace(/""/g, '"');
        const value = /Blanks|Errors/.test(type) ? undefined : (rule.getAttribute('text') ?? fromFormula ?? '');
        return { type: 'highlightCell', subType: 'text', operator: TEXT_TYPES[type], ...(value === undefined ? {} : { value }), style };
      }
      fidelity.drop('Conditional formatting of an unsupported kind', where);
      return null;
  }
}

function colorOf(el: Element | null, theme: ExcelThemeColors): string | undefined {
  return readColor(el, theme);
}

function x14DataBar(rule: Element, theme: ExcelThemeColors): { gradient: boolean; negative?: string; fill?: string; min?: UniverCfValue; max?: UniverCfValue; extras: CfDataBarExtras } | null {
  const bar = kid(rule, 'dataBar');
  if (!bar) return null;
  const cfvos = kids(bar, 'cfvo').map(readCfvo);
  const attrs: Record<string, string> = {};
  for (const name of ['minLength', 'maxLength', 'border', 'negativeBarColorSameAsPositive', 'negativeBarBorderColorSameAsPositive', 'axisPosition', 'direction']) {
    const value = bar.getAttribute(name);
    if (value !== null) attrs[name] = value;
  }
  const extras: CfDataBarExtras = { attrs };
  const border = colorOf(kid(bar, 'borderColor'), theme);
  if (border) extras.borderColor = border;
  const negativeBorder = colorOf(kid(bar, 'negativeBorderColor'), theme);
  if (negativeBorder) extras.negativeBorderColor = negativeBorder;
  const axis = colorOf(kid(bar, 'axisColor'), theme);
  if (axis) extras.axisColor = axis;
  if (cfvos[0]?.auto) extras.autoMin = true;
  if (cfvos[1]?.auto) extras.autoMax = true;
  return {
    gradient: isOn(bar, 'gradient', true),
    negative: colorOf(kid(bar, 'negativeFillColor'), theme),
    fill: colorOf(kid(bar, 'fillColor'), theme),
    min: cfvos[0]?.value,
    max: cfvos[1]?.value,
    extras,
  };
}

interface Ordered {
  priority: number;
  order: number;
  rule: UniverCfRule;
}

/** Every rule of a sheet, regular and x14, in priority order. */
export function parseConditionalFormatting(
  sheet: Element,
  dxfs: Element[],
  theme: ExcelThemeColors,
  fidelity: FidelityCollector,
  where: string,
  idPrefix: string,
): ParsedConditionalFormatting {
  const out: Ordered[] = [];
  const extras: Record<string, CfExtras> = {};
  let n = 0;
  const x14Rules = new Map<string, Element>();
  const x14Blocks: Element[] = [];
  for (const ext of kids(kid(sheet, 'extLst'), 'ext')) {
    for (const block of kids(kid(ext, 'conditionalFormattings'), 'conditionalFormatting')) x14Blocks.push(block);
  }
  for (const block of x14Blocks) for (const rule of kids(block, 'cfRule')) if (rule.getAttribute('id')) x14Rules.set(rule.getAttribute('id')!.toUpperCase(), rule);
  const used = new Set<Element>();

  const add = (rule: Element, ranges: UniverRange[], pivot: boolean, style: UniverStyle) => {
    const cfId = `${idPrefix}-${n}`;
    const type = rule.getAttribute('type') ?? '';
    let config: UniverCfRuleConfig | null = null;
    if (type === 'dataBar') {
      const base = kid(rule, 'dataBar');
      const linkId = kid(kid(kid(rule, 'extLst'), 'ext'), 'id')?.textContent?.trim().toUpperCase();
      const link = linkId ? x14Rules.get(linkId) : undefined;
      if (link) used.add(link);
      const ext = link ? x14DataBar(link, theme) : rule.namespaceURI === X14_NS ? x14DataBar(rule, theme) : null;
      const baseCfvos = kids(base, 'cfvo').map(readCfvo);
      if (ext) extras[cfId] = { ...extras[cfId], dataBar: ext.extras };
      config = {
        type: 'dataBar',
        isShowValue: isOn(base ?? rule, 'showValue', true),
        config: {
          min: ext?.min ?? baseCfvos[0]?.value ?? { type: 'min' },
          max: ext?.max ?? baseCfvos[1]?.value ?? { type: 'max' },
          isGradient: ext?.gradient ?? true,
          positiveColor: readColor(kid(base, 'color'), theme) ?? ext?.fill ?? '#638EC6',
          nativeColor: ext?.negative ?? '#FF0000',
        },
      };
    } else if (type === 'colorScale') {
      const el = kid(rule, 'colorScale');
      if (el) config = parseColorScale(el, theme);
    } else if (type === 'iconSet') {
      const el = kid(rule, 'iconSet');
      if (el) config = parseIconSet(el);
    } else {
      config = parseHighlight(rule, ranges, style, fidelity, where);
    }
    if (!config) return;
    if (pivot) extras[cfId] = { ...extras[cfId], pivot: true };
    n += 1;
    out.push({
      priority: Number(rule.getAttribute('priority') ?? Number.MAX_SAFE_INTEGER),
      order: n,
      rule: { cfId, ranges, stopIfTrue: isOn(rule, 'stopIfTrue'), rule: config },
    });
  };

  for (const block of kids(sheet, 'conditionalFormatting')) {
    const ranges = parseSqref(block.getAttribute('sqref') ?? '');
    if (!ranges.length) continue;
    const pivot = isOn(block, 'pivot');
    for (const rule of kids(block, 'cfRule')) {
      const dxfId = rule.getAttribute('dxfId');
      add(rule, ranges, pivot, readDxf(dxfId === null ? null : (dxfs[Number(dxfId)] ?? null), theme));
    }
  }
  for (const block of x14Blocks) {
    const ranges = parseSqref(kid(block, 'sqref')?.textContent ?? '');
    if (!ranges.length) continue;
    for (const rule of kids(block, 'cfRule')) {
      if (used.has(rule)) continue;
      add(rule, ranges, isOn(block, 'pivot'), readDxf(kid(rule, 'dxf'), theme));
    }
  }
  out.sort((a, b) => a.priority - b.priority || a.order - b.order);
  return { rules: out.map(o => o.rule), extras };
}

// --- writing -------------------------------------------------------------------------------------

/** Collects the dxf elements for styles.xml; an identical style shares one. */
export class DxfTable {
  readonly elements: string[] = [];
  private readonly ids = new Map<string, number>();

  constructor(private readonly base: number) {}

  idFor(style: UniverStyle | null | undefined): number | null {
    const xml = dxfXml(style);
    if (!xml) return null;
    let id = this.ids.get(xml);
    if (id === undefined) {
      id = this.base + this.elements.length;
      this.ids.set(xml, id);
      this.elements.push(xml);
    }
    return id;
  }
}

function colorAttrs(color: Partial<ExcelJS.Color> | undefined): string {
  const argb = color?.argb;
  return argb ? ` rgb="${argb}"` : '';
}

function argbOf(hex: string | undefined, fallback: string): string {
  const clean = (hex ?? fallback).replace('#', '').toUpperCase();
  if (clean.length === 3) return `FF${clean[0]}${clean[0]}${clean[1]}${clean[1]}${clean[2]}${clean[2]}`;
  return clean.length === 6 ? `FF${clean}` : clean;
}

/** The inner form of a dxf: element order follows CT_Dxf and CT_Font as Excel writes them. */
export function dxfXml(style: UniverStyle | null | undefined): string {
  if (!style) return '';
  const excel = styleToExcel(style);
  const parts: string[] = [];
  const font = excel.font;
  if (font && (font.bold || font.italic || font.strike || font.underline || font.color)) {
    const u = font.underline;
    parts.push(
      `<font>${font.bold ? '<b/>' : ''}${font.italic ? '<i/>' : ''}${font.strike ? '<strike/>' : ''}` +
        `${u ? (u === 'double' ? '<u val="double"/>' : '<u/>') : ''}${font.color ? `<color${colorAttrs(font.color)}/>` : ''}</font>`,
    );
  }
  if (excel.numFmt) parts.push(`<numFmt numFmtId="${200 + hash(excel.numFmt)}" formatCode="${escapeXml(excel.numFmt)}"/>`);
  const fill = excel.fill as ExcelJS.FillPattern | undefined;
  if (fill?.fgColor) parts.push(`<fill><patternFill patternType="solid"><fgColor${colorAttrs(fill.fgColor)}/><bgColor${colorAttrs(fill.fgColor)}/></patternFill></fill>`);
  const border = excel.border;
  if (border && (border.left || border.right || border.top || border.bottom)) {
    const side = (name: 'left' | 'right' | 'top' | 'bottom') => {
      const b = border[name];
      return b?.style ? `<${name} style="${b.style}"><color${colorAttrs(b.color) || ' auto="1"'}/></${name}>` : `<${name}/>`;
    };
    parts.push(`<border>${side('left')}${side('right')}${side('top')}${side('bottom')}</border>`);
  }
  return parts.join('');
}

function hash(text: string): number {
  let h = 0;
  for (const ch of text) h = (h * 31 + ch.charCodeAt(0)) % 1000;
  return h;
}

function cfvoAttrs(value: UniverCfValue): { type: string; val?: string } {
  if (value.type === 'min' || value.type === 'max') return { type: value.type };
  const raw = String(value.value ?? 0);
  if (value.type === 'formula') return { type: 'formula', val: raw.replace(/^=/, '') };
  if (raw.startsWith('=')) return { type: 'formula', val: raw.slice(1) };
  return { type: value.type, val: raw };
}

function cfvoXml(value: UniverCfValue, extra = ''): string {
  const { type, val } = cfvoAttrs(value);
  return `<cfvo type="${type}"${val === undefined ? '' : ` val="${escapeXml(val)}"`}${extra}/>`;
}

function x14Cfvo(value: UniverCfValue, auto?: string, extra = ''): string {
  if (auto) return `<x14:cfvo type="${auto}"${extra}/>`;
  const { type, val } = cfvoAttrs(value);
  return val === undefined ? `<x14:cfvo type="${type}"${extra}/>` : `<x14:cfvo type="${type}"${extra}><xm:f>${escapeXml(val)}</xm:f></x14:cfvo>`;
}

function newGuid(): string {
  const random = globalThis.crypto?.randomUUID?.() ?? 'xxxxxxxx-xxxx-4xxx-8xxx-xxxxxxxxxxxx'.replace(/x/g, () => Math.floor(Math.random() * 16).toString(16));
  return `{${random.toUpperCase()}}`;
}

function textFormula(operator: string, value: string, cell: string): string {
  const literal = `"${value.replace(/"/g, '""')}"`;
  switch (operator) {
    case 'containsText':
      return `NOT(ISERROR(SEARCH(${literal},${cell})))`;
    case 'notContainsText':
      return `ISERROR(SEARCH(${literal},${cell}))`;
    case 'beginsWith':
      return `LEFT(${cell},LEN(${literal}))=${literal}`;
    case 'endsWith':
      return `RIGHT(${cell},LEN(${literal}))=${literal}`;
    case 'containsBlanks':
      return `LEN(TRIM(${cell}))=0`;
    case 'notContainsBlanks':
      return `LEN(TRIM(${cell}))>0`;
    case 'containsErrors':
      return `ISERROR(${cell})`;
    case 'notContainsErrors':
      return `NOT(ISERROR(${cell}))`;
    default:
      return literal;
  }
}

function timeFormula(period: string, c: string): string {
  const today = 'TODAY()';
  switch (period) {
    case 'yesterday':
      return `FLOOR(${c},1)=${today}-1`;
    case 'tomorrow':
      return `FLOOR(${c},1)=${today}+1`;
    case 'last7Days':
      return `AND(${today}-FLOOR(${c},1)<=6,FLOOR(${c},1)<=${today})`;
    case 'thisMonth':
      return `AND(MONTH(${c})=MONTH(${today}),YEAR(${c})=YEAR(${today}))`;
    case 'lastMonth':
      return `AND(MONTH(${c})=MONTH(EDATE(${today},0-1)),YEAR(${c})=YEAR(EDATE(${today},0-1)))`;
    case 'nextMonth':
      return `AND(MONTH(${c})=MONTH(EDATE(${today},0+1)),YEAR(${c})=YEAR(EDATE(${today},0+1)))`;
    case 'thisWeek':
      return `AND(${today}-ROUNDDOWN(${c},0)<=WEEKDAY(${today})-1,ROUNDDOWN(${c},0)-${today}<=7-WEEKDAY(${today}))`;
    case 'lastWeek':
      return `AND(${today}-ROUNDDOWN(${c},0)>=(WEEKDAY(${today})),${today}-ROUNDDOWN(${c},0)<(WEEKDAY(${today})+7))`;
    case 'nextWeek':
      return `AND(ROUNDDOWN(${c},0)-${today}>(7-WEEKDAY(${today})),ROUNDDOWN(${c},0)-${today}<(15-WEEKDAY(${today})))`;
    default:
      return `FLOOR(${c},1)=${today}`;
  }
}

function esc(text: string): string {
  return escapeXml(text);
}

/** What a highlight rule becomes: attributes after type, and formulas. */
function highlightParts(rule: Extract<UniverCfRuleConfig, { type: 'highlightCell' }>, ranges: UniverRange[]): { type: string; attrs: string; formulas: string[] } {
  const cell = cellA1(ranges[0].startRow, ranges[0].startColumn);
  switch (rule.subType) {
    case 'number': {
      const values = Array.isArray(rule.value) ? rule.value : [rule.value ?? 0];
      return { type: 'cellIs', attrs: ` operator="${rule.operator}"`, formulas: values.map(String) };
    }
    case 'text': {
      if (rule.operator === 'equal' || rule.operator === 'notEqual') {
        return { type: 'cellIs', attrs: ` operator="${rule.operator}"`, formulas: [`"${(rule.value ?? '').replace(/"/g, '""')}"`] };
      }
      const xmlOperator = TEXT_XML_OPERATOR[rule.operator];
      const attrs = xmlOperator ? ` operator="${xmlOperator}" text="${esc(rule.value ?? '')}"` : '';
      return { type: rule.operator, attrs, formulas: [textFormula(rule.operator, rule.value ?? '', cell)] };
    }
    case 'formula':
      return { type: 'expression', attrs: '', formulas: [rule.value.replace(/^=/, '')] };
    case 'timePeriod':
      return { type: 'timePeriod', attrs: ` timePeriod="${rule.operator}"`, formulas: [timeFormula(rule.operator, cell)] };
    case 'rank':
      return { type: 'top10', attrs: `${rule.isPercent ? ' percent="1"' : ''}${rule.isBottom ? ' bottom="1"' : ''} rank="${rule.value}"`, formulas: [] };
    case 'average': {
      const below = rule.operator === 'lessThan' || rule.operator === 'lessThanOrEqual';
      const equal = rule.operator === 'greaterThanOrEqual' || rule.operator === 'lessThanOrEqual';
      return { type: 'aboveAverage', attrs: `${below ? ' aboveAverage="0"' : ''}${equal ? ' equalAverage="1"' : ''}`, formulas: [] };
    }
    default:
      return { type: rule.subType, attrs: '', formulas: [] };
  }
}

/** Icons as Excel writes them: one named set, reversed or not, or a custom list. */
function iconShape(rule: Extract<UniverCfRuleConfig, { type: 'iconSet' }>): { set: string; reverse: boolean; custom: boolean } {
  const n = rule.config.length;
  const set = rule.config[0]?.iconType ?? '3TrafficLights1';
  const uniform = rule.config.every(c => c.iconType === set) && set !== EMPTY_ICON_TYPE && /^\d/.test(set) && Number(set[0]) === n;
  const forward = uniform && rule.config.every((c, k) => Number(c.iconId) === k);
  const reversed = uniform && rule.config.every((c, k) => Number(c.iconId) === n - 1 - k);
  return { set: set === '_5Felling' ? '5Rating' : set, reverse: reversed && !forward, custom: !forward && !reversed };
}

function ascendingThresholds(rule: Extract<UniverCfRuleConfig, { type: 'iconSet' }>): { value: UniverCfValue; gte: boolean }[] {
  const n = rule.config.length;
  // The lowest icon's threshold means nothing to Excel, which still wants one.
  const out: { value: UniverCfValue; gte: boolean }[] = [{ value: { type: 'percent', value: 0 }, gte: true }];
  for (let i = 1; i < n; i++) {
    const entry = rule.config[n - 1 - i];
    out.push({ value: entry.value, gte: entry.operator !== 'greaterThan' });
  }
  return out;
}

function referencesOtherSheet(cf: UniverCfRule): boolean {
  const r = cf.rule;
  if (r.type === 'highlightCell' && r.subType === 'formula') return r.value.includes('!');
  const values = r.type === 'colorScale' ? r.config.map(c => c.value) : r.type === 'iconSet' ? r.config.map(c => c.value) : r.type === 'dataBar' ? [r.config.min, r.config.max] : [];
  return values.some(v => typeof v.value === 'string' && v.value.includes('!'));
}

export interface ConditionalFormattingXml {
  /** <conditionalFormatting> elements for the worksheet. */
  regular: string;
  /** The extLst <ext> holding x14 rules, or empty. */
  ext: string;
}

/** Rules as XML, priorities from their order. */
export function conditionalFormattingXml(
  rules: readonly UniverCfRule[],
  extras: Record<string, CfExtras> | undefined,
  dxfs: DxfTable,
  fidelity: FidelityCollector,
  where: string,
): ConditionalFormattingXml {
  const regular: string[] = [];
  const x14: string[] = [];
  rules.forEach((cf, index) => {
    if (!cf.ranges.length) return;
    const priority = index + 1;
    const r = cf.rule;
    const sqref = cf.ranges.map(rangeA1).join(' ');
    const stop = cf.stopIfTrue ? ' stopIfTrue="1"' : '';
    const pivot = extras?.[cf.cfId]?.pivot ? ' pivot="1"' : '';
    const wrap = (inner: string) => regular.push(`<conditionalFormatting${pivot} sqref="${sqref}">${inner}</conditionalFormatting>`);
    const wrapX14 = (inner: string) =>
      x14.push(`<x14:conditionalFormatting xmlns:xm="${XM_NS}"${pivot}>${inner}<xm:sqref>${sqref}</xm:sqref></x14:conditionalFormatting>`);
    const crossSheet = referencesOtherSheet(cf);
    switch (r.type) {
      case 'highlightCell': {
        const parts = highlightParts(r, cf.ranges);
        if (crossSheet) {
          const dxf = dxfXml(r.style);
          wrapX14(
            `<x14:cfRule type="${parts.type}" priority="${priority}"${stop}${parts.attrs} id="${newGuid()}">${parts.formulas.map(f => `<xm:f>${esc(f)}</xm:f>`).join('')}` +
              `${dxf ? `<x14:dxf>${dxf}</x14:dxf>` : ''}</x14:cfRule>`,
          );
          return;
        }
        const dxfId = dxfs.idFor(r.style);
        wrap(
          `<cfRule type="${parts.type}"${dxfId === null ? '' : ` dxfId="${dxfId}"`} priority="${priority}"${stop}${parts.attrs}>` +
            `${parts.formulas.map(f => `<formula>${esc(f)}</formula>`).join('')}</cfRule>`,
        );
        return;
      }
      case 'colorScale': {
        const config = [...r.config].sort((a, b) => a.index - b.index);
        wrap(
          `<cfRule type="colorScale" priority="${priority}"${stop}><colorScale>${config.map(c => cfvoXml(c.value)).join('')}` +
            `${config.map(c => `<color rgb="${argbOf(c.color, '#FFFFFF')}"/>`).join('')}</colorScale></cfRule>`,
        );
        return;
      }
      case 'dataBar': {
        const id = newGuid();
        const showValue = r.isShowValue === false ? ' showValue="0"' : '';
        wrap(
          `<cfRule type="dataBar" priority="${priority}"${stop}><dataBar${showValue}>${cfvoXml(r.config.min)}${cfvoXml(r.config.max)}<color rgb="${argbOf(r.config.positiveColor, '#638EC6')}"/></dataBar>` +
            `<extLst><ext xmlns:x14="${X14_NS}" uri="${DATABAR_ID_EXT_URI}"><x14:id>${id}</x14:id></ext></extLst></cfRule>`,
        );
        wrapX14(`<x14:cfRule type="dataBar" id="${id}">${x14DataBarXml(r, extras?.[cf.cfId]?.dataBar)}</x14:cfRule>`);
        return;
      }
      case 'iconSet': {
        const { set, reverse, custom } = iconShape(r);
        const thresholds = ascendingThresholds(r);
        const showValue = r.isShowValue === false ? ' showValue="0"' : '';
        if (r.config.some(c => c.iconType === '_5Felling')) fidelity.warn('The five-faces icon set is saved as five-star ratings', where);
        if (custom || X14_ONLY_ICON_SETS.has(set) || crossSheet) {
          const n = r.config.length;
          const icons = custom
            ? thresholds
                .map((_, i) => {
                  const entry = r.config[n - 1 - i];
                  if (entry.iconType === EMPTY_ICON_TYPE) return `<x14:cfIcon iconSet="${NO_ICONS}" iconId="0"/>`;
                  const count = Number(entry.iconType[0]) || n;
                  return `<x14:cfIcon iconSet="${entry.iconType === '_5Felling' ? '5Rating' : entry.iconType}" iconId="${count - 1 - Number(entry.iconId)}"/>`;
                })
                .join('')
            : '';
          wrapX14(
            `<x14:cfRule type="iconSet" priority="${priority}"${stop} id="${newGuid()}"><x14:iconSet iconSet="${set === EMPTY_ICON_TYPE ? `${n}TrafficLights1` : set}"${showValue}${reverse ? ' reverse="1"' : ''}${custom ? ' custom="1"' : ''}>` +
              `${thresholds.map(t => x14Cfvo(t.value, undefined, t.gte ? '' : ' gte="0"')).join('')}${icons}</x14:iconSet></x14:cfRule>`,
          );
          return;
        }
        wrap(
          `<cfRule type="iconSet" priority="${priority}"${stop}><iconSet iconSet="${set}"${showValue}${reverse ? ' reverse="1"' : ''}>` +
            `${thresholds.map(t => cfvoXml(t.value, t.gte ? '' : ' gte="0"')).join('')}</iconSet></cfRule>`,
        );
        return;
      }
    }
  });
  return {
    regular: regular.join(''),
    ext: x14.length ? `<ext xmlns:x14="${X14_NS}" uri="${CF_EXT_URI}"><x14:conditionalFormattings>${x14.join('')}</x14:conditionalFormattings></ext>` : '',
  };
}

function x14DataBarXml(r: Extract<UniverCfRuleConfig, { type: 'dataBar' }>, extras: CfDataBarExtras | undefined): string {
  const attrs = { minLength: '0', maxLength: '100', ...(extras?.attrs ?? {}), gradient: r.config.isGradient ? '1' : '0' };
  const attrXml = Object.entries(attrs)
    .map(([k, v]) => ` ${k}="${esc(v)}"`)
    .join('');
  const autoMin = extras?.autoMin && r.config.min.type === 'min' ? 'autoMin' : undefined;
  const autoMax = extras?.autoMax && r.config.max.type === 'max' ? 'autoMax' : undefined;
  const color = (name: string, hex: string | undefined) => (hex ? `<x14:${name} rgb="${argbOf(hex, '#000000')}"/>` : '');
  return (
    `<x14:dataBar${attrXml}>${x14Cfvo(r.config.min, autoMin)}${x14Cfvo(r.config.max, autoMax)}` +
    `${color('borderColor', extras?.borderColor)}${color('negativeFillColor', r.config.nativeColor)}${color('negativeBorderColor', extras?.negativeBorderColor)}${color('axisColor', extras?.axisColor ?? '#000000')}` +
    `</x14:dataBar>`
  );
}

import { parseSqref, rangeA1 } from '../shared/a1';
import { escapeXml, serializeXml } from '../shared/ooxml';
import { FidelityCollector } from '../shared/fidelity';
import { UniverDataValidationRule } from './univer-sheet-types';
import { X14_NS, XM_NS } from './xlsx-cf';

/**
 * Data validation read from and written to SpreadsheetML directly. A rule goes out as one element
 * over its whole range whatever its size, and a rule whose list or limit lives on another sheet goes
 * in the x14 extension, as Excel writes it.
 */

export const DV_EXT_URI = '{CCE6A557-97BC-4b89-ADB6-D9C93CAAB3DF}';

const ATTR_BOOLS: Record<string, keyof UniverDataValidationRule> = {
  allowBlank: 'allowBlank',
  showErrorMessage: 'showErrorMessage',
  showInputMessage: 'showInputMessage',
};
/** Univer's DataValidationErrorStyle. */
const ERROR_STYLE_TO_UNIVER: Record<string, number> = { information: 0, stop: 1, warning: 2 };
const ERROR_STYLE_FROM_UNIVER: Record<number, string> = { 0: 'information', 2: 'warning' };
const TEXT_ATTRS: Record<string, 'error' | 'errorTitle' | 'prompt' | 'promptTitle'> = { error: 'error', errorTitle: 'errorTitle', prompt: 'prompt', promptTitle: 'promptTitle' };
const KNOWN_TYPES = new Set(['list', 'whole', 'decimal', 'date', 'time', 'textLength', 'custom']);

function kids(parent: Element | null | undefined, localName: string): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter((n): n is Element => n.nodeType === 1 && (n as Element).localName === localName);
}

function isOn(value: string | null): boolean {
  return value === '1' || value === 'true';
}

/** A formula as Univer wants it: a bare list for a literal, `=` for a reference or an expression. */
function formulaFromXml(type: string, raw: string | undefined): string | undefined {
  if (raw === undefined || raw === '') return undefined;
  const text = raw.trim();
  if (type === 'list') {
    const literal = /^"([\s\S]*)"$/.exec(text);
    return literal ? literal[1].replace(/""/g, '"') : `=${text.replace(/^=/, '')}`;
  }
  if (type === 'custom') return `=${text.replace(/^=/, '')}`;
  return Number.isNaN(Number(text)) ? `=${text.replace(/^=/, '')}` : text;
}

function formulaToXml(type: string, formula: string | undefined): string | undefined {
  if (formula === undefined || formula === '') return undefined;
  if (formula.startsWith('=')) return formula.slice(1);
  if (type === 'list' || type === 'listMultiple') return `"${formula.replace(/"/g, '""')}"`;
  return formula;
}

function readRule(el: Element, uid: string, formula: (n: 1 | 2) => string | undefined, sqref: string, fidelity: FidelityCollector, where: string): UniverDataValidationRule | null {
  const type = el.getAttribute('type') ?? 'none';
  if (!KNOWN_TYPES.has(type)) {
    fidelity.drop('Data validation of an unsupported kind', where);
    return null;
  }
  const ranges = parseSqref(sqref);
  if (!ranges.length) return null;
  const rule: UniverDataValidationRule = { uid, type, ranges };
  const f1 = formulaFromXml(type, formula(1));
  const f2 = formulaFromXml(type, formula(2));
  if (f1 !== undefined) rule.formula1 = f1;
  if (f2 !== undefined) rule.formula2 = f2;
  const operator = el.getAttribute('operator');
  if (type !== 'list' && type !== 'custom') rule.operator = operator ?? 'between';
  for (const [attr, field] of Object.entries(ATTR_BOOLS)) {
    if (el.hasAttribute(attr)) (rule as unknown as Record<string, unknown>)[field] = isOn(el.getAttribute(attr));
  }
  for (const [attr, field] of Object.entries(TEXT_ATTRS)) {
    const value = el.getAttribute(attr);
    if (value) rule[field] = value;
  }
  rule.errorStyle = ERROR_STYLE_TO_UNIVER[el.getAttribute('errorStyle') ?? 'stop'] ?? 1;
  // Excel's showDropDown="1" means the arrow is hidden.
  if (isOn(el.getAttribute('showDropDown'))) rule.showDropDown = false;
  return rule;
}

export interface ParsedDataValidation {
  rules: UniverDataValidationRule[];
  /** Input-message-only rules (no type) Univer has no model for, written back as they were. */
  rawRegular: string[];
}

export function parseDataValidation(sheet: Element, idPrefix: string, fidelity: FidelityCollector, where: string): ParsedDataValidation {
  const rules: UniverDataValidationRule[] = [];
  const rawRegular: string[] = [];
  let n = 0;
  for (const el of kids(kids(sheet, 'dataValidations')[0], 'dataValidation')) {
    if ((el.getAttribute('type') ?? 'none') === 'none') {
      rawRegular.push(serializeXml(el));
      continue;
    }
    const rule = readRule(el, `${idPrefix}-${n}`, i => kids(el, `formula${i}`)[0]?.textContent ?? undefined, el.getAttribute('sqref') ?? '', fidelity, where);
    if (rule) {
      rules.push(rule);
      n += 1;
    }
  }
  for (const ext of kids(kids(sheet, 'extLst')[0], 'ext')) {
    for (const el of kids(kids(ext, 'dataValidations')[0], 'dataValidation')) {
      const sqref = kids(el, 'sqref')[0]?.textContent ?? '';
      const rule = readRule(el, `${idPrefix}-${n}`, i => kids(kids(el, `formula${i}`)[0], 'f')[0]?.textContent ?? undefined, sqref, fidelity, where);
      if (rule) {
        rules.push(rule);
        n += 1;
      }
    }
  }
  return { rules, rawRegular };
}

function attrs(rule: UniverDataValidationRule, type: string, fidelity: FidelityCollector, where: string): string {
  if (rule.type === 'checkbox') fidelity.warn('Checkboxes are saved as a TRUE or FALSE list', where);
  let out = type === 'none' ? '' : ` type="${type}"`;
  const style = rule.errorStyle === undefined ? undefined : ERROR_STYLE_FROM_UNIVER[rule.errorStyle];
  if (style) out += ` errorStyle="${style}"`;
  if (rule.operator && rule.operator !== 'between' && type !== 'list' && type !== 'custom') out += ` operator="${rule.operator}"`;
  if (rule.allowBlank) out += ' allowBlank="1"';
  if (rule.showDropDown === false) out += ' showDropDown="1"';
  if (rule.showInputMessage) out += ' showInputMessage="1"';
  if (rule.showErrorMessage) out += ' showErrorMessage="1"';
  for (const name of ['errorTitle', 'error', 'promptTitle', 'prompt'] as const) {
    const value = rule[name];
    if (value) out += ` ${name}="${escapeXml(value)}"`;
  }
  return out;
}

const XML_TYPE: Record<string, string> = { listMultiple: 'list', checkbox: 'list', any: 'none' };

export interface DataValidationXml {
  /** The <dataValidations> element, or empty. */
  regular: string;
  /** The extLst <ext> holding rules that reference other sheets, or empty. */
  ext: string;
}

export function dataValidationXml(rules: readonly UniverDataValidationRule[], rawRegular: readonly string[], fidelity: FidelityCollector, where: string): DataValidationXml {
  const regular: string[] = rawRegular.map(xml => xml.replace(/\sxmlns(:\w+)?="[^"]*"/g, ''));
  const x14: string[] = [];
  for (const rule of rules) {
    if (!rule.ranges.length) continue;
    const type = XML_TYPE[rule.type] ?? rule.type;
    const f1 = rule.type === 'checkbox' ? '"TRUE,FALSE"' : formulaToXml(rule.type, rule.formula1);
    const f2 = rule.type === 'checkbox' ? undefined : formulaToXml(rule.type, rule.formula2);
    const sqref = rule.ranges.map(rangeA1).join(' ');
    const crossSheet = [f1, f2].some(f => f !== undefined && !f.startsWith('"') && f.includes('!'));
    if (crossSheet) {
      x14.push(
        `<x14:dataValidation${attrs(rule, type, fidelity, where)}>` +
          `${f1 === undefined ? '' : `<x14:formula1><xm:f>${escapeXml(f1)}</xm:f></x14:formula1>`}` +
          `${f2 === undefined ? '' : `<x14:formula2><xm:f>${escapeXml(f2)}</xm:f></x14:formula2>`}` +
          `<xm:sqref>${sqref}</xm:sqref></x14:dataValidation>`,
      );
    } else {
      regular.push(
        `<dataValidation${attrs(rule, type, fidelity, where)} sqref="${sqref}">` +
          `${f1 === undefined ? '' : `<formula1>${escapeXml(f1)}</formula1>`}${f2 === undefined ? '' : `<formula2>${escapeXml(f2)}</formula2>`}</dataValidation>`,
      );
    }
  }
  return {
    regular: regular.length ? `<dataValidations count="${regular.length}">${regular.join('')}</dataValidations>` : '',
    ext: x14.length
      ? `<ext xmlns:x14="${X14_NS}" uri="${DV_EXT_URI}"><x14:dataValidations xmlns:xm="${XM_NS}" count="${x14.length}">${x14.join('')}</x14:dataValidations></ext>`
      : '',
  };
}

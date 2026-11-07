import { children, escapeXml, parseXml } from '../shared/ooxml';
import { UniverWorkbook, readResource, writeResource } from './univer-sheet-types';

/**
 * Defined names both ways: the workbook part's `<definedNames>` (every name, whether it is a
 * range, a formula or a constant) to Univer's defined-name resource, where they are editable,
 * and back into the workbook part on export. ExcelJS only keeps names that are plain ranges and
 * folds the print area and print titles into the page setup, so those two stay with it.
 */

export const RESOURCE_DEFINED_NAMES = 'SHEET_DEFINED_NAME_PLUGIN';
/** Univer's value for a name scoped to the whole workbook. */
export const DEFINED_NAME_WORKBOOK_SCOPE = 'AllDefaultWorkbook';
/** Names Excel owns and ExcelJS derives from the page setup. */
const PAGE_SETUP_NAMES = new Set(['_xlnm.Print_Area', '_xlnm.Print_Titles']);

export interface XlsxDefinedName {
  name: string;
  /** The text of the element: a reference, a formula without "=", or a constant. */
  value: string;
  /** Zero-based sheet index for a sheet-scoped name. */
  localSheetId?: number;
  hidden?: boolean;
  comment?: string;
}

export interface UniverDefinedName {
  id: string;
  unitId: string;
  name: string;
  /** A reference such as `Sheet1!$A$1:$B$2`, or a formula with a leading "=". */
  formulaOrRefString: string;
  comment?: string;
  hidden?: boolean;
  localSheetId?: string;
}

const SHEET_NAME = `(?:'(?:[^']|'')+'|[A-Za-z0-9_.\\u00A0-\\uFFFF]+)`;
const AREA = `(?:\\$?[A-Za-z]{1,3}\\$?\\d+(?::\\$?[A-Za-z]{1,3}\\$?\\d+)?|\\$?[A-Za-z]{1,3}:\\$?[A-Za-z]{1,3}|\\$?\\d+:\\$?\\d+)`;
const REFERENCE = new RegExp(`^${SHEET_NAME}!${AREA}(?:\\s*,\\s*${SHEET_NAME}!${AREA})*$`);

/** Whether a defined name's text is a reference (possibly a list of areas) rather than a formula or constant. */
export function isReferenceText(value: string): boolean {
  return REFERENCE.test(value.trim());
}

/** Reads `<definedNames>` from the workbook part. */
export function parseDefinedNames(workbookXml: Document): XlsxDefinedName[] {
  const root = workbookXml.documentElement;
  const ns = root.namespaceURI ?? '';
  const out: XlsxDefinedName[] = [];
  for (const el of children(children(root, ns, 'definedNames')[0], ns, 'definedName')) {
    const name = el.getAttribute('name');
    if (!name) continue;
    const entry: XlsxDefinedName = { name, value: (el.textContent ?? '').trim() };
    const local = el.getAttribute('localSheetId');
    if (local !== null && local !== '') entry.localSheetId = Number(local);
    if (el.getAttribute('hidden') === '1' || el.getAttribute('hidden') === 'true') entry.hidden = true;
    const comment = el.getAttribute('comment');
    if (comment) entry.comment = comment;
    out.push(entry);
  }
  return out;
}

/** Writes the names Univer should edit into the workbook's resources; page-setup names are left to ExcelJS. */
export function definedNamesToWorkbook(names: XlsxDefinedName[], workbook: UniverWorkbook): number {
  const map: Record<string, UniverDefinedName> = {};
  let n = 0;
  for (const name of names) {
    if (PAGE_SETUP_NAMES.has(name.name) || !name.value) continue;
    const id = `dn-${++n}`;
    const entry: UniverDefinedName = {
      id,
      unitId: workbook.id,
      name: name.name,
      formulaOrRefString: isReferenceText(name.value) ? name.value : `=${name.value}`,
    };
    if (name.comment) entry.comment = name.comment;
    if (name.hidden) entry.hidden = true;
    const sheetId = name.localSheetId !== undefined ? workbook.sheetOrder[name.localSheetId] : undefined;
    entry.localSheetId = sheetId ?? DEFINED_NAME_WORKBOOK_SCOPE;
    map[id] = entry;
  }
  if (n) writeResource(workbook, RESOURCE_DEFINED_NAMES, map);
  return n;
}

/** The names in the workbook's resources, as the workbook part wants them. */
export function definedNamesFromWorkbook(workbook: UniverWorkbook): XlsxDefinedName[] {
  const map = readResource<Record<string, UniverDefinedName>>(workbook, RESOURCE_DEFINED_NAMES) ?? {};
  const out: XlsxDefinedName[] = [];
  for (const entry of Object.values(map)) {
    if (!entry?.name || !entry.formulaOrRefString) continue;
    const text = entry.formulaOrRefString.trim();
    const item: XlsxDefinedName = { name: entry.name, value: text.startsWith('=') ? text.slice(1) : text };
    if (entry.localSheetId && entry.localSheetId !== DEFINED_NAME_WORKBOOK_SCOPE) {
      const index = workbook.sheetOrder.indexOf(entry.localSheetId);
      if (index >= 0) item.localSheetId = index;
    }
    if (entry.hidden) item.hidden = true;
    if (entry.comment) item.comment = entry.comment;
    out.push(item);
  }
  return out;
}

function definedNameXml(name: XlsxDefinedName): string {
  const attrs = [`name="${escapeXml(name.name)}"`];
  if (name.localSheetId !== undefined) attrs.push(`localSheetId="${name.localSheetId}"`);
  if (name.hidden) attrs.push('hidden="1"');
  if (name.comment) attrs.push(`comment="${escapeXml(name.comment)}"`);
  return `<definedName ${attrs.join(' ')}>${escapeXml(name.value)}</definedName>`;
}

/**
 * Puts `names` into the workbook part, keeping any names already there that ours do not cover
 * (ExcelJS writes the print area and print titles, and the filter database of a sheet).
 */
export function mergeDefinedNamesXml(workbookXml: string, names: XlsxDefinedName[]): string {
  const existing = parseDefinedNames(parseXml(workbookXml));
  const key = (n: XlsxDefinedName) => `${n.name}|${n.localSheetId ?? ''}`;
  const ours = new Set(names.map(key));
  const all = [...names, ...existing.filter(n => !ours.has(key(n)))];
  let xml = workbookXml.replace(/<definedNames\b[^>]*\/>|<definedNames\b[^>]*>[\s\S]*?<\/definedNames>/, '');
  if (!all.length) return xml;
  const fragment = `<definedNames>${all.map(definedNameXml).join('')}</definedNames>`;
  const laterElements = [
    'calcPr',
    'oleSize',
    'customWorkbookViews',
    'pivotCaches',
    'smartTagPr',
    'smartTagTypes',
    'webPublishing',
    'fileRecoveryPr',
    'webPublishObjects',
    'extLst',
  ];
  for (const name of laterElements) {
    const i = xml.search(new RegExp(`<${name}[\\s>/]`));
    if (i !== -1) return xml.slice(0, i) + fragment + xml.slice(i);
  }
  const close = xml.lastIndexOf('</workbook>');
  xml = close === -1 ? xml + fragment : xml.slice(0, close) + fragment + xml.slice(close);
  return xml;
}

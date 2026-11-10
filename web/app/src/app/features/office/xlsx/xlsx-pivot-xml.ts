import { GridValue } from '../sheets/sheet-grid';
import {
  Aggregate,
  BLANK_LABEL,
  PivotAxisItem,
  PivotConfig,
  PivotResult,
  PivotValueField,
  itemLabel,
} from '../sheets/pivots/pivot-engine';
import { RangeRef, parseRangeA1 } from '../shared/a1';
import { NS, attr, children, elementChildren, escapeXml, parseXml, serializeXml } from '../shared/ooxml';

/**
 * Pivot tables and their caches in the file, both ways. Reading gives the editor a configuration by
 * field name. Writing regenerates only what a pivot's layout decides, in the order the schema
 * (CT_pivotTableDefinition) requires, keeps the rest of the part, and marks the cache to refresh on
 * load so Excel rebuilds from the source. A pivot nobody edited is never written.
 */

export const SS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const X14 = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
const X14_CACHE_EXT_URI = '{725AE2AE-9491-48be-B2B4-4EB974FC3084}';
const XML_HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n';

export type PivotSource = { kind: 'range'; sheet: string; range: RangeRef } | { kind: 'table'; name: string } | { kind: 'other' };

export interface PivotCacheField {
  name: string;
  /** Shared item labels, in the file's order: slicers and item lists index into this. */
  items: string[];
  grouped: boolean;
  calculated: boolean;
  numFmtId: string | null;
}

export interface PivotCacheModel {
  path: string;
  source: PivotSource;
  fields: PivotCacheField[];
  /** x14:pivotCacheDefinition's id, which pivot-backed slicers refer to. */
  pivotCacheId: string | null;
}

export interface PivotLocation {
  row: number;
  column: number;
  range: RangeRef;
  /** Report filter rows above the table. */
  pageRows: number;
}

export interface PivotTableModel {
  path: string;
  name: string;
  location: PivotLocation;
  config: PivotConfig;
  /** Why the layout cannot be edited here, in words; empty when it can. */
  limits: string[];
  /** Fields that hold an item list although no axis needs one (a slicer filters them). */
  itemFields: string[];
}

function intAttr(el: Element, name: string): number | undefined {
  const v = el.getAttribute(name);
  return v === null || v === '' ? undefined : Number(v);
}

function sharedLabel(el: Element): string {
  switch (el.localName) {
    case 'm':
      return BLANK_LABEL;
    case 'b':
      return itemLabel(el.getAttribute('v') === '1' || el.getAttribute('v') === 'true');
    default:
      return el.getAttribute('v') ?? '';
  }
}

export function parsePivotCache(xml: string, path: string): PivotCacheModel {
  const root = parseXml(xml).documentElement;
  const sourceEl = children(root, SS, 'cacheSource')[0];
  const ws = children(sourceEl, SS, 'worksheetSource')[0];
  let source: PivotSource = { kind: 'other' };
  if (sourceEl?.getAttribute('type') === 'worksheet' && ws && !attr(ws, NS.r, 'id')) {
    const ref = ws.getAttribute('ref');
    const sheet = ws.getAttribute('sheet');
    const name = ws.getAttribute('name');
    const range = ref ? parseRangeA1(ref) : null;
    if (range && sheet) source = { kind: 'range', sheet, range };
    else if (name) source = { kind: 'table', name };
  }
  const fields = children(children(root, SS, 'cacheFields')[0], SS, 'cacheField').map(f => ({
    name: f.getAttribute('name') ?? '',
    items: elementChildren(children(f, SS, 'sharedItems')[0]).map(sharedLabel),
    grouped: children(f, SS, 'fieldGroup').length > 0,
    calculated: f.getAttribute('databaseField') === '0' || f.getAttribute('formula') !== null,
    numFmtId: f.getAttribute('numFmtId'),
  }));
  const ids = root.getElementsByTagNameNS(X14, 'pivotCacheDefinition');
  return { path, source, fields, pivotCacheId: ids[0]?.getAttribute('pivotCacheId') ?? null };
}

export function parsePivotTable(xml: string, path: string, cache: PivotCacheModel): PivotTableModel {
  const root = parseXml(xml).documentElement;
  const limits: string[] = [];
  if (cache.source.kind === 'other') limits.push('its source is outside this workbook or a data model');
  if (cache.fields.some(f => f.grouped)) limits.push('it uses grouped items');
  if (cache.fields.some(f => f.calculated)) limits.push('it uses calculated fields');
  if (children(root, SS, 'filters').length) limits.push('it uses label or value filters');
  // Formatting kept through a rewrite must not point at fields or items, whose positions change.
  const formats = children(root, SS, 'formats')[0];
  if (
    children(root, SS, 'conditionalFormats').length ||
    children(root, SS, 'chartFormats').length ||
    (formats && formats.getElementsByTagNameNS(SS, 'references').length)
  )
    limits.push('its formatting points at particular items, which a rewrite would move');
  const nameOf = (index: number | undefined) => (index === undefined ? undefined : cache.fields[index]?.name);
  const pivotFields = children(children(root, SS, 'pivotFields')[0], SS, 'pivotField');
  const hidden: Record<string, string[]> = {};
  const itemFields: string[] = [];
  /** The item list's order per field: pageField `item` and axis items index it. */
  const itemsOf = pivotFields.map((pf, i) => {
    const items = children(children(pf, SS, 'items')[0], SS, 'item');
    const field = cache.fields[i];
    if (items.length && !pf.getAttribute('axis') && field) itemFields.push(field.name);
    const labels = items.map(item => {
      const x = intAttr(item, 'x');
      return x === undefined || item.getAttribute('t') ? null : (field?.items[x] ?? null);
    });
    items.forEach((item, position) => {
      const label = labels[position];
      if ((item.getAttribute('h') === '1' || item.getAttribute('h') === 'true') && label !== null && field) (hidden[field.name] ??= []).push(label);
    });
    return labels;
  });
  const axis = (element: string) =>
    children(children(root, SS, element)[0], SS, 'field')
      .map(f => intAttr(f, 'x') ?? -1)
      .filter(x => x >= 0)
      .map(x => nameOf(x))
      .filter((n): n is string => !!n);
  const filters: string[] = [];
  for (const pf of children(children(root, SS, 'pageFields')[0], SS, 'pageField')) {
    const fld = intAttr(pf, 'fld');
    const name = nameOf(fld);
    if (!name || fld === undefined) continue;
    filters.push(name);
    const item = intAttr(pf, 'item');
    if (item !== undefined) {
      const chosen = itemsOf[fld]?.[item];
      if (chosen) hidden[name] = (itemsOf[fld] ?? []).filter((l): l is string => l !== null && l !== chosen);
    }
  }
  const values: PivotValueField[] = children(children(root, SS, 'dataFields')[0], SS, 'dataField').flatMap(d => {
    const field = nameOf(intAttr(d, 'fld'));
    if (!field) return [];
    if (d.getAttribute('showDataAs') && !limits.includes('it shows values as calculations')) limits.push('it shows values as calculations');
    const value: PivotValueField = { field, aggregate: (d.getAttribute('subtotal') as Aggregate | null) ?? 'sum' };
    const name = d.getAttribute('name');
    if (name) value.name = name;
    const fmt = intAttr(d, 'numFmtId');
    if (fmt !== undefined) value.numFmtId = fmt;
    return [value];
  });
  const loc = children(root, SS, 'location')[0];
  const range = parseRangeA1(loc?.getAttribute('ref') ?? 'A1') ?? { startRow: 0, startColumn: 0, endRow: 0, endColumn: 0 };
  return {
    path,
    name: root.getAttribute('name') ?? 'PivotTable',
    location: { row: range.startRow, column: range.startColumn, range, pageRows: filters.length },
    config: {
      rows: axis('rowFields'),
      columns: axis('colFields'),
      values,
      filters,
      hidden,
      rowGrandTotals: root.getAttribute('rowGrandTotals') !== '0',
      colGrandTotals: root.getAttribute('colGrandTotals') !== '0',
    },
    limits,
    itemFields,
  };
}

// --- writing the cache ---------------------------------------------------------------------------

/** A source column as the cache will store it: its labels by shared index, keeping `previous` order. */
export interface CacheColumn {
  name: string;
  items: string[];
  /** The source's values for a label, as typed values for the file. */
  typed: Map<string, GridValue>;
  numericOnly: boolean;
  hasBlank: boolean;
  /** Written as a shared list: everything but plain numbers no axis uses. */
  shared: boolean;
}

export function cacheColumns(table: readonly GridValue[][], previous: readonly PivotCacheField[], axisFields: ReadonlySet<string>): CacheColumn[] {
  const headers = table[0];
  return headers.map((h, c) => {
    const name = String(h);
    const old = previous.find(f => f.name === name);
    const typed = new Map<string, GridValue>();
    let numericOnly = true;
    let hasBlank = false;
    for (let r = 1; r < table.length; r++) {
      const v = table[r][c] ?? null;
      const value = v === '' ? null : v;
      if (value === null) hasBlank = true;
      else if (typeof value !== 'number') numericOnly = false;
      const label = itemLabel(value);
      if (!typed.has(label)) typed.set(label, value);
    }
    // Existing items keep their index, gone or not: slicers and other pivots point at them.
    const items = [...(old?.items ?? [])];
    for (const label of items) {
      if (!typed.has(label)) typed.set(label, label === BLANK_LABEL ? null : numericOnly && !Number.isNaN(Number(label)) ? Number(label) : label);
    }
    for (const label of typed.keys()) if (!items.includes(label)) items.push(label);
    return { name, items, typed, numericOnly, hasBlank, shared: !numericOnly || hasBlank || axisFields.has(name) };
  });
}

function typedItemXml(value: GridValue): string {
  if (value === null) return '<m/>';
  if (typeof value === 'number') return `<n v="${value}"/>`;
  if (typeof value === 'boolean') return `<b v="${value ? 1 : 0}"/>`;
  return `<s v="${escapeXml(value)}"/>`;
}

function cacheFieldXml(column: CacheColumn, numFmtId: string | null): string {
  const numbers = [...column.typed.values()].filter((v): v is number => typeof v === 'number');
  const attrs: string[] = [];
  if (!numbers.length && column.hasBlank) attrs.push('containsBlank="1"');
  const hasText = [...column.typed.values()].some(v => typeof v === 'string' || typeof v === 'boolean');
  if (numbers.length) {
    if (!hasText) attrs.push('containsSemiMixedTypes="0"', 'containsString="0"');
    if (hasText) attrs.push('containsMixedTypes="1"');
    attrs.push('containsNumber="1"');
    if (numbers.every(Number.isInteger)) attrs.push('containsInteger="1"');
    attrs.push(`minValue="${Math.min(...numbers)}"`, `maxValue="${Math.max(...numbers)}"`);
  }
  if (numbers.length && column.hasBlank) attrs.push('containsBlank="1"');
  const open = `sharedItems${attrs.length ? ` ${attrs.join(' ')}` : ''}`;
  const shared = column.shared ? `<${open} count="${column.items.length}">${column.items.map(l => typedItemXml(column.typed.get(l) ?? null)).join('')}</sharedItems>` : `<${open}/>`;
  return `<cacheField name="${escapeXml(column.name)}" numFmtId="${numFmtId ?? 0}">${shared}</cacheField>`;
}

export function pivotRecordsXml(table: readonly GridValue[][], columns: readonly CacheColumn[]): string {
  const rows = table.slice(1).map(row => {
    const cells = columns.map((column, c) => {
      const raw = row[c] ?? null;
      const value = raw === '' ? null : raw;
      return column.shared ? `<x v="${column.items.indexOf(itemLabel(value))}"/>` : typedItemXml(value);
    });
    return `<r>${cells.join('')}</r>`;
  });
  return `${XML_HEAD}<pivotCacheRecords xmlns="${SS}" xmlns:r="${NS.r}" count="${rows.length}">${rows.join('')}</pivotCacheRecords>`;
}

export function newPivotCacheXml(sheet: string, ref: string): string {
  return (
    `${XML_HEAD}<pivotCacheDefinition xmlns="${SS}" xmlns:r="${NS.r}" r:id="rId1" refreshOnLoad="1" refreshedBy="WhatIff" createdVersion="6" refreshedVersion="6" minRefreshableVersion="3" recordCount="0">` +
    `<cacheSource type="worksheet"><worksheetSource ref="${escapeXml(ref)}" sheet="${escapeXml(sheet)}"/></cacheSource><cacheFields count="0"/></pivotCacheDefinition>`
  );
}

/** Replaces the cache's fields, updates the source reference and asks Excel to rebuild on open. */
export function updatePivotCacheXml(
  xml: string,
  columns: readonly CacheColumn[],
  recordCount: number,
  opts: { previous?: readonly PivotCacheField[]; ref?: string; sheet?: string; pivotCacheId?: string } = {},
): string {
  const doc = parseXml(xml);
  const root = doc.documentElement;
  root.setAttribute('refreshOnLoad', '1');
  root.setAttribute('recordCount', String(recordCount));
  root.removeAttribute('saveData');
  const ws = children(children(root, SS, 'cacheSource')[0], SS, 'worksheetSource')[0];
  if (ws && opts.ref && !ws.getAttribute('name')) ws.setAttribute('ref', opts.ref);
  if (ws && opts.sheet && !ws.getAttribute('name')) ws.setAttribute('sheet', opts.sheet);
  const old = children(root, SS, 'cacheFields')[0];
  const fragment = `<cacheFields count="${columns.length}">${columns
    .map(c => cacheFieldXml(c, opts.previous?.find(f => f.name === c.name)?.numFmtId ?? null))
    .join('')}</cacheFields>`;
  const replacement = importFragment(doc, fragment)[0];
  if (old) root.replaceChild(replacement, old);
  else root.appendChild(replacement);
  if (opts.pivotCacheId && !root.getElementsByTagNameNS(X14, 'pivotCacheDefinition').length) {
    const ext = `<ext xmlns:x14="${X14}" uri="${X14_CACHE_EXT_URI}"><x14:pivotCacheDefinition pivotCacheId="${opts.pivotCacheId}"/></ext>`;
    const list = children(root, SS, 'extLst')[0];
    const nodes = importFragment(doc, list ? ext : `<extLst>${ext}</extLst>`);
    (list ?? root).appendChild(nodes[0]);
  }
  return `${XML_HEAD}${serializeXml(root)}`;
}

function importFragment(doc: Document, xml: string): Element[] {
  const wrapper = parseXml(`<w xmlns="${SS}" xmlns:r="${NS.r}">${xml}</w>`).documentElement;
  return elementChildren(wrapper).map(el => doc.importNode(el, true) as Element);
}

// --- writing the pivot table ---------------------------------------------------------------------

export function newPivotTableXml(name: string, cacheId: string): string {
  return (
    `${XML_HEAD}<pivotTableDefinition xmlns="${SS}" name="${escapeXml(name)}" cacheId="${escapeXml(cacheId)}" applyNumberFormats="0" applyBorderFormats="0" applyFontFormats="0" applyPatternFormats="0" applyAlignmentFormats="0" applyWidthHeightFormats="1" dataCaption="Values" updatedVersion="6" minRefreshableVersion="3" useAutoFormatting="1" itemPrintTitles="1" createdVersion="6" indent="0" outline="1" outlineData="1" multipleFieldFilters="0">` +
    `<pivotTableStyleInfo name="PivotStyleLight16" showRowHeaders="1" showColHeaders="1" showRowStripes="0" showColStripes="0" showLastColumn="1"/></pivotTableDefinition>`
  );
}

/** The elements after the layout's, which stay, in schema order. */
const KEPT_AFTER_LAYOUT = new Set(['formats', 'pivotHierarchies', 'pivotTableStyleInfo', 'rowHierarchiesUsage', 'colHierarchiesUsage', 'extLst']);

/** `<x v>` with the default left out, as Excel writes it. */
function xEl(position: number): string {
  return position ? `<x v="${position}"/>` : '<x/>';
}

function axisItemsXml(
  items: readonly PivotAxisItem[],
  fields: readonly string[],
  positions: Record<string, Map<string, number>>,
  valueCount: number,
  hasValueAxis: boolean,
): string {
  let previous: { path: string[]; v: number } | null = null;
  const out = items.map(item => {
    const attrs: string[] = [];
    if (item.kind === 'grand') {
      attrs.push('t="grand"');
      if (hasValueAxis && item.valueIndex) attrs.push(`i="${item.valueIndex}"`);
      previous = null;
      return `<i ${attrs.join(' ')}>${hasValueAxis ? xEl(item.valueIndex) : '<x/>'}</i>`;
    }
    const prev: { path: string[]; v: number } | null = previous;
    let same = 0;
    if (prev && item.kind === 'item') while (same < item.path.length && same < prev.path.length && prev.path[same] === item.path[same]) same++;
    const parts = item.path.slice(same).map((label, i) => xEl(positions[fields[same + i]]?.get(label) ?? 0));
    if (hasValueAxis) parts.push(xEl(item.valueIndex));
    if (item.kind === 'subtotal') attrs.push('t="default"');
    if (same) attrs.push(`r="${same}"`);
    if (hasValueAxis && item.valueIndex) attrs.push(`i="${item.valueIndex}"`);
    previous = { path: item.path, v: item.valueIndex };
    return parts.length ? `<i${attrs.length ? ` ${attrs.join(' ')}` : ''}>${parts.join('')}</i>` : '<i/>';
  });
  void valueCount;
  return out.join('');
}

export interface PivotWriteInput {
  config: PivotConfig;
  result: PivotResult;
  /** The cache's fields, in order. */
  columns: readonly CacheColumn[];
  /** Where the table sits, without the report filters. */
  ref: string;
  /** Fields that need an item list although no axis uses them. */
  itemFields?: readonly string[];
}

export function updatePivotTableXml(xml: string, input: PivotWriteInput): string {
  const { config, result, columns, ref } = input;
  const doc = parseXml(xml);
  const root = doc.documentElement;
  const kept = elementChildren(root).filter(el => KEPT_AFTER_LAYOUT.has(el.localName));
  for (const el of elementChildren(root)) root.removeChild(el);
  for (const name of ['compact', 'compactData', 'dataOnRows', 'dataPosition', 'gridDropZones', 'rowGrandTotals', 'colGrandTotals']) root.removeAttribute(name);
  root.setAttribute('outline', '1');
  root.setAttribute('outlineData', '1');
  if (!config.rowGrandTotals) root.setAttribute('rowGrandTotals', '0');
  if (!config.colGrandTotals) root.setAttribute('colGrandTotals', '0');

  const multi = config.values.length > 1;
  const names = columns.map(c => c.name);
  const positions: Record<string, Map<string, number>> = {};
  const itemIndex = new Map<string, string[]>();
  for (const column of columns) {
    // Items sort as the table does; each points at its shared index.
    const sorted = result.fieldItems[column.name] ?? column.items;
    itemIndex.set(column.name, sorted);
    positions[column.name] = new Map(sorted.map((l, i) => [l, i]));
  }
  const dataFields = new Set(config.values.map(v => v.field));
  const fieldsWithItems = new Set([...config.rows, ...config.columns, ...config.filters, ...Object.entries(config.hidden).filter(([, l]) => l.length).map(([f]) => f), ...(input.itemFields ?? [])]);
  const singlePage = (name: string): string | null => {
    const hidden = new Set(config.hidden[name] ?? []);
    const shown = (itemIndex.get(name) ?? []).filter(l => !hidden.has(l));
    return hidden.size && shown.length === 1 ? shown[0] : null;
  };
  const pivotFields = columns
    .map(column => {
      const attrs: string[] = [];
      if (config.rows.includes(column.name)) attrs.push('axis="axisRow"');
      else if (config.columns.includes(column.name)) attrs.push('axis="axisCol"');
      else if (config.filters.includes(column.name)) attrs.push('axis="axisPage"');
      if (dataFields.has(column.name)) attrs.push('dataField="1"');
      if (config.filters.includes(column.name) && (config.hidden[column.name]?.length ?? 0) > 0 && !singlePage(column.name)) attrs.push('multipleItemSelectionAllowed="1"');
      attrs.push('showAll="0"');
      if (!fieldsWithItems.has(column.name)) return `<pivotField ${attrs.join(' ')}/>`;
      const hidden = new Set(config.filters.includes(column.name) && singlePage(column.name) ? [] : (config.hidden[column.name] ?? []));
      const sorted = itemIndex.get(column.name) ?? [];
      const items = sorted.map(label => `<item ${hidden.has(label) ? 'h="1" ' : ''}x="${column.items.indexOf(label)}"/>`);
      return `<pivotField ${attrs.join(' ')}><items count="${sorted.length + 1}">${items.join('')}<item t="default"/></items></pivotField>`;
    })
    .join('');
  const parts: string[] = [];
  const pageAttrs = config.filters.length ? ` rowPageCount="${config.filters.length}" colPageCount="1"` : '';
  parts.push(`<location ref="${ref}" firstHeaderRow="${result.firstHeaderRow}" firstDataRow="${result.firstDataRow}" firstDataCol="${result.firstDataCol}"${pageAttrs}/>`);
  parts.push(`<pivotFields count="${columns.length}">${pivotFields}</pivotFields>`);
  if (config.rows.length) parts.push(`<rowFields count="${config.rows.length}">${config.rows.map(r => `<field x="${names.indexOf(r)}"/>`).join('')}</rowFields>`);
  const rowCount = result.rowItems.length;
  parts.push(`<rowItems count="${rowCount}">${axisItemsXml(result.rowItems, config.rows, positions, 1, false)}</rowItems>`);
  const colFields = [...config.columns.map(c => names.indexOf(c)), ...(multi ? [-2] : [])];
  if (colFields.length) parts.push(`<colFields count="${colFields.length}">${colFields.map(x => `<field x="${x}"/>`).join('')}</colFields>`);
  parts.push(`<colItems count="${result.colItems.length}">${axisItemsXml(result.colItems, config.columns, positions, config.values.length, multi)}</colItems>`);
  if (config.filters.length) {
    const pages = config.filters.map(f => {
      const chosen = singlePage(f);
      return `<pageField fld="${names.indexOf(f)}"${chosen ? ` item="${positions[f].get(chosen)}"` : ''} hier="-1"/>`;
    });
    parts.push(`<pageFields count="${pages.length}">${pages.join('')}</pageFields>`);
  }
  if (config.values.length) {
    const fields = config.values.map(v => {
      const attrs = [`name="${escapeXml(v.name ?? defaultCaption(v))}"`, `fld="${names.indexOf(v.field)}"`];
      if (v.aggregate !== 'sum') attrs.push(`subtotal="${v.aggregate}"`);
      attrs.push('baseField="0"', 'baseItem="0"');
      if (v.numFmtId !== undefined) attrs.push(`numFmtId="${v.numFmtId}"`);
      return `<dataField ${attrs.join(' ')}/>`;
    });
    parts.push(`<dataFields count="${fields.length}">${fields.join('')}</dataFields>`);
  }
  for (const node of importFragment(doc, parts.join(''))) root.appendChild(node);
  for (const node of kept) root.appendChild(node);
  return `${XML_HEAD}${serializeXml(root)}`;
}

function defaultCaption(v: PivotValueField): string {
  const label: Record<Aggregate, string> = {
    sum: 'Sum',
    count: 'Count',
    average: 'Average',
    max: 'Max',
    min: 'Min',
    product: 'Product',
    countNums: 'Count Numbers',
    stdDev: 'StdDev',
    stdDevp: 'StdDevp',
    var: 'Var',
    varp: 'Varp',
  };
  return `${label[v.aggregate]} of ${v.field}`;
}

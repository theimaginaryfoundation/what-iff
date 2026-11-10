import { NS, OoxmlPackage, Relationship, attr, children, elementChildren, escapeXml, parseXml, relsPathFor, resolveTarget, serializeXml } from '../shared/ooxml';
import { RangeRef, parseRangeA1 } from '../shared/a1';
import { mergeDefinedNamesXml } from './xlsx-defined-names';
import { SS } from './xlsx-pivot-xml';

/**
 * Slicers and timelines in the file (MS-XLSX 2.6). Their parts hang off sheets and the workbook, and
 * the references to them live in extension lists ExcelJS does not write, so the parts go through the
 * preserved path and the references are written here, after it. Pivot-backed slicers keep their state
 * in the slicer cache's items; table-backed ones in the table's autoFilter.
 */

export const X14 = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
export const X15 = 'http://schemas.microsoft.com/office/spreadsheetml/2010/11/main';
const SLE = 'http://schemas.microsoft.com/office/drawing/2010/slicer';
const SLE15 = 'http://schemas.microsoft.com/office/drawing/2012/slicer';
const MC = NS.mc;
const XDR = 'http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing';
const REL_DRAWING = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing';
export const REL_SLICER = 'http://schemas.microsoft.com/office/2007/relationships/slicer';
export const REL_SLICER_CACHE = 'http://schemas.microsoft.com/office/2007/relationships/slicerCache';
export const REL_TIMELINE = 'http://schemas.microsoft.com/office/2011/relationships/timeline';
export const REL_TIMELINE_CACHE = 'http://schemas.microsoft.com/office/2011/relationships/timelineCache';
export const CT_SLICER = 'application/vnd.ms-excel.slicer+xml';
export const CT_SLICER_CACHE = 'application/vnd.ms-excel.slicerCache+xml';
export const CT_TIMELINE = 'application/vnd.ms-excel.timeline+xml';
export const CT_TIMELINE_CACHE = 'application/vnd.ms-excel.timelineCache+xml';
const CT_DRAWING = 'application/vnd.openxmlformats-officedocument.drawing+xml';
const EXT_SHEET_PIVOT_SLICERS = '{A8765BA9-456A-4dab-B4F3-ACF838C121DE}';
const EXT_SHEET_TABLE_SLICERS = '{3A4CF648-6AED-40f4-86FF-DC5316D8AED3}';
const EXT_SHEET_TIMELINES = '{7E03D99C-DC04-49d9-9315-930204A7B6E9}';
const EXT_BOOK_PIVOT_SLICERS = '{BBE1A952-AA13-448e-AADC-164F8A28A991}';
const EXT_BOOK_TABLE_SLICERS = '{46BE6895-7355-4a93-B00E-2C351335B9C9}';
const EXT_BOOK_TIMELINES = '{D0CA8CA8-9F24-4464-BF8E-62219DCF47F9}';
const EXT_TABLE_SLICER_CACHE = '{2F2917AC-EB37-4324-AD4E-5DD8C200BD13}';
const XML_HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n';

export interface SlicerCacheItem {
  /** Index into the pivot cache field's shared items. */
  x: number;
  selected: boolean;
}

export interface SlicerCacheModel {
  path: string;
  name: string;
  sourceName: string;
  kind: 'pivot' | 'table';
  pivotCacheId: string | null;
  pivotTables: { tabId: string; name: string }[];
  items: SlicerCacheItem[];
  tableId: string | null;
  /** The table column's id. */
  column: string | null;
}

export interface SlicerPartEntry {
  name: string;
  cache: string;
  caption: string;
  rowHeight: string;
  columnCount: string | null;
  style: string | null;
}

export interface TableModel {
  path: string;
  id: string;
  name: string;
  range: RangeRef;
  /** Rows of totals at the bottom, which filters and slicers leave out. */
  totalsRows: number;
  headerRows: number;
  columns: { id: string; name: string }[];
  /** Filtered columns by offset in the table: the labels shown. A blank is "(blank)". */
  filters: Record<number, string[]>;
}

export interface AnchorMarker {
  col: number;
  colOff: number;
  row: number;
  rowOff: number;
}

export interface SlicerAnchor {
  from: AnchorMarker;
  to: AnchorMarker;
}

export function parseSlicerCache(xml: string, path: string): SlicerCacheModel {
  const root = parseXml(xml).documentElement;
  const tabular = root.getElementsByTagNameNS(X14, 'tabular')[0];
  const tableCache = root.getElementsByTagNameNS(X15, 'tableSlicerCache')[0];
  return {
    path,
    name: root.getAttribute('name') ?? '',
    sourceName: root.getAttribute('sourceName') ?? '',
    kind: tableCache ? 'table' : 'pivot',
    pivotCacheId: tabular?.getAttribute('pivotCacheId') ?? null,
    pivotTables: Array.from(root.getElementsByTagNameNS(X14, 'pivotTable')).map(p => ({ tabId: p.getAttribute('tabId') ?? '', name: p.getAttribute('name') ?? '' })),
    items: Array.from(root.getElementsByTagNameNS(X14, 'i')).map(i => ({
      x: Number(i.getAttribute('x') ?? 0),
      selected: i.getAttribute('s') === '1' || i.getAttribute('s') === 'true',
    })),
    tableId: tableCache?.getAttribute('tableId') ?? null,
    column: tableCache?.getAttribute('column') ?? null,
  };
}

export function parseSlicerPart(xml: string): SlicerPartEntry[] {
  return Array.from(parseXml(xml).documentElement.getElementsByTagNameNS(X14, 'slicer')).map(s => ({
    name: s.getAttribute('name') ?? '',
    cache: s.getAttribute('cache') ?? '',
    caption: s.getAttribute('caption') ?? s.getAttribute('name') ?? '',
    rowHeight: s.getAttribute('rowHeight') ?? '241300',
    columnCount: s.getAttribute('columnCount'),
    style: s.getAttribute('style'),
  }));
}

export function newSlicerPartXml(entries: readonly SlicerPartEntry[]): string {
  const slicers = entries.map(
    e =>
      `<slicer name="${escapeXml(e.name)}" cache="${escapeXml(e.cache)}" caption="${escapeXml(e.caption)}"${e.columnCount ? ` columnCount="${escapeXml(e.columnCount)}"` : ''}${e.style ? ` style="${escapeXml(e.style)}"` : ''} rowHeight="${escapeXml(e.rowHeight)}"/>`,
  );
  return `${XML_HEAD}<slicers xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}">${slicers.join('')}</slicers>`;
}

/** Items in display order; `s` is left off an unselected item, as Excel writes it. */
function itemsXml(items: readonly SlicerCacheItem[]): string {
  return `<items count="${items.length}">${items.map(i => `<i x="${i.x}"${i.selected ? ' s="1"' : ''}/>`).join('')}</items>`;
}

export function newPivotSlicerCacheXml(name: string, sourceName: string, pivotCacheId: string, pivotTables: readonly { tabId: string; name: string }[], items: readonly SlicerCacheItem[]): string {
  const tables = pivotTables.map(p => `<pivotTable tabId="${escapeXml(p.tabId)}" name="${escapeXml(p.name)}"/>`).join('');
  return (
    `${XML_HEAD}<slicerCacheDefinition xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}" name="${escapeXml(name)}" sourceName="${escapeXml(sourceName)}">` +
    `<pivotTables>${tables}</pivotTables><data><tabular pivotCacheId="${escapeXml(pivotCacheId)}">${itemsXml(items)}</tabular></data></slicerCacheDefinition>`
  );
}

export function newTableSlicerCacheXml(name: string, sourceName: string, tableId: string, column: string): string {
  return (
    `${XML_HEAD}<slicerCacheDefinition xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}" name="${escapeXml(name)}" sourceName="${escapeXml(sourceName)}">` +
    `<extLst><x:ext uri="${EXT_TABLE_SLICER_CACHE}" xmlns:x15="${X15}"><x15:tableSlicerCache tableId="${escapeXml(tableId)}" column="${escapeXml(column)}"/></x:ext></extLst></slicerCacheDefinition>`
  );
}

/** Rewrites a pivot-backed slicer cache's item states and the pivots it drives. */
export function updateSlicerCacheXml(xml: string, items: readonly SlicerCacheItem[]): string {
  const doc = parseXml(xml);
  const tabular = doc.documentElement.getElementsByTagNameNS(X14, 'tabular')[0];
  if (!tabular) return xml;
  for (const old of elementChildren(tabular)) if (old.localName === 'items') tabular.removeChild(old);
  const fragment = parseXml(`<w xmlns="${X14}">${itemsXml(items)}</w>`).documentElement;
  tabular.insertBefore(doc.importNode(elementChildren(fragment)[0], true), tabular.firstChild);
  return `${XML_HEAD}${serializeXml(doc.documentElement)}`;
}

export function parseTable(xml: string, path: string): TableModel {
  const root = parseXml(xml).documentElement;
  const filters: Record<number, string[]> = {};
  const auto = children(root, SS, 'autoFilter')[0];
  for (const column of children(auto, SS, 'filterColumn')) {
    const set = children(column, SS, 'filters')[0];
    if (!set) continue;
    const labels = children(set, SS, 'filter').map(f => f.getAttribute('val') ?? '');
    if (set.getAttribute('blank') === '1') labels.push('(blank)');
    filters[Number(column.getAttribute('colId') ?? 0)] = labels;
  }
  return {
    path,
    id: root.getAttribute('id') ?? '',
    name: root.getAttribute('displayName') ?? root.getAttribute('name') ?? '',
    range: parseRangeA1(root.getAttribute('ref') ?? 'A1') ?? { startRow: 0, startColumn: 0, endRow: 0, endColumn: 0 },
    totalsRows: Number(root.getAttribute('totalsRowCount') ?? 0),
    headerRows: root.getAttribute('headerRowCount') === '0' ? 0 : 1,
    columns: children(children(root, SS, 'tableColumns')[0], SS, 'tableColumn').map(c => ({ id: c.getAttribute('id') ?? '', name: c.getAttribute('name') ?? '' })),
    filters,
  };
}

/** Replaces a table's filters; a table with none yet gets an autoFilter over its data. */
export function updateTableFiltersXml(xml: string, filters: Record<number, string[]>): string {
  const doc = parseXml(xml);
  const root = doc.documentElement;
  const ref = root.getAttribute('ref') ?? '';
  let auto = children(root, SS, 'autoFilter')[0];
  if (!auto) {
    auto = doc.createElementNS(SS, 'autoFilter');
    auto.setAttribute('ref', ref);
    // autoFilter comes before sortState, tableColumns and the rest (CT_Table).
    root.insertBefore(auto, elementChildren(root)[0] ?? null);
  }
  for (const old of children(auto, SS, 'filterColumn')) auto.removeChild(old);
  const columns = Object.keys(filters)
    .map(Number)
    .sort((a, b) => a - b)
    .map(col => {
      const labels = filters[col];
      const blank = labels.includes('(blank)');
      const values = labels.filter(l => l !== '(blank)').map(l => `<filter val="${escapeXml(l)}"/>`);
      return `<filterColumn colId="${col}"><filters${blank ? ' blank="1"' : ''}>${values.join('')}</filters></filterColumn>`;
    })
    .join('');
  if (columns) {
    const wrapped = parseXml(`<w xmlns="${SS}">${columns}</w>`).documentElement;
    const later = elementChildren(auto)[0] ?? null;
    for (const el of elementChildren(wrapped)) auto.insertBefore(doc.importNode(el, true), later);
  }
  return `${XML_HEAD}${serializeXml(root)}`;
}

/** Where each slicer in a drawing sits, by its name. */
export function slicerAnchors(drawingXml: string): Map<string, SlicerAnchor> {
  const out = new Map<string, SlicerAnchor>();
  const root = parseXml(drawingXml).documentElement;
  for (const anchor of elementChildren(root)) {
    const names = [...Array.from(anchor.getElementsByTagNameNS(SLE, 'slicer')), ...Array.from(anchor.getElementsByTagNameNS(SLE15, 'slicer'))];
    const name = names[0]?.getAttribute('name');
    if (!name) continue;
    const marker = (tag: string): AnchorMarker => {
      const el = children(anchor, XDR, tag)[0];
      const n = (k: string) => Number(children(el, XDR, k)[0]?.textContent ?? 0) || 0;
      return { col: n('col'), colOff: n('colOff'), row: n('row'), rowOff: n('rowOff') };
    };
    out.set(name, { from: marker('from'), to: marker('to') });
  }
  return out;
}

/** Moves a slicer's anchor, or removes it with `null`, in a kept drawing; everything else stays as it is. */
export function patchSlicerAnchor(drawingXml: string, name: string, anchor: SlicerAnchor | null): string {
  const doc = parseXml(drawingXml);
  const root = doc.documentElement;
  for (const el of elementChildren(root)) {
    const found = [...Array.from(el.getElementsByTagNameNS(SLE, 'slicer')), ...Array.from(el.getElementsByTagNameNS(SLE15, 'slicer'))].some(s => s.getAttribute('name') === name);
    if (!found) continue;
    if (!anchor) {
      root.removeChild(el);
      break;
    }
    for (const [tag, marker] of [['from', anchor.from], ['to', anchor.to]] as const) {
      const holder = children(el, XDR, tag)[0];
      if (!holder) continue;
      for (const key of ['col', 'colOff', 'row', 'rowOff'] as const) {
        const node = children(holder, XDR, key)[0];
        if (node) node.textContent = String(Math.round(marker[key]));
      }
    }
    break;
  }
  return `${XML_HEAD}${serializeXml(root)}`;
}

function markerXml(tag: string, m: AnchorMarker): string {
  return `<xdr:${tag}><xdr:col>${m.col}</xdr:col><xdr:colOff>${Math.round(m.colOff)}</xdr:colOff><xdr:row>${m.row}</xdr:row><xdr:rowOff>${Math.round(m.rowOff)}</xdr:rowOff></xdr:${tag}>`;
}

/** The frame Excel draws a slicer from, with the fallback shape older readers show. */
export function slicerAnchorXml(name: string, kind: 'pivot' | 'table', anchor: SlicerAnchor, shapeId: number): string {
  const requires = kind === 'table' ? 'sle15' : 'a14';
  const ns =
    kind === 'table'
      ? `xmlns:sle15="${SLE15}"`
      : 'xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"';
  const a = 'http://schemas.openxmlformats.org/drawingml/2006/main';
  return (
    `<xdr:twoCellAnchor xmlns:xdr="${XDR}" xmlns:a="${a}" editAs="oneCell">${markerXml('from', anchor.from)}${markerXml('to', anchor.to)}` +
    `<mc:AlternateContent xmlns:mc="${MC}" ${ns}><mc:Choice Requires="${requires}">` +
    `<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="${shapeId}" name="${escapeXml(name)}"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr>` +
    `<xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="${SLE}"><sle:slicer xmlns:sle="${SLE}" name="${escapeXml(name)}"/></a:graphicData></a:graphic></xdr:graphicFrame>` +
    `</mc:Choice><mc:Fallback xmlns=""><xdr:sp macro="" textlink=""><xdr:nvSpPr><xdr:cNvPr id="0" name=""/><xdr:cNvSpPr><a:spLocks noTextEdit="1"/></xdr:cNvSpPr></xdr:nvSpPr>` +
    `<xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1828800" cy="2524125"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:prstClr val="white"/></a:solidFill><a:ln w="1"><a:solidFill><a:prstClr val="green"/></a:solidFill></a:ln></xdr:spPr>` +
    `<xdr:txBody><a:bodyPr vertOverflow="clip" horzOverflow="clip"/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" sz="1100"/><a:t>This shape represents a slicer. Slicers are supported in Excel 2010 or later.</a:t></a:r></a:p></xdr:txBody></xdr:sp></mc:Fallback></mc:AlternateContent><xdr:clientData/></xdr:twoCellAnchor>`
  );
}

/** What the editor changed about slicers, for the pass that writes references into the package. */
export interface SlicerPackageEdits {
  /** `Slicer_` names to add, as Excel writes them. */
  addNames: string[];
  removeNames: string[];
  /** New frames per sheet name: slicers made here. */
  anchors: Record<string, string[]>;
}

export interface SlicerPreserved {
  /** Slicer parts per sheet name. */
  sheetSlicers: Record<string, string[]>;
  sheetTimelines: Record<string, string[]>;
  slicerCaches: string[];
  timelineCaches: string[];
  /** The original `sheetId` of each sheet name; slicer caches address pivots by it. */
  sheetIds: Record<string, string>;
  edits?: SlicerPackageEdits;
}

export function emptySlicerPreserved(): SlicerPreserved {
  return { sheetSlicers: {}, sheetTimelines: {}, slicerCaches: [], timelineCaches: [], sheetIds: {} };
}

export function hasSlicerParts(p: SlicerPreserved | undefined): boolean {
  return !!p && (p.slicerCaches.length > 0 || p.timelineCaches.length > 0 || Object.keys(p.sheetSlicers).length > 0 || Object.keys(p.sheetTimelines).length > 0 || !!p.edits?.removeNames.length);
}

function relativeTo(fromPart: string, toPart: string): string {
  const fromDir = fromPart.slice(0, fromPart.lastIndexOf('/') + 1).split('/').filter(Boolean);
  const to = toPart.split('/');
  let i = 0;
  while (i < fromDir.length && i < to.length - 1 && fromDir[i] === to[i]) i++;
  return [...Array(fromDir.length - i).fill('..'), ...to.slice(i)].join('/');
}

function allocator(rels: Relationship[]): () => string {
  let next = rels.length + 1;
  return () => {
    while (rels.some(r => r.id === `rId${next}`)) next++;
    return `rId${next++}`;
  };
}

function relsXml(rels: Relationship[]): string {
  const entries = rels
    .map(r => `<Relationship Id="${r.id}" Type="${r.type}" Target="${escapeXml(r.target)}"${r.external ? ' TargetMode="External"' : ''}/>`)
    .join('');
  return `${XML_HEAD}<Relationships xmlns="${NS.rel}">${entries}</Relationships>`;
}

function addExtensions(xml: string, rootName: string, exts: string): string {
  if (!exts) return xml;
  if (/<extLst\b[^>]*\/>/.test(xml)) return xml.replace(/<extLst\b[^>]*\/>/, `<extLst>${exts}</extLst>`);
  if (/<\/extLst>\s*<\/[^>]*$/.test(xml) || xml.includes('</extLst>')) {
    const at = xml.lastIndexOf('</extLst>');
    return `${xml.slice(0, at)}${exts}${xml.slice(at)}`;
  }
  return xml.replace(`</${rootName}>`, `<extLst>${exts}</extLst></${rootName}>`);
}

/** Elements of a worksheet that follow `<drawing>`. */
const AFTER_DRAWING = ['legacyDrawing', 'legacyDrawingHF', 'picture', 'oleObjects', 'controls', 'webPublishItems', 'tableParts', 'extLst'];

function insertDrawing(xml: string, relId: string): string {
  for (const name of AFTER_DRAWING) {
    const i = xml.search(new RegExp(`<${name}[\\s>/]`));
    if (i !== -1) return `${xml.slice(0, i)}<drawing r:id="${relId}"/>${xml.slice(i)}`;
  }
  return xml.replace('</worksheet>', `<drawing r:id="${relId}"/></worksheet>`);
}

/**
 * Runs after the preserved parts are in the package: adds the sheet and workbook relationships and
 * extension lists that point at slicers and timelines, rewrites sheet ids (ExcelJS numbers sheets
 * afresh, and caches address pivots by id), adds and removes `Slicer_` names, and places new frames.
 *
 * The part graph, which ExcelJS knows nothing of, so every edge is written here:
 *
 *   workbook.xml ──rel + x14/x15 ext──▶ slicerCache*.xml, timelineCache*.xml  (+ a Slicer_ name each)
 *   sheet*.xml   ──rel + x14/x15 ext──▶ slicer*.xml, timeline*.xml           (the visible slicers)
 *   sheet*.xml   ──rel──▶ drawing*.xml, which holds each slicer's frame (graphicFrame anchor)
 *   slicerCache  ──tabId + pivotTable name──▶ a pivot on a sheet, or ──tableId──▶ a table
 *
 * Invariants: every relationship id is fresh in its rels part, a sheet's slicer list is split by
 * kind (pivot slicers in the x14 ext, table slicers in the x15 one), and a cache's tabId is the
 * sheet's id after this save.
 */
export async function injectSlicerReferences(bytes: Uint8Array, preserved: SlicerPreserved, saveMime: string): Promise<Uint8Array> {
  if (!hasSlicerParts(preserved) && !Object.keys(preserved.edits?.anchors ?? {}).length) return bytes;
  const pkg = await OoxmlPackage.open(bytes);
  const book = await readWorkbookSheets(pkg);
  const kindOf = await renumberCacheSheetIds(pkg, preserved, book.newIds);
  const contentOverrides: string[] = [];
  const sheetNames = new Set([
    ...Object.keys(preserved.sheetSlicers),
    ...Object.keys(preserved.sheetTimelines),
    ...Object.keys(preserved.edits?.anchors ?? {}),
  ]);
  for (const name of sheetNames) {
    const sheetPath = book.sheetPaths.get(name);
    if (sheetPath) await patchSheet(pkg, preserved, name, sheetPath, kindOf, contentOverrides);
  }
  await patchWorkbook(pkg, preserved, book.wbXml, book.wbRels);
  if (contentOverrides.length) {
    const ct = (await pkg.text('[Content_Types].xml')) ?? '';
    pkg.put('[Content_Types].xml', ct.replace('</Types>', `${contentOverrides.join('')}</Types>`));
  }
  return pkg.save(saveMime);
}

const WORKBOOK_PATH = 'xl/workbook.xml';

/** Each sheet's part path and its id after this save, by name. */
async function readWorkbookSheets(pkg: OoxmlPackage): Promise<{
  wbXml: string;
  wbRels: Relationship[];
  sheetPaths: Map<string, string>;
  newIds: Map<string, string>;
}> {
  const wbXml = (await pkg.text(WORKBOOK_PATH)) ?? '';
  const wb = parseXml(wbXml).documentElement;
  const wbRels = [...(await pkg.rels(WORKBOOK_PATH)).values()];
  const sheetPaths = new Map<string, string>();
  const newIds = new Map<string, string>();
  for (const sheet of children(children(wb, SS, 'sheets')[0], SS, 'sheet')) {
    const name = sheet.getAttribute('name') ?? '';
    const rel = wbRels.find(r => r.id === attr(sheet, NS.r, 'id'));
    if (rel) sheetPaths.set(name, resolveTarget(WORKBOOK_PATH, rel.target));
    newIds.set(name, sheet.getAttribute('sheetId') ?? '');
  }
  return { wbXml, wbRels, sheetPaths, newIds };
}

/** Points each slicer cache's pivots at their sheets' new ids; returns each cache's kind by name. */
async function renumberCacheSheetIds(
  pkg: OoxmlPackage,
  preserved: SlicerPreserved,
  newIds: Map<string, string>,
): Promise<Map<string, 'pivot' | 'table'>> {
  const kindOf = new Map<string, 'pivot' | 'table'>();
  for (const path of preserved.slicerCaches) {
    const xml = await pkg.text(path);
    if (!xml) continue;
    const cache = parseSlicerCache(xml, path);
    kindOf.set(cache.name, cache.kind);
    const fixed = xml.replace(/(<(?:[\w]+:)?pivotTable\b[^>]*\btabId=")([^"]*)(")/g, (whole, a: string, id: string, c: string) => {
      const name = Object.entries(preserved.sheetIds).find(([, old]) => old === id)?.[0];
      const now = name ? newIds.get(name) : undefined;
      return now ? `${a}${now}${c}` : whole;
    });
    if (fixed !== xml) pkg.put(path, fixed);
  }
  return kindOf;
}

/** One sheet: relationships and extension lists for its slicers and timelines, and new frames. */
async function patchSheet(
  pkg: OoxmlPackage,
  preserved: SlicerPreserved,
  name: string,
  sheetPath: string,
  kindOf: Map<string, 'pivot' | 'table'>,
  contentOverrides: string[],
): Promise<void> {
  const rels = [...(await pkg.rels(sheetPath)).values()];
  const nextId = allocator(rels);
  let xml = (await pkg.text(sheetPath)) ?? '';
  const pivotRefs: string[] = [];
  const tableRefs: string[] = [];
  for (const part of preserved.sheetSlicers[name] ?? []) {
    const id = nextId();
    rels.push({ id, type: REL_SLICER, target: relativeTo(sheetPath, part), external: false });
    const partXml = (await pkg.text(part)) ?? '';
    const table = parseSlicerPart(partXml).some(s => kindOf.get(s.cache) === 'table');
    (table ? tableRefs : pivotRefs).push(`<x14:slicer r:id="${id}"/>`);
  }
  const timelineRefs = (preserved.sheetTimelines[name] ?? []).map(part => {
    const id = nextId();
    rels.push({ id, type: REL_TIMELINE, target: relativeTo(sheetPath, part), external: false });
    return `<x15:timelineRef r:id="${id}"/>`;
  });
  let exts = '';
  if (pivotRefs.length) exts += `<ext xmlns:x14="${X14}" uri="${EXT_SHEET_PIVOT_SLICERS}"><x14:slicerList>${pivotRefs.join('')}</x14:slicerList></ext>`;
  if (tableRefs.length) exts += `<ext xmlns:x15="${X15}" uri="${EXT_SHEET_TABLE_SLICERS}"><x14:slicerList xmlns:x14="${X14}">${tableRefs.join('')}</x14:slicerList></ext>`;
  if (timelineRefs.length) exts += `<ext xmlns:x15="${X15}" uri="${EXT_SHEET_TIMELINES}"><x15:timelineRefs>${timelineRefs.join('')}</x15:timelineRefs></ext>`;
  xml = addExtensions(xml, 'worksheet', exts);
  xml = await placeFrames(pkg, sheetPath, xml, rels, nextId, preserved.edits?.anchors[name] ?? [], contentOverrides);
  if (!/xmlns:r=/.test(xml.slice(0, 800))) xml = xml.replace('<worksheet ', `<worksheet xmlns:r="${NS.r}" `);
  pkg.put(sheetPath, xml);
  pkg.put(relsPathFor(sheetPath), relsXml(rels));
}

/** Adds new slicer frames to the sheet's drawing, making one when the sheet has none. */
async function placeFrames(
  pkg: OoxmlPackage,
  sheetPath: string,
  sheetXml: string,
  rels: Relationship[],
  nextId: () => string,
  frames: readonly string[],
  contentOverrides: string[],
): Promise<string> {
  if (!frames.length) return sheetXml;
  const own = rels.find(r => r.type === REL_DRAWING);
  if (own) {
    const drawingPath = resolveTarget(sheetPath, own.target);
    const drawing = (await pkg.text(drawingPath)) ?? '';
    const close = drawing.lastIndexOf('</');
    pkg.put(drawingPath, `${drawing.slice(0, close)}${frames.join('')}${drawing.slice(close)}`);
    return sheetXml;
  }
  let n = 1;
  while (pkg.has(`xl/drawings/drawingS${n}.xml`)) n++;
  const drawingPath = `xl/drawings/drawingS${n}.xml`;
  pkg.put(drawingPath, `${XML_HEAD}<xdr:wsDr xmlns:xdr="${XDR}" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">${frames.join('')}</xdr:wsDr>`);
  contentOverrides.push(`<Override PartName="/${drawingPath}" ContentType="${CT_DRAWING}"/>`);
  const id = nextId();
  rels.push({ id, type: REL_DRAWING, target: relativeTo(sheetPath, drawingPath), external: false });
  return insertDrawing(sheetXml, id);
}

/** The workbook: cache relationships and extension lists, and the `Slicer_` defined names. */
async function patchWorkbook(pkg: OoxmlPackage, preserved: SlicerPreserved, wbXmlIn: string, wbRels: Relationship[]): Promise<void> {
  let wbXml = wbXmlIn;
  const nextWbId = allocator(wbRels);
  const pivotCaches: string[] = [];
  const tableCaches: string[] = [];
  for (const path of preserved.slicerCaches) {
    const xml = await pkg.text(path);
    if (!xml) continue;
    const id = nextWbId();
    wbRels.push({ id, type: REL_SLICER_CACHE, target: relativeTo(WORKBOOK_PATH, path), external: false });
    (parseSlicerCache(xml, path).kind === 'table' ? tableCaches : pivotCaches).push(`<x14:slicerCache r:id="${id}"/>`);
  }
  const timelineCaches = preserved.timelineCaches.map(path => {
    const id = nextWbId();
    wbRels.push({ id, type: REL_TIMELINE_CACHE, target: relativeTo(WORKBOOK_PATH, path), external: false });
    return `<x15:timelineCacheRef r:id="${id}"/>`;
  });
  let bookExts = '';
  if (pivotCaches.length) bookExts += `<ext xmlns:x14="${X14}" uri="${EXT_BOOK_PIVOT_SLICERS}"><x14:slicerCaches>${pivotCaches.join('')}</x14:slicerCaches></ext>`;
  if (tableCaches.length) bookExts += `<ext xmlns:x15="${X15}" uri="${EXT_BOOK_TABLE_SLICERS}"><x15:slicerCaches xmlns:x14="${X14}">${tableCaches.join('')}</x15:slicerCaches></ext>`;
  if (timelineCaches.length) bookExts += `<ext xmlns:x15="${X15}" uri="${EXT_BOOK_TIMELINES}"><x15:timelineCacheRefs>${timelineCaches.join('')}</x15:timelineCacheRefs></ext>`;
  wbXml = addExtensions(wbXml, 'workbook', bookExts);
  for (const name of preserved.edits?.removeNames ?? []) {
    wbXml = wbXml.replace(new RegExp(`<definedName\\b[^>]*\\bname="${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}"[^>]*>[^<]*</definedName>`), '');
  }
  wbXml = wbXml.replace(/<definedNames>\s*<\/definedNames>/, '');
  if (preserved.edits?.addNames.length) wbXml = mergeDefinedNamesXml(wbXml, preserved.edits.addNames.map(name => ({ name, value: '#N/A' })));
  if (!/xmlns:r=/.test(wbXml.slice(0, 800))) wbXml = wbXml.replace('<workbook ', `<workbook xmlns:r="${NS.r}" `);
  pkg.put(WORKBOOK_PATH, wbXml);
  pkg.put(relsPathFor(WORKBOOK_PATH), relsXml(wbRels));
}

import { NS, OoxmlPackage, REL_TYPE, Relationship, attr, children, parseXml, relsPathFor, resolveTarget } from '../shared/ooxml';

/**
 * What ExcelJS cannot carry and the editor cannot edit, kept from the original package and
 * written back into the saved one: charts (with the drawing anchors and media they sit with)
 * and pivot tables (with their caches). The cells a pivot table shows stay ordinary cells, so
 * they survive editing; Excel refreshes the pivot from its cache on open. A sheet whose drawing
 * holds a chart gets its original drawing back whole, so the pictures on that sheet are kept as
 * they were rather than as the editor last placed them.
 */

const XDR = 'http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing';
const CT_CHART = 'application/vnd.openxmlformats-officedocument.drawingml.chart+xml';
const CT_DRAWING = 'application/vnd.openxmlformats-officedocument.drawing+xml';
const CT_PIVOT_TABLE = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotTable+xml';
const CT_PIVOT_CACHE_DEF = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheDefinition+xml';
const CT_PIVOT_CACHE_REC = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheRecords+xml';
const CT_CHART_STYLE = 'application/vnd.ms-office.chartstyle+xml';
const CT_CHART_COLORS = 'application/vnd.ms-office.chartcolorstyle+xml';
const REL_DRAWING = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing';
const REL_CHART = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart';
const REL_PIVOT_TABLE = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotTable';
const REL_PIVOT_CACHE_DEF = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheDefinition';

export interface PreservedPart {
  /** Package path in the original, kept as the key so references can be rewritten. */
  path: string;
  bytes: Uint8Array;
  contentType: string;
}

export interface PreservedChart {
  /** The drawing anchor, zero-based grid cells with EMU offsets. */
  from: { col: number; colOff: number; row: number; rowOff: number };
  to: { col: number; colOff: number; row: number; rowOff: number };
  /** The chart part's path, a key into `parts`. */
  chartPath: string;
  name: string;
}

export interface PreservedSheet {
  /** The sheet's drawing part when it holds at least one chart: path and the charts it anchors. */
  drawing?: { path: string; charts: PreservedChart[] };
  pivotTables: string[];
}

export interface PreservedParts {
  /** Every part that goes back verbatim: drawings, charts and their style parts, media the drawing uses, pivot tables and caches. Keyed by original path. */
  parts: Record<string, PreservedPart>;
  /** Relationship parts for the above, keyed by the part they belong to. */
  rels: Record<string, Relationship[]>;
  bySheet: Record<string, PreservedSheet>;
  /** Pivot cache definitions referenced from the workbook: cacheId to part path. */
  pivotCaches: { cacheId: string; path: string }[];
}

/** Whether there is anything to carry. */
export function hasPreservedParts(p: PreservedParts | null | undefined): boolean {
  return !!p && (Object.values(p.bySheet).some(s => s.drawing || s.pivotTables.length) || p.pivotCaches.length > 0);
}

/** Collects charts and pivot tables from the original package, per sheet name. */
export async function collectPreservedParts(pkg: OoxmlPackage): Promise<PreservedParts> {
  const out: PreservedParts = { parts: {}, rels: {}, bySheet: {}, pivotCaches: [] };
  const contentTypes = await readContentTypes(pkg);
  const wbPath = 'xl/workbook.xml';
  const wb = await pkg.xml(wbPath);
  if (!wb) return out;
  const wbRels = await pkg.rels(wbPath);
  const sheets = children(wb.documentElement, wb.documentElement.namespaceURI ?? '', 'sheets')[0];
  const keep = async (path: string): Promise<boolean> => {
    if (out.parts[path]) return true;
    const bytes = await pkg.bytes(path);
    if (!bytes) return false;
    out.parts[path] = { path, bytes, contentType: contentTypes.get(`/${path}`) ?? contentTypeFor(path) };
    const rels = await pkg.rels(path);
    if (rels.size) out.rels[path] = [...rels.values()];
    return true;
  };
  for (const sheet of children(sheets, wb.documentElement.namespaceURI ?? '', 'sheet')) {
    const name = sheet.getAttribute('name') ?? '';
    const relId = attr(sheet, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    if (!rel) continue;
    const sheetPath = resolveTarget(wbPath, rel.target);
    const sheetRels = await pkg.rels(sheetPath);
    const entry: PreservedSheet = { pivotTables: [] };
    for (const r of sheetRels.values()) {
      if (r.type === REL_DRAWING) {
        const drawingPath = resolveTarget(sheetPath, r.target);
        const drawing = await pkg.xml(drawingPath);
        if (!drawing) continue;
        const drawingRels = await pkg.rels(drawingPath);
        const charts = anchorsWithCharts(drawing, drawingRels, drawingPath);
        if (!charts.length) continue;
        await keep(drawingPath);
        for (const dr of drawingRels.values()) {
          if (dr.external) continue;
          const target = resolveTarget(drawingPath, dr.target);
          await keep(target);
          // Chart style and colour parts hang off the chart.
          for (const cr of (await pkg.rels(target)).values()) if (!cr.external) await keep(resolveTarget(target, cr.target));
        }
        entry.drawing = { path: drawingPath, charts };
      } else if (r.type === REL_PIVOT_TABLE) {
        const pivotPath = resolveTarget(sheetPath, r.target);
        if (!(await keep(pivotPath))) continue;
        entry.pivotTables.push(pivotPath);
        for (const pr of (await pkg.rels(pivotPath)).values()) {
          if (pr.external) continue;
          const def = resolveTarget(pivotPath, pr.target);
          await keep(def);
          for (const rr of (await pkg.rels(def)).values()) if (!rr.external) await keep(resolveTarget(def, rr.target));
        }
      }
    }
    if (entry.drawing || entry.pivotTables.length) out.bySheet[name] = entry;
  }
  const caches = children(wb.documentElement, wb.documentElement.namespaceURI ?? '', 'pivotCaches')[0];
  for (const cache of children(caches, wb.documentElement.namespaceURI ?? '', 'pivotCache')) {
    const relId = attr(cache, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    const cacheId = cache.getAttribute('cacheId');
    if (!rel || !cacheId) continue;
    const path = resolveTarget(wbPath, rel.target);
    if (await keep(path)) {
      for (const rr of (await pkg.rels(path)).values()) if (!rr.external) await keep(resolveTarget(path, rr.target));
      out.pivotCaches.push({ cacheId, path });
    }
  }
  return out;
}

function anchorsWithCharts(drawing: Document, rels: Map<string, Relationship>, drawingPath: string): PreservedChart[] {
  const out: PreservedChart[] = [];
  const C = 'http://schemas.openxmlformats.org/drawingml/2006/chart';
  for (const anchor of Array.from(drawing.documentElement.childNodes).filter((n): n is Element => n.nodeType === 1)) {
    const chartEl = anchor.getElementsByTagNameNS(C, 'chart')[0];
    if (!chartEl) continue;
    const relId = attr(chartEl, NS.r, 'id');
    const rel = relId ? rels.get(relId) : undefined;
    if (!rel) continue;
    const marker = (name: 'from' | 'to') => {
      const el = children(anchor, XDR, name)[0];
      const n = (tag: string) => Number(children(el, XDR, tag)[0]?.textContent ?? 0) || 0;
      return { col: n('col'), colOff: n('colOff'), row: n('row'), rowOff: n('rowOff') };
    };
    const from = children(anchor, XDR, 'from')[0] ? marker('from') : { col: 0, colOff: 0, row: 0, rowOff: 0 };
    let to = children(anchor, XDR, 'to')[0] ? marker('to') : { col: from.col + 8, colOff: 0, row: from.row + 15, rowOff: 0 };
    if (anchor.localName === 'oneCellAnchor') to = { col: from.col + 8, colOff: 0, row: from.row + 15, rowOff: 0 };
    const name = anchor.getElementsByTagNameNS(XDR, 'cNvPr')[0]?.getAttribute('name') ?? 'Chart';
    out.push({ from, to, chartPath: resolveTarget(drawingPath, rel.target), name });
  }
  return out;
}

async function readContentTypes(pkg: OoxmlPackage): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  const doc = await pkg.xml('[Content_Types].xml');
  if (!doc) return out;
  for (const el of Array.from(doc.getElementsByTagNameNS(NS.ct, 'Override'))) {
    const part = el.getAttribute('PartName');
    const type = el.getAttribute('ContentType');
    if (part && type) out.set(part, type);
  }
  return out;
}

function contentTypeFor(path: string): string {
  if (/^xl\/charts\/chart/.test(path)) return CT_CHART;
  if (/^xl\/charts\/style/.test(path)) return CT_CHART_STYLE;
  if (/^xl\/charts\/colors/.test(path)) return CT_CHART_COLORS;
  if (/^xl\/drawings\//.test(path)) return CT_DRAWING;
  if (/^xl\/pivotTables\//.test(path)) return CT_PIVOT_TABLE;
  if (/pivotCacheDefinition/.test(path)) return CT_PIVOT_CACHE_DEF;
  if (/pivotCacheRecords/.test(path)) return CT_PIVOT_CACHE_REC;
  return 'application/xml';
}

/**
 * Writes the preserved parts into a package ExcelJS produced: parts under new names (so nothing
 * ExcelJS wrote is overwritten), relationships rewritten to those names, sheet and workbook
 * references added, content types declared. Sheets are matched by name.
 */
export async function injectPreservedParts(bytes: Uint8Array, preserved: PreservedParts): Promise<Uint8Array> {
  if (!hasPreservedParts(preserved)) return bytes;
  const pkg = await OoxmlPackage.open(bytes);
  const rename = new Map<string, string>();
  for (const path of Object.keys(preserved.parts)) rename.set(path, renamed(path));
  // 1. Parts and their relationship files, with targets rewritten.
  for (const [path, part] of Object.entries(preserved.parts)) {
    const newPath = rename.get(path)!;
    pkg.put(newPath, part.bytes);
    const rels = preserved.rels[path];
    if (rels?.length) {
      const rewritten = rels.map(r => {
        if (r.external) return r;
        const target = resolveTarget(path, r.target);
        const newTarget = rename.get(target);
        return newTarget ? { ...r, target: relative(newPath, newTarget) } : r;
      });
      pkg.put(relsPathFor(newPath), relsXml(rewritten));
    }
  }
  // 2. Sheet references.
  const wbPath = 'xl/workbook.xml';
  const wbXml = (await pkg.text(wbPath)) ?? '';
  const wb = parseXml(wbXml);
  const wbNs = wb.documentElement.namespaceURI ?? '';
  const wbRels = await pkg.rels(wbPath);
  const sheetPathByName = new Map<string, string>();
  for (const sheet of children(children(wb.documentElement, wbNs, 'sheets')[0], wbNs, 'sheet')) {
    const relId = attr(sheet, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    if (rel) sheetPathByName.set(sheet.getAttribute('name') ?? '', resolveTarget(wbPath, rel.target));
  }
  for (const [name, entry] of Object.entries(preserved.bySheet)) {
    const sheetPath = sheetPathByName.get(name);
    if (!sheetPath) continue;
    const rels = [...(await pkg.rels(sheetPath)).values()];
    let sheetXml = (await pkg.text(sheetPath)) ?? '';
    let next = rels.length + 1;
    const nextId = () => {
      while (rels.some(r => r.id === `rId${next}`)) next++;
      return `rId${next++}`;
    };
    if (entry.drawing) {
      // Any drawing ExcelJS wrote for this sheet (pictures) is replaced by the original whole.
      const own = rels.find(r => r.type === REL_DRAWING);
      if (own) {
        rels.splice(rels.indexOf(own), 1);
        sheetXml = sheetXml.replace(/<drawing\b[^>]*\/>/, '');
      }
      const id = nextId();
      rels.push({ id, type: REL_DRAWING, target: relative(sheetPath, rename.get(entry.drawing.path)!), external: false });
      sheetXml = insertBefore(sheetXml, `<drawing r:id="${id}"/>`, [
        'legacyDrawing',
        'legacyDrawingHF',
        'picture',
        'oleObjects',
        'controls',
        'webPublishItems',
        'tableParts',
        'extLst',
      ]);
      if (!/xmlns:r=/.test(sheetXml.slice(0, 600))) sheetXml = sheetXml.replace('<worksheet ', `<worksheet xmlns:r="${NS.r}" `);
    }
    for (const pivotPath of entry.pivotTables) {
      rels.push({ id: nextId(), type: REL_PIVOT_TABLE, target: relative(sheetPath, rename.get(pivotPath)!), external: false });
    }
    pkg.put(sheetPath, sheetXml);
    pkg.put(relsPathFor(sheetPath), relsXml(rels));
  }
  // 3. Workbook pivot caches.
  if (preserved.pivotCaches.length) {
    const rels = [...wbRels.values()];
    let next = rels.length + 1;
    const cachesXml = preserved.pivotCaches
      .map(c => {
        while (rels.some(r => r.id === `rId${next}`)) next++;
        const id = `rId${next++}`;
        rels.push({ id, type: REL_PIVOT_CACHE_DEF, target: relative(wbPath, rename.get(c.path)!), external: false });
        return `<pivotCache cacheId="${c.cacheId}" r:id="${id}"/>`;
      })
      .join('');
    let xml = wbXml.replace(/<pivotCaches>[\s\S]*?<\/pivotCaches>/, '');
    xml = insertBefore(xml, `<pivotCaches>${cachesXml}</pivotCaches>`, [
      'smartTagPr',
      'smartTagTypes',
      'webPublishing',
      'fileRecoveryPr',
      'webPublishObjects',
      'extLst',
    ]);
    pkg.put(wbPath, xml);
    pkg.put(relsPathFor(wbPath), relsXml(rels));
  }
  // 4. Content types.
  let ct = (await pkg.text('[Content_Types].xml')) ?? '';
  const overrides = Object.entries(preserved.parts)
    .map(([path, part]) => `<Override PartName="/${rename.get(path)}" ContentType="${part.contentType}"/>`)
    .join('');
  ct = ct.replace('</Types>', `${overrides}</Types>`);
  pkg.put('[Content_Types].xml', ct);
  return pkg.save('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet');
}

/** "xl/charts/chart1.xml" becomes "xl/charts/chartP1.xml": the same folder, a name ExcelJS never uses; a name already of that form stays. */
function renamed(path: string): string {
  const slash = path.lastIndexOf('/');
  const dir = path.slice(0, slash + 1);
  const name = path.slice(slash + 1);
  const m = /^([A-Za-z]+?)(P?)(\d*)(\.[^.]+)$/.exec(name);
  if (!m) return `${dir}p-${name}`;
  return `${dir}${m[1]}P${m[3] || '1'}${m[4]}`;
}

function relative(fromPart: string, toPart: string): string {
  const fromDir = fromPart
    .slice(0, fromPart.lastIndexOf('/') + 1)
    .split('/')
    .filter(Boolean);
  const to = toPart.split('/');
  let i = 0;
  while (i < fromDir.length && i < to.length - 1 && fromDir[i] === to[i]) i++;
  return [...Array(fromDir.length - i).fill('..'), ...to.slice(i)].join('/');
}

function relsXml(rels: Relationship[]): string {
  const entries = rels
    .map(
      r =>
        `<Relationship Id="${r.id}" Type="${r.type}" Target="${r.target.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"${r.external ? ' TargetMode="External"' : ''}/>`,
    )
    .join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${NS.rel}">${entries}</Relationships>`;
}

/** Inserts `fragment` before the first of `laterElements` present in `xml`, else before the root's closing tag. */
function insertBefore(xml: string, fragment: string, laterElements: string[]): string {
  for (const name of laterElements) {
    const i = xml.search(new RegExp(`<${name}[\\s>/]`));
    if (i !== -1) return xml.slice(0, i) + fragment + xml.slice(i);
  }
  const close = xml.lastIndexOf('</');
  return close === -1 ? xml + fragment : xml.slice(0, close) + fragment + xml.slice(close);
}

export { REL_TYPE as OOXML_REL_TYPE, REL_CHART };

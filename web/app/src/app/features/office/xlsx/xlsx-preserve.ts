import {
  NS,
  OoxmlPackage,
  REL_TYPE,
  Relationship,
  attr,
  children,
  parseXml,
  relsPathFor,
  resolveTarget,
  serializeXml,
} from '../shared/ooxml';
import { XlsxDefinedName, mergeDefinedNamesXml } from './xlsx-defined-names';
import { XlsxExtras } from './xlsx-extras';

/**
 * What ExcelJS cannot carry and the editor cannot edit, kept from the original package and
 * written back into the saved one: charts and shapes (with the drawing anchors and media they
 * sit with), pivot tables (with their caches), tables, sparklines, external links, macros,
 * custom XML, custom document properties and printer settings. The cells a pivot table or a
 * table shows stay ordinary cells, so they survive editing; Excel refreshes the pivot from its
 * cache on open. A sheet whose drawing holds a chart or a shape gets its original drawing back
 * whole, so the pictures on that sheet are kept as they were rather than as the editor last
 * placed them.
 */

const XDR = 'http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing';
const C = 'http://schemas.openxmlformats.org/drawingml/2006/chart';
const X14 = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
const CT_CHART = 'application/vnd.openxmlformats-officedocument.drawingml.chart+xml';
const CT_DRAWING = 'application/vnd.openxmlformats-officedocument.drawing+xml';
const CT_PIVOT_TABLE = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotTable+xml';
const CT_PIVOT_CACHE_DEF = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheDefinition+xml';
const CT_PIVOT_CACHE_REC = 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheRecords+xml';
const CT_CHART_STYLE = 'application/vnd.ms-office.chartstyle+xml';
const CT_CHART_COLORS = 'application/vnd.ms-office.chartcolorstyle+xml';
const CT_TABLE = 'application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml';
const CT_EXTERNAL_LINK = 'application/vnd.openxmlformats-officedocument.spreadsheetml.externalLink+xml';
const CT_VBA = 'application/vnd.ms-office.vbaProject';
const CT_PRINTER_SETTINGS = 'application/vnd.openxmlformats-officedocument.spreadsheetml.printerSettings';
const CT_CUSTOM_XML_PROPS = 'application/vnd.openxmlformats-officedocument.customXmlProperties+xml';
const CT_CUSTOM_PROPERTIES = 'application/vnd.openxmlformats-officedocument.custom-properties+xml';
const CT_WORKBOOK = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml';
const CT_WORKBOOK_MACRO = 'application/vnd.ms-excel.sheet.macroEnabled.main+xml';
const REL_OFFICE = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const REL_DRAWING = `${REL_OFFICE}/drawing`;
const REL_CHART = `${REL_OFFICE}/chart`;
const REL_PIVOT_TABLE = `${REL_OFFICE}/pivotTable`;
const REL_PIVOT_CACHE_DEF = `${REL_OFFICE}/pivotCacheDefinition`;
const REL_TABLE = `${REL_OFFICE}/table`;
const REL_EXTERNAL_LINK = `${REL_OFFICE}/externalLink`;
const REL_VBA = `${REL_OFFICE}/vbaProject`;
const REL_CUSTOM_XML = `${REL_OFFICE}/customXml`;
const REL_PRINTER_SETTINGS = `${REL_OFFICE}/printerSettings`;
const REL_THREADED_COMMENTS = 'http://schemas.microsoft.com/office/2017/10/relationships/threadedComment';
const REL_CUSTOM_PROPERTIES = `${REL_OFFICE}/custom-properties`;
const SPARKLINE_EXT_URI = '{05C60535-1F16-4fd2-B633-F4F36F0B64E0}';
const SLICER_LIST_EXT_URI = '{A8765BA9-456A-4dab-B4F3-ACF838C121DE}';

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
  /** The sheet's drawing part when it holds a chart or a shape: path, the charts it anchors, and how many other shapes. */
  drawing?: { path: string; charts: PreservedChart[]; shapes: number };
  pivotTables: string[];
  tables: string[];
  printerSettings?: string;
  /** Serialised `<ext>` elements of the sheet's extLst that are carried: sparkline groups. */
  exts: string[];
  threadedComments: boolean;
  slicers: boolean;
}

export interface PreservedParts {
  /** Every part that goes back verbatim, keyed by original path. */
  parts: Record<string, PreservedPart>;
  /** Relationship parts for the above, keyed by the part they belong to. */
  rels: Record<string, Relationship[]>;
  bySheet: Record<string, PreservedSheet>;
  /** Pivot cache definitions referenced from the workbook: cacheId to part path. */
  pivotCaches: { cacheId: string; path: string }[];
  /** External workbook links, in the order the workbook lists them (formulas index them by position). */
  externalLinks: string[];
  vbaProject?: string;
  /** Custom XML item parts hung off the workbook. */
  customXml: string[];
  /** docProps/custom.xml. */
  customProperties?: string;
  /** What ExcelJS carries that Univer has no model for (xlsx-extras.ts). */
  extras?: XlsxExtras;
}

/** Whether the original package held anything that goes back by surgery. */
export function hasPreservedParts(p: PreservedParts | null | undefined): boolean {
  if (!p) return false;
  return (
    Object.values(p.bySheet).some(s => s.drawing || s.pivotTables.length || s.tables.length || s.printerSettings || s.exts.length) ||
    p.pivotCaches.length > 0 ||
    p.externalLinks.length > 0 ||
    !!p.vbaProject ||
    p.customXml.length > 0 ||
    !!p.customProperties
  );
}

/** Whether the saved file must be a macro-enabled workbook. */
export function hasMacros(p: PreservedParts | null | undefined): boolean {
  return !!p?.vbaProject;
}

export function emptyPreservedSheet(): PreservedSheet {
  return { pivotTables: [], tables: [], exts: [], threadedComments: false, slicers: false };
}

/** Collects everything to carry from the original package, per sheet name and for the workbook. */
export async function collectPreservedParts(pkg: OoxmlPackage): Promise<PreservedParts> {
  const out: PreservedParts = { parts: {}, rels: {}, bySheet: {}, pivotCaches: [], externalLinks: [], customXml: [] };
  const contentTypes = await readContentTypes(pkg);
  const wbPath = 'xl/workbook.xml';
  const wb = await pkg.xml(wbPath);
  if (!wb) return out;
  const wbNs = wb.documentElement.namespaceURI ?? '';
  const wbRels = await pkg.rels(wbPath);
  /** Keeps a part, its relationship file, and (recursively) every internal part it points at. */
  const keepTree = async (path: string): Promise<boolean> => {
    if (out.parts[path]) return true;
    const bytes = await pkg.bytes(path);
    if (!bytes) return false;
    out.parts[path] = { path, bytes, contentType: contentTypes.resolve(path) };
    const rels = await pkg.rels(path);
    if (rels.size) out.rels[path] = [...rels.values()];
    for (const r of rels.values()) if (!r.external) await keepTree(resolveTarget(path, r.target));
    return true;
  };
  const sheets = children(wb.documentElement, wbNs, 'sheets')[0];
  for (const sheet of children(sheets, wbNs, 'sheet')) {
    const name = sheet.getAttribute('name') ?? '';
    const relId = attr(sheet, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    if (!rel) continue;
    const sheetPath = resolveTarget(wbPath, rel.target);
    const sheetRels = await pkg.rels(sheetPath);
    const entry = emptyPreservedSheet();
    for (const r of sheetRels.values()) {
      const target = resolveTarget(sheetPath, r.target);
      if (r.type === REL_DRAWING) {
        const drawing = await pkg.xml(target);
        if (!drawing) continue;
        const { charts, shapes } = drawingContents(drawing, await pkg.rels(target), target);
        if (!charts.length && !shapes) continue;
        await keepTree(target);
        entry.drawing = { path: target, charts, shapes };
      } else if (r.type === REL_PIVOT_TABLE) {
        if (await keepTree(target)) entry.pivotTables.push(target);
      } else if (r.type === REL_TABLE) {
        if (await keepTree(target)) entry.tables.push(target);
      } else if (r.type === REL_PRINTER_SETTINGS) {
        if (await keepTree(target)) entry.printerSettings = target;
      } else if (r.type === REL_THREADED_COMMENTS) {
        entry.threadedComments = true;
      }
    }
    const sheetXml = await pkg.xml(sheetPath);
    if (sheetXml) {
      const extLst = children(sheetXml.documentElement, wbNs, 'extLst')[0];
      for (const ext of children(extLst, wbNs, 'ext')) {
        const uri = ext.getAttribute('uri') ?? '';
        if (uri === SPARKLINE_EXT_URI || ext.getElementsByTagNameNS(X14, 'sparklineGroups').length) entry.exts.push(serializeXml(ext));
        else if (uri === SLICER_LIST_EXT_URI || ext.getElementsByTagNameNS(X14, 'slicerList').length) entry.slicers = true;
      }
    }
    if (
      entry.drawing ||
      entry.pivotTables.length ||
      entry.tables.length ||
      entry.printerSettings ||
      entry.exts.length ||
      entry.threadedComments ||
      entry.slicers
    )
      out.bySheet[name] = entry;
  }
  const caches = children(wb.documentElement, wbNs, 'pivotCaches')[0];
  for (const cache of children(caches, wbNs, 'pivotCache')) {
    const relId = attr(cache, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    const cacheId = cache.getAttribute('cacheId');
    if (!rel || !cacheId) continue;
    const path = resolveTarget(wbPath, rel.target);
    if (await keepTree(path)) out.pivotCaches.push({ cacheId, path });
  }
  const externals = children(wb.documentElement, wbNs, 'externalReferences')[0];
  for (const ref of children(externals, wbNs, 'externalReference')) {
    const relId = attr(ref, NS.r, 'id');
    const rel = relId ? wbRels.get(relId) : undefined;
    if (!rel) continue;
    const path = resolveTarget(wbPath, rel.target);
    if (await keepTree(path)) out.externalLinks.push(path);
  }
  for (const r of wbRels.values()) {
    if (r.external) continue;
    const path = resolveTarget(wbPath, r.target);
    if (r.type === REL_VBA) {
      if (await keepTree(path)) out.vbaProject = path;
    } else if (r.type === REL_CUSTOM_XML) {
      if (await keepTree(path)) out.customXml.push(path);
    }
  }
  const rootRels = await pkg.rels('');
  for (const r of rootRels.values()) {
    if (r.type === REL_CUSTOM_PROPERTIES && !r.external) {
      const path = resolveTarget('', r.target);
      if (await keepTree(path)) out.customProperties = path;
    }
  }
  return out;
}

/** The anchors of a drawing: charts with their positions, and a count of everything else that is not a picture. */
function drawingContents(
  drawing: Document,
  rels: Map<string, Relationship>,
  drawingPath: string,
): { charts: PreservedChart[]; shapes: number } {
  const charts: PreservedChart[] = [];
  let shapes = 0;
  for (const anchor of Array.from(drawing.documentElement.childNodes).filter((n): n is Element => n.nodeType === 1)) {
    const content = Array.from(anchor.childNodes).filter(
      (n): n is Element => n.nodeType === 1 && !['from', 'to', 'ext', 'pos', 'clientData'].includes((n as Element).localName),
    );
    const chartEl = anchor.getElementsByTagNameNS(C, 'chart')[0];
    if (chartEl) {
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
      charts.push({ from, to, chartPath: resolveTarget(drawingPath, rel.target), name });
      continue;
    }
    if (content.some(el => el.localName !== 'pic')) shapes++;
  }
  return { charts, shapes };
}

interface ContentTypes {
  resolve(path: string): string;
}

async function readContentTypes(pkg: OoxmlPackage): Promise<ContentTypes> {
  const overrides = new Map<string, string>();
  const defaults = new Map<string, string>();
  const doc = await pkg.xml('[Content_Types].xml');
  if (doc) {
    for (const el of Array.from(doc.getElementsByTagNameNS(NS.ct, 'Override'))) {
      const part = el.getAttribute('PartName');
      const type = el.getAttribute('ContentType');
      if (part && type) overrides.set(part, type);
    }
    for (const el of Array.from(doc.getElementsByTagNameNS(NS.ct, 'Default'))) {
      const ext = el.getAttribute('Extension');
      const type = el.getAttribute('ContentType');
      if (ext && type) defaults.set(ext.toLowerCase(), type);
    }
  }
  return {
    resolve: (path: string) => {
      const override = overrides.get(`/${path}`);
      if (override) return override;
      const guessed = contentTypeFor(path);
      if (guessed) return guessed;
      const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase();
      return defaults.get(ext) ?? 'application/xml';
    },
  };
}

function contentTypeFor(path: string): string | null {
  if (/^xl\/charts\/chart/.test(path)) return CT_CHART;
  if (/^xl\/charts\/style/.test(path)) return CT_CHART_STYLE;
  if (/^xl\/charts\/colors/.test(path)) return CT_CHART_COLORS;
  if (/^xl\/drawings\/drawing/.test(path)) return CT_DRAWING;
  if (/^xl\/pivotTables\//.test(path)) return CT_PIVOT_TABLE;
  if (/pivotCacheDefinition/.test(path)) return CT_PIVOT_CACHE_DEF;
  if (/pivotCacheRecords/.test(path)) return CT_PIVOT_CACHE_REC;
  if (/^xl\/tables\//.test(path)) return CT_TABLE;
  if (/^xl\/externalLinks\/externalLink\d*\.xml$/.test(path)) return CT_EXTERNAL_LINK;
  if (/^xl\/vbaProject\.bin$/.test(path)) return CT_VBA;
  if (/^xl\/printerSettings\//.test(path)) return CT_PRINTER_SETTINGS;
  if (/^customXml\/itemProps/.test(path)) return CT_CUSTOM_XML_PROPS;
  if (/^docProps\/custom\.xml$/.test(path)) return CT_CUSTOM_PROPERTIES;
  return null;
}

export interface InjectOptions {
  /** The names to write into the workbook part (xlsx-defined-names.ts). */
  definedNames?: XlsxDefinedName[];
}

/**
 * Writes the preserved parts into a package ExcelJS produced: parts under new names where
 * ExcelJS could have written one of its own (so nothing it wrote is overwritten),
 * relationships rewritten to those names, sheet and workbook references added, content types
 * declared, and the defined names merged in. Sheets are matched by name.
 */
export async function injectPreservedParts(bytes: Uint8Array, preserved: PreservedParts, options: InjectOptions = {}): Promise<Uint8Array> {
  const names = options.definedNames ?? [];
  if (!hasPreservedParts(preserved) && !names.length) return bytes;
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
  let wbXml = (await pkg.text(wbPath)) ?? '';
  const wb = parseXml(wbXml);
  const wbNs = wb.documentElement.namespaceURI ?? '';
  const wbRels = [...(await pkg.rels(wbPath)).values()];
  const sheetPathByName = new Map<string, string>();
  for (const sheet of children(children(wb.documentElement, wbNs, 'sheets')[0], wbNs, 'sheet')) {
    const relId = attr(sheet, NS.r, 'id');
    const rel = relId ? wbRels.find(r => r.id === relId) : undefined;
    if (rel) sheetPathByName.set(sheet.getAttribute('name') ?? '', resolveTarget(wbPath, rel.target));
  }
  for (const [name, entry] of Object.entries(preserved.bySheet)) {
    const sheetPath = sheetPathByName.get(name);
    if (!sheetPath) continue;
    const rels = [...(await pkg.rels(sheetPath)).values()];
    let sheetXml = (await pkg.text(sheetPath)) ?? '';
    const nextId = idAllocator(rels);
    if (entry.drawing) {
      // Any drawing ExcelJS wrote for this sheet (pictures) is replaced by the original whole.
      const own = rels.find(r => r.type === REL_DRAWING);
      if (own) {
        rels.splice(rels.indexOf(own), 1);
        sheetXml = sheetXml.replace(/<drawing\b[^>]*\/>/, '');
      }
      const id = nextId();
      rels.push({ id, type: REL_DRAWING, target: relative(sheetPath, rename.get(entry.drawing.path)!), external: false });
      sheetXml = insertBefore(sheetXml, `<drawing r:id="${id}"/>`, AFTER_DRAWING);
    }
    for (const pivotPath of entry.pivotTables) {
      rels.push({ id: nextId(), type: REL_PIVOT_TABLE, target: relative(sheetPath, rename.get(pivotPath)!), external: false });
    }
    if (entry.tables.length) {
      sheetXml = sheetXml.replace(/<tableParts\b[^>]*\/>|<tableParts\b[^>]*>[\s\S]*?<\/tableParts>/, '');
      const parts = entry.tables.map(tablePath => {
        const id = nextId();
        rels.push({ id, type: REL_TABLE, target: relative(sheetPath, rename.get(tablePath)!), external: false });
        return `<tablePart r:id="${id}"/>`;
      });
      sheetXml = insertBefore(sheetXml, `<tableParts count="${parts.length}">${parts.join('')}</tableParts>`, ['extLst']);
    }
    if (entry.printerSettings) {
      const id = nextId();
      rels.push({ id, type: REL_PRINTER_SETTINGS, target: relative(sheetPath, rename.get(entry.printerSettings)!), external: false });
      if (/<pageSetup\b/.test(sheetXml))
        sheetXml = sheetXml.replace(
          /<pageSetup\b([^>]*?)(\s*\/?>)/,
          (_m, attrs: string, end: string) => `<pageSetup${attrs.replace(/\s+r:id="[^"]*"/, '')} r:id="${id}"${end}`,
        );
      else sheetXml = insertBefore(sheetXml, `<pageSetup r:id="${id}"/>`, AFTER_PAGE_SETUP);
    }
    if (entry.exts.length) {
      const extXml = entry.exts.join('');
      if (/<extLst\b[^>]*\/>/.test(sheetXml)) sheetXml = sheetXml.replace(/<extLst\b[^>]*\/>/, `<extLst>${extXml}</extLst>`);
      else if (/<\/extLst>/.test(sheetXml)) sheetXml = sheetXml.replace('</extLst>', `${extXml}</extLst>`);
      else sheetXml = insertBefore(sheetXml, `<extLst>${extXml}</extLst>`, []);
    }
    if (!/xmlns:r=/.test(sheetXml.slice(0, 800))) sheetXml = sheetXml.replace('<worksheet ', `<worksheet xmlns:r="${NS.r}" `);
    pkg.put(sheetPath, sheetXml);
    pkg.put(relsPathFor(sheetPath), relsXml(rels));
  }
  // 3. Workbook: pivot caches, external links, macros, custom XML, defined names.
  const nextWbId = idAllocator(wbRels);
  if (preserved.pivotCaches.length) {
    const cachesXml = preserved.pivotCaches
      .map(c => {
        const id = nextWbId();
        wbRels.push({ id, type: REL_PIVOT_CACHE_DEF, target: relative(wbPath, rename.get(c.path)!), external: false });
        return `<pivotCache cacheId="${c.cacheId}" r:id="${id}"/>`;
      })
      .join('');
    wbXml = wbXml.replace(/<pivotCaches>[\s\S]*?<\/pivotCaches>/, '');
    wbXml = insertBefore(wbXml, `<pivotCaches>${cachesXml}</pivotCaches>`, AFTER_PIVOT_CACHES);
  }
  if (preserved.externalLinks.length) {
    const refs = preserved.externalLinks
      .map(path => {
        const id = nextWbId();
        wbRels.push({ id, type: REL_EXTERNAL_LINK, target: relative(wbPath, rename.get(path)!), external: false });
        return `<externalReference r:id="${id}"/>`;
      })
      .join('');
    wbXml = wbXml.replace(/<externalReferences>[\s\S]*?<\/externalReferences>/, '');
    wbXml = insertBefore(wbXml, `<externalReferences>${refs}</externalReferences>`, AFTER_EXTERNAL_REFERENCES);
  }
  if (preserved.vbaProject) {
    wbRels.push({ id: nextWbId(), type: REL_VBA, target: relative(wbPath, rename.get(preserved.vbaProject)!), external: false });
  }
  for (const path of preserved.customXml) {
    wbRels.push({ id: nextWbId(), type: REL_CUSTOM_XML, target: relative(wbPath, rename.get(path)!), external: false });
  }
  if (names.length) wbXml = mergeDefinedNamesXml(wbXml, names);
  if (!/xmlns:r=/.test(wbXml.slice(0, 800))) wbXml = wbXml.replace('<workbook ', `<workbook xmlns:r="${NS.r}" `);
  pkg.put(wbPath, wbXml);
  pkg.put(relsPathFor(wbPath), relsXml(wbRels));
  // 4. Package relationships (custom document properties).
  if (preserved.customProperties) {
    const rootRels = [...(await pkg.rels('')).values()].filter(r => r.type !== REL_CUSTOM_PROPERTIES);
    const id = idAllocator(rootRels)();
    rootRels.push({ id, type: REL_CUSTOM_PROPERTIES, target: rename.get(preserved.customProperties)!, external: false });
    pkg.put('_rels/.rels', relsXml(rootRels));
  }
  // 5. Content types.
  let ct = (await pkg.text('[Content_Types].xml')) ?? '';
  for (const path of Object.keys(preserved.parts)) {
    const newPath = rename.get(path)!;
    ct = ct.replace(new RegExp(`<Override PartName="/${newPath.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}"[^>]*/>`), '');
  }
  const overrides = Object.entries(preserved.parts)
    .map(([path, part]) => `<Override PartName="/${rename.get(path)}" ContentType="${part.contentType}"/>`)
    .join('');
  ct = ct.replace('</Types>', `${overrides}</Types>`);
  if (preserved.vbaProject) ct = ct.replace(`ContentType="${CT_WORKBOOK}"`, `ContentType="${CT_WORKBOOK_MACRO}"`);
  pkg.put('[Content_Types].xml', ct);
  return pkg.save(preserved.vbaProject ? XLSM_MIME : XLSX_MIME);
}

export const XLSX_MIME = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
export const XLSM_MIME = 'application/vnd.ms-excel.sheet.macroEnabled.12';

/** Elements of a worksheet that follow `<drawing>`. */
const AFTER_DRAWING = ['legacyDrawing', 'legacyDrawingHF', 'picture', 'oleObjects', 'controls', 'webPublishItems', 'tableParts', 'extLst'];
/** Elements of a worksheet that follow `<pageSetup>`. */
const AFTER_PAGE_SETUP = [
  'headerFooter',
  'rowBreaks',
  'colBreaks',
  'customProperties',
  'cellWatches',
  'ignoredErrors',
  'smartTags',
  'drawing',
  ...AFTER_DRAWING,
];
const AFTER_PIVOT_CACHES = ['smartTagPr', 'smartTagTypes', 'webPublishing', 'fileRecoveryPr', 'webPublishObjects', 'extLst'];
const AFTER_EXTERNAL_REFERENCES = ['definedNames', 'calcPr', 'oleSize', 'customWorkbookViews', 'pivotCaches', ...AFTER_PIVOT_CACHES];

/** Parts ExcelJS never writes keep their names; Excel and other tools look some of them up by name. */
const KEEP_NAME =
  /^(xl\/vbaProject\.bin|xl\/vbaProjectSignature\.bin|customXml\/|docProps\/custom\.xml|xl\/externalLinks\/|xl\/printerSettings\/|xl\/tables\/|xl\/pivotTables\/|xl\/pivotCache\/|xl\/charts\/|xl\/embeddings\/)/;

/** "xl/drawings/drawing1.xml" becomes "xl/drawings/drawingP1.xml": the same folder, a name ExcelJS never uses; a name already of that form stays. */
function renamed(path: string): string {
  if (KEEP_NAME.test(path)) return path;
  const slash = path.lastIndexOf('/');
  const dir = path.slice(0, slash + 1);
  const name = path.slice(slash + 1);
  const m = /^([A-Za-z]+?)(P?)(\d*)(\.[^.]+)$/.exec(name);
  if (!m) return `${dir}p-${name}`;
  return `${dir}${m[1]}P${m[3] || '1'}${m[4]}`;
}

function idAllocator(rels: Relationship[]): () => string {
  let next = rels.length + 1;
  return () => {
    while (rels.some(r => r.id === `rId${next}`)) next++;
    return `rId${next++}`;
  };
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

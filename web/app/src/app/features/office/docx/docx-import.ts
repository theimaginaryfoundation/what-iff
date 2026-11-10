import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import {
  NS,
  OoxmlPackage,
  REL_TYPE,
  Relationship,
  attr,
  bytesToBase64,
  child,
  mimeForExtension,
  num,
  resolveTarget,
  twipsToPx,
} from '../shared/ooxml';
import { BodyBuilder, BodyContext, extrasXml } from './docx-body';
import { noteIdFor, parseNotes } from './docx-notes';
import { parseTableStyles } from './docx-table-styles';
import { ParsedStyles, parseNumbering, parseStyles, paragraphPropsFromPPr } from './docx-styles';
import {
  DocDocument,
  DocDocumentStyle,
  DocFooter,
  DocHeader,
  DocNote,
  DocPageSetup,
  DocSectionBreak,
  DocTable,
  DocDrawing,
  RESOURCE_DOC_DRAWINGS,
} from './univer-doc-types';

export interface DocxImportOptions {
  id: string;
  name: string;
}

export interface DocxImportResult {
  document: DocDocument;
  report: FidelityReport;
}

/**
 * Reads a .docx into a Univer document snapshot (ADR 0x043, office suite): paragraphs with
 * their named and direct formatting, runs, hyperlinks, page-number fields, bullet and numbered
 * lists, tables with spans, shading and borders, inline and floating images, headers and footers,
 * page size and margins. Block content with no counterpart is kept verbatim for export; charts,
 * shapes, equations, footnotes and legacy objects are reported as dropped.
 */
export async function docxToDocument(bytes: ArrayBuffer | Uint8Array, opts: DocxImportOptions): Promise<DocxImportResult> {
  const pkg = await OoxmlPackage.open(bytes);
  return packageToDocument(pkg, opts);
}

export async function packageToDocument(pkg: OoxmlPackage, opts: DocxImportOptions): Promise<DocxImportResult> {
  const fidelity = new FidelityCollector();
  const rootRels = await pkg.rels('');
  const mainRel = [...rootRels.values()].find(r => r.type === REL_TYPE.officeDocument);
  const mainPath = mainRel ? resolveTarget('', mainRel.target) : 'word/document.xml';
  const main = await pkg.xml(mainPath);
  if (!main) throw new Error('Not a Word document: no main document part');
  const rels = await pkg.rels(mainPath);
  const partFor = (type: string) => {
    const rel = [...rels.values()].find(r => r.type === type);
    return rel ? resolveTarget(mainPath, rel.target) : null;
  };
  const stylesPart = partFor(REL_TYPE.styles);
  const numberingPart = partFor(REL_TYPE.numbering);
  const parsedStyles = parseStyles(stylesPart ? await pkg.xml(stylesPart) : null);
  const numbering = parseNumbering(numberingPart ? await pkg.xml(numberingPart) : null);
  const knownLists = new Set(Object.keys(numbering.lists));
  const styleNumbering = styleNumberingMap(stylesPart ? await pkg.xml(stylesPart) : null);
  const tableStyles = parseTableStyles(stylesPart ? await pkg.xml(stylesPart) : null);
  const ids = new Map<string, number>();
  const nextId = (prefix: string) => {
    const n = (ids.get(prefix) ?? 0) + 1;
    ids.set(prefix, n);
    return `${prefix}${n}`;
  };
  const mediaCache = new Map<string, string | null>();
  const mediaFor =
    (partPath: string, partRels: Map<string, Relationship>) =>
    (relId: string): string | null => {
      const rel = partRels.get(relId);
      if (!rel || rel.external) return null;
      const path = resolveTarget(partPath, rel.target);
      return mediaCache.get(path) ?? null;
    };
  // Preload every image once; headers and footers share media with the body.
  for (const path of pkg.paths()) {
    if (!/^word\/media\//.test(path)) continue;
    const data = await pkg.bytes(path);
    if (!data) continue;
    const ext = path.slice(path.lastIndexOf('.') + 1);
    const mime = mimeForExtension(ext);
    mediaCache.set(path, mime === 'application/octet-stream' ? null : `data:${mime};base64,${bytesToBase64(data)}`);
    if (mime === 'application/octet-stream') fidelity.drop('Images of an unsupported type');
  }

  const hasComments = !!partFor(REL_TYPE.comments);
  const notes: Record<string, DocNote> = {};
  const baseContext = (partPath: string, partRels: Map<string, Relationship>, where: string): BodyContext => ({
    unitId: opts.id,
    partPath,
    rels: partRels,
    media: mediaFor(partPath, partRels),
    styles: parsedStyles.styles,
    styleNumbering,
    tableStyles,
    knownLists,
    fidelity,
    where,
    nextId,
    hasComments,
    noteFor: (kind, wordId) => {
      const id = noteIdFor(kind, wordId);
      return notes[id] ? id : null;
    },
  });

  const headers: Record<string, DocHeader> = {};
  const footers: Record<string, DocFooter> = {};
  const tableSource: Record<string, DocTable> = {};
  const drawings: Record<string, DocDrawing> = {};
  const drawingsOrder: string[] = [];

  // Note stories first, so body references can find them.
  for (const kind of ['footnote', 'endnote'] as const) {
    const path = partFor(kind === 'footnote' ? REL_TYPE.footnotes : REL_TYPE.endnotes);
    const doc = path ? await pkg.xml(path) : null;
    if (!doc) continue;
    const partRels = await pkg.rels(path!);
    const parsed = parseNotes(
      doc,
      kind,
      ctx => new BodyBuilder({ ...baseContext(path!, partRels, kind === 'footnote' ? 'Footnotes' : 'Endnotes'), ...ctx }),
    );
    Object.assign(notes, parsed.notes);
    Object.assign(drawings, parsed.drawings);
    drawingsOrder.push(...parsed.drawingsOrder);
  }

  const loadHeaderFooter = async (relId: string, kind: 'header' | 'footer'): Promise<string | null> => {
    const rel = rels.get(relId);
    if (!rel) return null;
    const path = resolveTarget(mainPath, rel.target);
    const existing =
      kind === 'header' ? Object.values(headers).find(h => h.docxPath === path) : Object.values(footers).find(f => f.docxPath === path);
    if (existing) return kind === 'header' ? (existing as DocHeader).headerId : (existing as DocFooter).footerId;
    const doc = await pkg.xml(path);
    if (!doc) return null;
    const partRels = await pkg.rels(path);
    const builder = new BodyBuilder({ ...baseContext(path, partRels, kind === 'header' ? 'Header' : 'Footer'), headerFooter: true });
    builder.blocks(doc.documentElement);
    const body = builder.finish();
    const id = nextId(kind === 'header' ? 'h' : 'f');
    // Univer lays every drawing out from the document's own map, headers and footers included.
    Object.assign(drawings, builder.drawings);
    drawingsOrder.push(...builder.drawingsOrder);
    const tables = Object.keys(builder.tableSource).length ? { tableSource: builder.tableSource } : {};
    if (kind === 'header') headers[id] = { headerId: id, body, docxPath: path, ...tables };
    else footers[id] = { footerId: id, body, docxPath: path, ...tables };
    return id;
  };

  const sectionsSeen: { el: Element; startIndex: number }[] = [];
  const builder = new BodyBuilder({
    ...baseContext(mainPath, rels, 'Body'),
    onSection: (el, startIndex) => sectionsSeen.push({ el, startIndex }),
  });
  const bodyEl = child(main.documentElement, NS.w, 'body');
  if (bodyEl) builder.blocks(bodyEl);

  // Section properties: the last w:sectPr describes the document; earlier ones end sections.
  const sectionSetups: {
    startIndex: number;
    setup: DocPageSetup & Pick<DocSectionBreak, 'sectionType' | 'columnProperties' | 'columnSeparatorType'>;
  }[] = [];
  for (const { el, startIndex } of sectionsSeen)
    sectionSetups.push({ startIndex, setup: await sectionSetup(el, loadHeaderFooter, fidelity) });
  const last = sectionSetups.pop();
  const documentStyle: DocDocumentStyle = {
    documentFlavor: 1,
    pageSize: { width: 794, height: 1123 },
    marginTop: 96,
    marginBottom: 96,
    marginLeft: 96,
    marginRight: 96,
    marginHeader: 48,
    marginFooter: 48,
    ...(last?.setup ?? {}),
  };
  delete (documentStyle as Partial<DocSectionBreak>).sectionType;
  delete (documentStyle as Partial<DocSectionBreak>).columnProperties;
  delete (documentStyle as Partial<DocSectionBreak>).columnSeparatorType;
  if (parsedStyles.defaultTextStyle) documentStyle.textStyle = parsedStyles.defaultTextStyle;
  const normal = parsedStyles.defaultParagraphStyleId ? parsedStyles.styles[parsedStyles.defaultParagraphStyleId] : undefined;
  const defaultParagraph = { ...(parsedStyles.defaultParagraphStyle ?? {}), ...(normal?.paragraphStyle ?? {}) };
  delete defaultParagraph.namedStyleType;
  if (Object.keys(defaultParagraph).length) documentStyle.defaultParagraphStyle = defaultParagraph;
  if (normal?.textStyle) documentStyle.textStyle = { ...(documentStyle.textStyle ?? {}), ...normal.textStyle };
  const settingsPart = partFor(REL_TYPE.settings);
  if (settingsPart) {
    const settings = await pkg.xml(settingsPart);
    const tab = num(attr(child(settings?.documentElement, NS.w, 'defaultTabStop'), NS.w, 'val'));
    if (tab !== undefined) documentStyle.defaultTabStop = twipsToPx(tab);
    if (child(settings?.documentElement, NS.w, 'evenAndOddHeaders')) documentStyle.evenAndOddHeaders = 1;
  }

  const body = builder.finish();
  // Mid-document sections: each earlier w:sectPr becomes a section break after its paragraph.
  for (const section of sectionSetups) {
    const at = section.startIndex;
    if (at <= 0 || body.dataStream[at - 1] !== '\r') continue;
    shiftOffsets(body, at);
    body.dataStream = body.dataStream.slice(0, at) + '\n' + body.dataStream.slice(at);
    body.sectionBreaks!.push({ sectionId: nextId('s'), ...section.setup, startIndex: at });
  }
  body.sectionBreaks!.sort((a, b) => a.startIndex - b.startIndex);
  if (sectionSetups.length) fidelity.drop('Section page setups (all pages take the last one)');

  Object.assign(tableSource, builder.tableSource);
  Object.assign(drawings, builder.drawings);
  drawingsOrder.push(...builder.drawingsOrder);

  const document: DocDocument = {
    id: opts.id,
    title: opts.name,
    body,
    documentStyle,
    styles: parsedStyles.styles,
    settings: { zoomRatio: 1 },
  };
  if (Object.keys(numbering.lists).length) document.lists = numbering.lists;
  if (Object.keys(headers).length) document.headers = headers;
  if (Object.keys(footers).length) document.footers = footers;
  if (Object.keys(notes).length) document.notes = notes;
  if (Object.keys(tableSource).length) document.tableSource = tableSource;
  if (drawingsOrder.length) {
    document.drawings = drawings;
    document.drawingsOrder = drawingsOrder;
    // Layout reads `drawings`; the drawing plugin that paints them loads from its resource.
    document.resources = [{ name: RESOURCE_DOC_DRAWINGS, data: JSON.stringify({ data: drawings, order: drawingsOrder }) }];
  }
  return { document, report: fidelity.report() };
}

/** Paragraph styles that carry numbering (w:style/w:pPr/w:numPr), walking basedOn. */
function styleNumberingMap(stylesDoc: Document | null): Map<string, { numId: string; level: number }> {
  const out = new Map<string, { numId: string; level: number }>();
  if (!stylesDoc) return out;
  const direct = new Map<string, { numId: string; level: number }>();
  const basedOn = new Map<string, string>();
  for (const el of Array.from(stylesDoc.documentElement.childNodes).filter(
    (n): n is Element => n.nodeType === 1 && (n as Element).localName === 'style',
  )) {
    if (attr(el, NS.w, 'type') !== 'paragraph') continue;
    const id = attr(el, NS.w, 'styleId');
    if (!id) continue;
    const props = paragraphPropsFromPPr(child(el, NS.w, 'pPr'));
    if (props.numbering) direct.set(id, props.numbering);
    const base = attr(child(el, NS.w, 'basedOn'), NS.w, 'val');
    if (base) basedOn.set(id, base);
  }
  for (const id of new Set([...direct.keys(), ...basedOn.keys()])) {
    let cur: string | undefined = id;
    const seen = new Set<string>();
    while (cur && !seen.has(cur)) {
      seen.add(cur);
      const n = direct.get(cur);
      if (n) {
        out.set(id, n);
        break;
      }
      cur = basedOn.get(cur);
    }
  }
  return out;
}

async function sectionSetup(
  sectPr: Element,
  loadHeaderFooter: (relId: string, kind: 'header' | 'footer') => Promise<string | null>,
  fidelity: FidelityCollector,
): Promise<DocPageSetup & Pick<DocSectionBreak, 'sectionType' | 'columnProperties' | 'columnSeparatorType'>> {
  const out: DocPageSetup & Pick<DocSectionBreak, 'sectionType' | 'columnProperties' | 'columnSeparatorType'> = {};
  const pgSz = child(sectPr, NS.w, 'pgSz');
  const w = num(attr(pgSz, NS.w, 'w'));
  const h = num(attr(pgSz, NS.w, 'h'));
  if (w && h) out.pageSize = { width: twipsToPx(w), height: twipsToPx(h) };
  if (attr(pgSz, NS.w, 'orient') === 'landscape') out.pageOrient = 1;
  const pgMar = child(sectPr, NS.w, 'pgMar');
  if (pgMar) {
    const m = (name: string) => num(attr(pgMar, NS.w, name));
    const top = m('top'),
      bottom = m('bottom'),
      left = m('left'),
      right = m('right'),
      header = m('header'),
      footer = m('footer');
    if (top !== undefined) out.marginTop = twipsToPx(Math.abs(top));
    if (bottom !== undefined) out.marginBottom = twipsToPx(Math.abs(bottom));
    if (left !== undefined) out.marginLeft = twipsToPx(left);
    if (right !== undefined) out.marginRight = twipsToPx(right);
    if (header !== undefined) out.marginHeader = twipsToPx(header);
    if (footer !== undefined) out.marginFooter = twipsToPx(footer);
  }
  const start = num(attr(child(sectPr, NS.w, 'pgNumType'), NS.w, 'start'));
  if (start !== undefined) out.pageNumberStart = start;
  const type = attr(child(sectPr, NS.w, 'type'), NS.w, 'val');
  out.sectionType = type === 'continuous' ? 1 : type === 'evenPage' ? 4 : type === 'oddPage' ? 5 : 3;
  const cols = child(sectPr, NS.w, 'cols');
  const colCount = num(attr(cols, NS.w, 'num')) ?? 1;
  if (colCount > 1) {
    const space = twipsToPx(num(attr(cols, NS.w, 'space')) ?? 720);
    out.columnProperties = Array.from({ length: colCount }, () => ({ width: 0, paddingEnd: space }));
    out.columnSeparatorType = attr(cols, NS.w, 'sep') === '1' ? 2 : 1;
    fidelity.warn('Multiple columns');
  }
  for (const ref of Array.from(sectPr.childNodes).filter(
    (n): n is Element =>
      n.nodeType === 1 &&
      (n as Element).namespaceURI === NS.w &&
      ((n as Element).localName === 'headerReference' || (n as Element).localName === 'footerReference'),
  )) {
    const relId = attr(ref, NS.r, 'id');
    if (!relId) continue;
    const kind = ref.localName === 'headerReference' ? 'header' : 'footer';
    const id = await loadHeaderFooter(relId, kind);
    if (!id) continue;
    const which = attr(ref, NS.w, 'type') ?? 'default';
    if (kind === 'header') {
      if (which === 'first') out.firstPageHeaderId = id;
      else if (which === 'even') out.evenPageHeaderId = id;
      else out.defaultHeaderId = id;
    } else if (which === 'first') out.firstPageFooterId = id;
    else if (which === 'even') out.evenPageFooterId = id;
    else out.defaultFooterId = id;
  }
  if (child(sectPr, NS.w, 'titlePg')) out.useFirstPageHeaderFooter = 1;
  const extras = extrasXml(sectPr, MODELLED_SECTPR);
  if (extras) out.docxSectPr = extras;
  return out;
}

const MODELLED_SECTPR = new Set([
  'headerReference',
  'footerReference',
  'type',
  'pgSz',
  'pgMar',
  'pgNumType',
  'cols',
  'titlePg',
]);

/** After inserting one character at `at`, every recorded offset at or past it moves by one. */
function shiftOffsets(body: DocDocument['body'], at: number): void {
  const bump = (n: number) => (n >= at ? n + 1 : n);
  for (const r of body.textRuns ?? []) {
    if (r.st >= at) r.st += 1;
    if (r.ed > at) r.ed += 1;
  }
  for (const p of body.paragraphs ?? []) p.startIndex = bump(p.startIndex);
  for (const s of body.sectionBreaks ?? []) s.startIndex = bump(s.startIndex);
  for (const b of body.customBlocks ?? []) b.startIndex = bump(b.startIndex);
  for (const r of body.customRanges ?? []) {
    r.startIndex = bump(r.startIndex);
    r.endIndex = bump(r.endIndex);
  }
  for (const t of body.tables ?? []) {
    t.startIndex = bump(t.startIndex);
    t.endIndex = bump(t.endIndex);
  }
  for (const r of body.docxRawBlocks ?? []) r.startIndex = bump(r.startIndex);
  for (const b of body.docxRawCustomBlocks ?? []) b.startIndex = bump(b.startIndex);
}

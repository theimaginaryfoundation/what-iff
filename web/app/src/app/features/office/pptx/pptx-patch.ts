import { FidelityCollector } from '../shared/fidelity';
import {
  NS,
  OoxmlPackage,
  REL_TYPE,
  Relationship,
  attr,
  child,
  children,
  elementChildren,
  extensionForMime,
  parseDataUrl,
  parseXml,
  relsPathFor,
  resolveTarget,
  serializeXml,
} from '../shared/ooxml';
import { Deck, Slide, SlideElement, TableElement } from './deck-model';
import {
  COMMENT_REL_EXT,
  CommentedSlide,
  PptxComments,
  REL_MODERN_COMMENTS,
  SlideCommentThread,
  threadsOn,
  writePptxComments,
} from './pptx-comments';
import {
  A,
  P,
  SlideWriteContext,
  TC_PR_ORDER,
  allShapeIds,
  ensureTextBody,
  newLineNode,
  newPictureNode,
  newShapeNode,
  nodeId,
  patchBackground,
  patchBodyProperties,
  patchFill,
  patchLine,
  patchNotesText,
  patchParagraphs,
  patchTransform,
  shapeNodes,
} from './pptx-xml';

/**
 * Saves a deck by patching the package it was read from (ADR 0x043, office suite): the parts
 * the editor did not touch are copied byte for byte (masters, layouts, theme, media, charts,
 * animations, comments, sections, fonts); a changed slide gets its shapes patched in place;
 * slides added, copied, removed or reordered are reflected in the presentation's slide list,
 * relationships and content types. `imported` is the deck as it was read, the baseline every
 * comparison is made against.
 */

const PPTX_MIME = 'application/vnd.openxmlformats-officedocument.presentationml.presentation';
const REL = {
  slide: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide',
  slideLayout: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout',
  notesSlide: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide',
  notesMaster: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesMaster',
  comments: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments',
  image: REL_TYPE.image,
  hyperlink: REL_TYPE.hyperlink,
};
const CT = {
  slide: 'application/vnd.openxmlformats-officedocument.presentationml.slide+xml',
  notesSlide: 'application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml',
};
const P14 = 'http://schemas.microsoft.com/office/powerpoint/2010/main';
const XML_DECLARATION = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\r\n';

export interface PatchOptions {
  original: ArrayBuffer | Uint8Array;
  imported: Deck;
  /** The comment threads to save and those the file was read with (ADR 0x045); left out, comments stay as they were. */
  comments?: { threads: readonly SlideCommentThread[]; imported: PptxComments };
}

/** A stable JSON form of a model value, without the editor's own ids. */
function fingerprint(value: unknown): string {
  const OMIT = new Set(['id', 'sourcePath', 'clonedFrom']);
  const walk = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(walk);
    if (v && typeof v === 'object') {
      const out: Record<string, unknown> = {};
      for (const key of Object.keys(v as object).sort()) {
        if (OMIT.has(key)) continue;
        const inner = (v as Record<string, unknown>)[key];
        if (inner !== undefined) out[key] = walk(inner);
      }
      return out;
    }
    return v;
  };
  return JSON.stringify(walk(value));
}

export function sameModel(a: unknown, b: unknown): boolean {
  return fingerprint(a) === fingerprint(b);
}

interface SlideEntry {
  path: string;
  sldId: string;
  rId: string;
}

interface ResolvedSlide {
  slide: Slide;
  path: string;
  /** The slide as imported from `path` (or from the part it was copied from). */
  base: Slide | null;
  /** True when the part is new in this save (made or copied) and so must always be written. */
  fresh: boolean;
  sldId: string | null;
  /** Each element's `p:cNvPr` id as written, once the slide's shapes have been patched. */
  shapeIds?: Map<string, string>;
}

class Package {
  /** Parts of the output, by path; starts as a copy of the original and takes replacements. */
  private readonly parts = new Map<string, Uint8Array | string>();
  private readonly contentTypes: Document;
  private readonly defaults = new Set<string>();
  private contentTypesChanged = false;

  private constructor(
    private readonly source: OoxmlPackage,
    contentTypes: Document,
  ) {
    this.contentTypes = contentTypes;
    for (const el of Array.from(contentTypes.getElementsByTagNameNS(NS.ct, 'Default'))) {
      const ext = el.getAttribute('Extension');
      if (ext) this.defaults.add(ext.toLowerCase());
    }
  }

  static async open(bytes: ArrayBuffer | Uint8Array): Promise<Package> {
    const source = await OoxmlPackage.open(bytes);
    const ct = await source.xml('[Content_Types].xml');
    if (!ct) throw new Error('Not a presentation: no content types');
    const pkg = new Package(source, ct);
    for (const path of source.paths()) pkg.parts.set(path, (await source.bytes(path))!);
    return pkg;
  }

  paths(): string[] {
    return [...this.parts.keys()];
  }

  has(path: string): boolean {
    return this.parts.has(path);
  }

  async text(path: string): Promise<string | null> {
    const part = this.parts.get(path);
    if (part === undefined) return null;
    return typeof part === 'string' ? part : new TextDecoder().decode(part);
  }

  async xml(path: string): Promise<Document | null> {
    const text = await this.text(path);
    return text === null ? null : parseXml(text);
  }

  bytes(path: string): Uint8Array | null {
    const part = this.parts.get(path);
    if (part === undefined) return null;
    return typeof part === 'string' ? new TextEncoder().encode(part) : part;
  }

  async rels(partPath: string): Promise<Relationship[]> {
    const doc = await this.xml(relsPathFor(partPath));
    if (!doc) return [];
    return Array.from(doc.getElementsByTagNameNS(NS.rel, 'Relationship')).map(el => ({
      id: el.getAttribute('Id') ?? '',
      type: el.getAttribute('Type') ?? '',
      target: el.getAttribute('Target') ?? '',
      external: el.getAttribute('TargetMode') === 'External',
    }));
  }

  put(path: string, content: string | Uint8Array): void {
    this.parts.set(path, content);
  }

  putXml(path: string, doc: Document): void {
    this.parts.set(path, XML_DECLARATION + serializeXml(doc.documentElement));
  }

  putRels(partPath: string, rels: Relationship[]): void {
    const entries = rels
      .map(
        r =>
          `<Relationship Id="${r.id}" Type="${r.type}" Target="${r.target.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"${r.external ? ' TargetMode="External"' : ''}/>`,
      )
      .join('');
    this.parts.set(relsPathFor(partPath), `${XML_DECLARATION}<Relationships xmlns="${NS.rel}">${entries}</Relationships>`);
  }

  remove(path: string): void {
    this.parts.delete(path);
    this.parts.delete(relsPathFor(path));
    for (const el of Array.from(this.contentTypes.getElementsByTagNameNS(NS.ct, 'Override'))) {
      if (el.getAttribute('PartName') === `/${path}`) {
        el.parentNode?.removeChild(el);
        this.contentTypesChanged = true;
      }
    }
  }

  addOverride(path: string, contentType: string): void {
    const root = this.contentTypes.documentElement;
    const el = this.contentTypes.createElementNS(NS.ct, 'Override');
    el.setAttribute('PartName', `/${path}`);
    el.setAttribute('ContentType', contentType);
    root.appendChild(el);
    this.contentTypesChanged = true;
  }

  addDefault(extension: string, contentType: string): void {
    const ext = extension.toLowerCase();
    if (this.defaults.has(ext)) return;
    this.defaults.add(ext);
    const root = this.contentTypes.documentElement;
    const el = this.contentTypes.createElementNS(NS.ct, 'Default');
    el.setAttribute('Extension', ext);
    el.setAttribute('ContentType', contentType);
    root.insertBefore(el, root.firstChild);
    this.contentTypesChanged = true;
  }

  /** The next free number for parts named like "ppt/slides/slide{n}.xml". */
  nextNumber(pattern: RegExp): number {
    let max = 0;
    for (const path of this.parts.keys()) {
      const m = pattern.exec(path);
      if (m) max = Math.max(max, Number(m[1]) || 0);
    }
    return max + 1;
  }

  async save(): Promise<Uint8Array> {
    const out = OoxmlPackage.empty();
    if (this.contentTypesChanged) this.parts.set('[Content_Types].xml', XML_DECLARATION + serializeXml(this.contentTypes.documentElement));
    for (const [path, content] of this.parts) out.put(path, content);
    return out.save(PPTX_MIME);
  }
}

// --- the deck -----------------------------------------------------------------------------------

export async function patchPptx(deck: Deck, opts: PatchOptions, fidelity: FidelityCollector): Promise<Uint8Array> {
  const pkg = await Package.open(opts.original);
  const rootRels = await pkg.rels('');
  const main = rootRels.find(r => r.type === REL_TYPE.officeDocument);
  const presPath = main ? resolveTarget('', main.target) : 'ppt/presentation.xml';
  const pres = await pkg.xml(presPath);
  if (!pres) throw new Error('Not a presentation: no presentation part');
  const presRels = await pkg.rels(presPath);
  const entries: SlideEntry[] = [];
  for (const sldId of children(child(pres.documentElement, P, 'sldIdLst'), P, 'sldId')) {
    const rId = attr(sldId, NS.r, 'id') ?? '';
    const rel = presRels.find(r => r.id === rId);
    if (rel && rel.type === REL.slide)
      entries.push({ path: resolveTarget(presPath, rel.target), sldId: sldId.getAttribute('id') ?? '', rId });
  }
  const importedByPath = new Map<string, Slide>();
  for (const slide of opts.imported.slides) if (slide.sourcePath) importedByPath.set(slide.sourcePath, slide);
  const entryByPath = new Map(entries.map(e => [e.path, e]));
  const notesMaster = presRels.find(r => r.type === REL.notesMaster);

  // 1. Which part each slide of the deck lives in.
  const resolved: ResolvedSlide[] = [];
  const used = new Set<string>();
  let slideNumber = pkg.nextNumber(/^ppt\/slides\/slide(\d+)\.xml$/);
  for (const [index, slide] of deck.slides.entries()) {
    if (slide.sourcePath && entryByPath.has(slide.sourcePath) && !used.has(slide.sourcePath)) {
      used.add(slide.sourcePath);
      resolved.push({
        slide,
        path: slide.sourcePath,
        base: importedByPath.get(slide.sourcePath) ?? null,
        fresh: false,
        sldId: entryByPath.get(slide.sourcePath)!.sldId,
      });
      continue;
    }
    const path = `ppt/slides/slide${slideNumber++}.xml`;
    const from = slide.clonedFrom && pkg.has(slide.clonedFrom) ? slide.clonedFrom : null;
    if (from) {
      pkg.put(path, pkg.bytes(from)!);
      // The copy has no notes or comments of its own yet.
      const rels = (await pkg.rels(from)).filter(
        r => r.type !== REL.notesSlide && r.type !== REL.comments && r.type !== REL_MODERN_COMMENTS,
      );
      pkg.putRels(path, rels);
      const copy = await pkg.xml(path);
      if (copy && dropCommentRel(copy)) pkg.putXml(path, copy);
      resolved.push({ slide, path, base: importedByPath.get(from) ?? null, fresh: true, sldId: null });
    } else {
      // A slide made here takes the layout of the slide before it (or the first one).
      const reference = resolved[index - 1]?.path ?? entries[0]?.path ?? null;
      const layout = reference ? (await pkg.rels(reference)).find(r => r.type === REL.slideLayout) : undefined;
      const layoutTarget = layout ? relativeTarget(path, resolveTarget(reference!, layout.target)) : null;
      pkg.put(path, XML_DECLARATION + blankSlideXml());
      pkg.putRels(path, layoutTarget ? [{ id: 'rId1', type: REL.slideLayout, target: layoutTarget, external: false }] : []);
      if (!layoutTarget) fidelity.warn('A new slide has no layout: the file had none to copy');
      resolved.push({ slide, path, base: null, fresh: true, sldId: null });
    }
    pkg.addOverride(path, CT.slide);
  }

  // 2. Slides that are gone, with their notes and comments.
  for (const entry of entries) {
    if (used.has(entry.path)) continue;
    for (const rel of await pkg.rels(entry.path)) {
      if (rel.external) continue;
      if (rel.type === REL.notesSlide || rel.type === REL.comments || rel.type === REL_MODERN_COMMENTS)
        pkg.remove(resolveTarget(entry.path, rel.target));
    }
    pkg.remove(entry.path);
  }

  // 3. Each slide's content.
  const notesNumber = { n: pkg.nextNumber(/^ppt\/notesSlides\/notesSlide(\d+)\.xml$/) };
  const mediaNumber = { n: pkg.nextNumber(/^ppt\/media\/image(\d+)\./) };
  for (const item of resolved) {
    if (!item.fresh && item.base && sameModel(item.slide, item.base)) continue;
    await patchSlidePart(
      pkg,
      item,
      { notesNumber, mediaNumber, notesMasterTarget: notesMaster ? resolveTarget(presPath, notesMaster.target) : null },
      fidelity,
    );
  }

  // 4. The slide list, relationships and sections; untouched when the slides are the same parts in the same order.
  if (!(resolved.every((item, i) => entries[i]?.path === item.path) && resolved.length === entries.length))
    writeSlideList(pkg, pres, presPath, presRels, entries, resolved, used);

  // 5. Comments, once every slide has its id.
  if (opts.comments) {
    const { threads, imported } = opts.comments;
    const beforeByPath = new Map<string, SlideCommentThread[]>();
    for (const slide of opts.imported.slides)
      if (slide.sourcePath) beforeByPath.set(slide.sourcePath, threadsOn(imported.threads, slide.id));
    const slides: CommentedSlide[] = resolved.map(item => ({
      path: item.path,
      sldId: item.sldId ?? '',
      slide: item.slide,
      threads: threadsOn(threads, item.slide.id),
      before: item.fresh ? null : (beforeByPath.get(item.path) ?? []),
      // An unchanged slide's shapes are the file's own, so their ids are the ones they were read with.
      shapeIdOf: (elementId: string) => {
        if (item.shapeIds) return item.shapeIds.get(elementId);
        return item.slide.elements.find(e => e.id === elementId)?.sourceId?.replace(/#\d+$/, '');
      },
    }));
    await writePptxComments(pkg, presPath, slides, imported);
  }
  return pkg.save();
}

function writeSlideList(
  pkg: Package,
  pres: Document,
  presPath: string,
  presRels: Relationship[],
  entries: SlideEntry[],
  resolved: ResolvedSlide[],
  used: Set<string>,
): void {
  const keptRels = presRels.filter(r => r.type !== REL.slide || used.has(resolveTarget(presPath, r.target)));
  let nextRel = 1;
  const relIds = new Set(keptRels.map(r => r.id));
  const freshRelId = () => {
    while (relIds.has(`rId${nextRel}`)) nextRel++;
    const id = `rId${nextRel++}`;
    relIds.add(id);
    return id;
  };
  let nextSldId = Math.max(255, ...entries.map(e => Number(e.sldId) || 0)) + 1;
  const list = child(pres.documentElement, P, 'sldIdLst') ?? pres.documentElement.appendChild(pres.createElementNS(P, 'p:sldIdLst'));
  for (const old of children(list, P, 'sldId')) list.removeChild(old);
  const sections = sectionList(pres);
  const sectionOf = new Map<string, Element>();
  for (const section of sections)
    for (const id of children(child(section, P14, 'sldIdLst'), P14, 'sldId')) sectionOf.set(id.getAttribute('id') ?? '', section);
  const removedIds = new Set(entries.filter(e => !used.has(e.path)).map(e => e.sldId));
  for (const section of sections) {
    for (const id of children(child(section, P14, 'sldIdLst'), P14, 'sldId'))
      if (removedIds.has(id.getAttribute('id') ?? '')) id.parentNode?.removeChild(id);
  }
  let previousSldId: string | null = null;
  for (const item of resolved) {
    const existing = entries.find(e => e.path === item.path);
    const rId = existing?.rId ?? freshRelId();
    if (!existing) keptRels.push({ id: rId, type: REL.slide, target: relativeTarget(presPath, item.path), external: false });
    const sldId = existing?.sldId ?? String(nextSldId++);
    item.sldId = sldId;
    const el = list.appendChild(pres.createElementNS(P, 'p:sldId'));
    el.setAttribute('id', sldId);
    el.setAttributeNS(NS.r, 'r:id', rId);
    if (!existing && sections.length) {
      // A new slide joins the section of the slide before it.
      const section = (previousSldId && sectionOf.get(previousSldId)) || sections[sections.length - 1];
      const ids = child(section, P14, 'sldIdLst') ?? section.appendChild(pres.createElementNS(P14, 'p14:sldIdLst'));
      ids.appendChild(pres.createElementNS(P14, 'p14:sldId')).setAttribute('id', sldId);
      sectionOf.set(sldId, section);
    }
    previousSldId = sldId;
  }
  pkg.putXml(presPath, pres);
  pkg.putRels(presPath, keptRels);
}

/** Removes a slide's link to its modern comment part; true when it had one. */
function dropCommentRel(slide: Document): boolean {
  const extLst = child(slide.documentElement, P, 'extLst');
  const exts = children(extLst, P, 'ext').filter(e => e.getAttribute('uri') === COMMENT_REL_EXT);
  for (const ext of exts) extLst!.removeChild(ext);
  if (extLst && !children(extLst, P, 'ext').length) extLst.parentNode?.removeChild(extLst);
  return exts.length > 0;
}

/**
 * The shape ids pptxgenjs gave a slide's elements: it writes one top-level shape per element, in
 * order, so they pair up when the counts agree; otherwise no element can be named.
 */
async function writtenShapeIds(pkg: Package, path: string, slide: Slide): Promise<(elementId: string) => string | undefined> {
  const doc = await pkg.xml(path);
  const spTree = child(child(doc?.documentElement, P, 'cSld'), P, 'spTree');
  const nodes = elementChildren(spTree).filter(node => nodeId(node) !== null);
  if (nodes.length !== slide.elements.length) return () => undefined;
  const ids = new Map(slide.elements.map((el, i) => [el.id, nodeId(nodes[i])!]));
  return elementId => ids.get(elementId);
}

/**
 * Adds comments to a package written from scratch (pptxgenjs), whose slides are slide1..n in deck
 * order.
 */
export async function addCommentsToNewPackage(bytes: Uint8Array, deck: Deck, threads: readonly SlideCommentThread[]): Promise<Uint8Array> {
  if (!threads.length) return bytes;
  const pkg = await Package.open(bytes);
  const main = (await pkg.rels('')).find(r => r.type === REL_TYPE.officeDocument);
  const presPath = main ? resolveTarget('', main.target) : 'ppt/presentation.xml';
  const pres = await pkg.xml(presPath);
  const presRels = await pkg.rels(presPath);
  if (!pres) return bytes;
  const slides: CommentedSlide[] = [];
  for (const [i, sldIdEl] of children(child(pres.documentElement, P, 'sldIdLst'), P, 'sldId').entries()) {
    const rel = presRels.find(r => r.id === attr(sldIdEl, NS.r, 'id'));
    const slide = deck.slides[i];
    if (!rel || !slide) continue;
    const path = resolveTarget(presPath, rel.target);
    slides.push({
      path,
      sldId: sldIdEl.getAttribute('id') ?? '',
      slide,
      threads: threadsOn(threads, slide.id),
      before: null,
      shapeIdOf: await writtenShapeIds(pkg, path, slide),
    });
  }
  await writePptxComments(pkg, presPath, slides, null);
  return pkg.save();
}

function sectionList(pres: Document): Element[] {
  return Array.from(pres.getElementsByTagNameNS(P14, 'section'));
}

/** "../slideLayouts/slideLayout1.xml" for a target seen from a part's folder. */
function relativeTarget(fromPart: string, toPart: string): string {
  const fromDir = fromPart
    .slice(0, fromPart.lastIndexOf('/') + 1)
    .split('/')
    .filter(Boolean);
  const to = toPart.split('/');
  let i = 0;
  while (i < fromDir.length && i < to.length - 1 && fromDir[i] === to[i]) i++;
  return [...Array(fromDir.length - i).fill('..'), ...to.slice(i)].join('/');
}

function blankSlideXml(): string {
  return (
    `<p:sld xmlns:a="${A}" xmlns:r="${NS.r}" xmlns:p="${P}"><p:cSld><p:spTree>` +
    `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
    `<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
    `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
  );
}

// --- one slide ----------------------------------------------------------------------------------

interface Counters {
  notesNumber: { n: number };
  mediaNumber: { n: number };
  notesMasterTarget: string | null;
}

async function patchSlidePart(pkg: Package, item: ResolvedSlide, counters: Counters, fidelity: FidelityCollector): Promise<void> {
  const { slide, path, base } = item;
  const doc = await pkg.xml(path);
  if (!doc) return;
  const rels = await pkg.rels(path);
  const where = `Slide ${slide.sourcePath ?? path}`;
  let relsChanged = false;
  const relIds = new Set(rels.map(r => r.id));
  let nextRel = 1;
  const freshRelId = () => {
    while (relIds.has(`rId${nextRel}`)) nextRel++;
    const id = `rId${nextRel++}`;
    relIds.add(id);
    return id;
  };
  const shapeIds = allShapeIds(doc.documentElement);
  let nextShape = Math.max(1, ...shapeIds) + 1;
  const ctx: SlideWriteContext = {
    doc,
    hyperlinkRel: url => {
      const existing = rels.find(r => r.type === REL.hyperlink && r.external && r.target === url);
      if (existing) return existing.id;
      const id = freshRelId();
      rels.push({ id, type: REL.hyperlink, target: url, external: true });
      relsChanged = true;
      return id;
    },
    imageRel: dataUrl => {
      const parsed = parseDataUrl(dataUrl);
      const ext = parsed ? extensionForMime(parsed.mime) : 'bin';
      if (!parsed || ext === 'bin') {
        fidelity.drop('Images of an unsupported type', where);
        return null;
      }
      const mediaPath = `ppt/media/image${counters.mediaNumber.n++}.${ext}`;
      pkg.put(mediaPath, parsed.bytes);
      pkg.addDefault(ext, parsed.mime);
      const id = freshRelId();
      rels.push({ id, type: REL.image, target: relativeTarget(path, mediaPath), external: false });
      relsChanged = true;
      return id;
    },
    nextShapeId: () => nextShape++,
    warn: feature => fidelity.warn(feature, where),
  };

  const cSld = child(doc.documentElement, P, 'cSld');
  const spTree = child(cSld, P, 'spTree');
  if (!cSld || !spTree) return;
  const contentChanged = item.fresh || !base || !sameModel({ ...slide, notes: '' }, { ...base, notes: '' });
  if (contentChanged) {
    if (!base || !sameModel(slide.background, base.background)) patchBackground(cSld, slide.background, ctx);
    item.shapeIds = new Map();
    patchElements(spTree, slide, base, ctx, item.shapeIds);
    pkg.putXml(path, doc);
  }

  // Speaker notes.
  if ((base ? slide.notes !== base.notes : slide.notes.trim() !== '') || (item.fresh && slide.notes.trim() !== '')) {
    const notesRel = rels.find(r => r.type === REL.notesSlide);
    if (notesRel) {
      const notesPath = resolveTarget(path, notesRel.target);
      const notes = await pkg.xml(notesPath);
      const body = notes ? notesBody(notes) : null;
      if (notes && body) {
        patchNotesText(ensureTextBody(body), slide.notes, { ...ctx, doc: notes });
        pkg.putXml(notesPath, notes);
      }
    } else if (counters.notesMasterTarget) {
      const notesPath = `ppt/notesSlides/notesSlide${counters.notesNumber.n++}.xml`;
      const notes = parseXml(blankNotesXml());
      patchNotesText(ensureTextBody(notesBody(notes)!), slide.notes, { ...ctx, doc: notes });
      pkg.putXml(notesPath, notes);
      pkg.putRels(notesPath, [
        { id: 'rId1', type: REL.notesMaster, target: relativeTarget(notesPath, counters.notesMasterTarget), external: false },
        { id: 'rId2', type: REL.slide, target: relativeTarget(notesPath, path), external: false },
      ]);
      pkg.addOverride(notesPath, CT.notesSlide);
      rels.push({ id: freshRelId(), type: REL.notesSlide, target: relativeTarget(path, notesPath), external: false });
      relsChanged = true;
    } else fidelity.warn('Speaker notes could not be added: the file has no notes master', where);
  }
  if (relsChanged) pkg.putRels(path, rels);
}

/** The notes placeholder (`p:ph type="body"`) of a notes slide. */
function notesBody(notes: Document): Element | null {
  for (const sp of Array.from(notes.getElementsByTagNameNS(P, 'sp'))) {
    const ph = sp.getElementsByTagNameNS(P, 'ph')[0];
    if (ph && ph.getAttribute('type') === 'body') return sp;
  }
  return null;
}

function blankNotesXml(): string {
  return (
    `<p:notes xmlns:a="${A}" xmlns:r="${NS.r}" xmlns:p="${P}"><p:cSld><p:spTree>` +
    `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
    `<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
    `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>` +
    `<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
    `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`
  );
}

// --- the shapes of a slide ----------------------------------------------------------------------

function patchElements(spTree: Element, slide: Slide, base: Slide | null, ctx: SlideWriteContext, written: Map<string, string>): void {
  const treeChildren = elementChildren(spTree);
  const nodeById = new Map<string, Element>();
  const repeats = new Map<string, number>();
  for (const node of treeChildren) {
    const id = nodeId(node);
    if (!id) continue;
    const n = repeats.get(id) ?? 0;
    repeats.set(id, n + 1);
    nodeById.set(n === 0 ? id : `${id}#${n}`, node);
  }
  const baseById = new Map<string, SlideElement>();
  for (const el of base?.elements ?? []) if (el.sourceId) baseById.set(el.sourceId, el);
  const modelIds = new Set(slide.elements.map(el => el.sourceId).filter((id): id is string => !!id));

  // Shapes the editor removed: those the import saw and the model no longer has.
  for (const [id] of baseById) {
    if (modelIds.has(id)) continue;
    const node = nodeById.get(id);
    if (node) {
      spTree.removeChild(node);
      nodeById.delete(id);
    }
  }

  // Shapes that changed.
  for (const el of slide.elements) {
    const node = el.sourceId ? nodeById.get(el.sourceId) : undefined;
    if (!node) continue;
    const was = el.sourceId ? baseById.get(el.sourceId) : undefined;
    if (was && sameModel(el, was)) continue;
    for (const shape of shapeNodes(node)) patchElement(shape, el, was, ctx);
  }

  // Order: the matched shapes take the slots of the matched shapes in model order; anything
  // the import did not see keeps its place.
  const matched = slide.elements.filter(el => el.sourceId && nodeById.has(el.sourceId));
  const matchedNodes = new Set(matched.map(el => nodeById.get(el.sourceId!)!));
  const slots = elementChildren(spTree).filter(node => matchedNodes.has(node));
  const desired = matched.map(el => nodeById.get(el.sourceId!)!);
  if (slots.some((node, i) => node !== desired[i])) {
    const markers = slots.map(node => spTree.insertBefore(ctx.doc.createComment('slot'), node));
    for (const node of slots) spTree.removeChild(node);
    markers.forEach((marker, i) => {
      spTree.insertBefore(desired[i], marker);
      spTree.removeChild(marker);
    });
  }

  // Shapes made here go in after the model element before them.
  let previous: Element | null = null;
  for (const el of slide.elements) {
    const existing = el.sourceId ? nodeById.get(el.sourceId) : undefined;
    if (existing) {
      previous = existing;
      const id = nodeId(existing);
      if (id) written.set(el.id, id);
      continue;
    }
    const node = newNode(el, ctx);
    if (!node) continue;
    const id = nodeId(node);
    if (id) written.set(el.id, id);
    if (previous) spTree.insertBefore(node, previous.nextSibling);
    else {
      // Before the first matched shape, after the group properties.
      const first = elementChildren(spTree).find(n => nodeId(n) !== null);
      spTree.insertBefore(node, first ?? null);
    }
    previous = node;
  }
}

function newNode(el: SlideElement, ctx: SlideWriteContext): Element | null {
  switch (el.kind) {
    case 'shape':
      return newShapeNode(el, ctx);
    case 'image':
      return newPictureNode(el, ctx);
    case 'line':
      return newLineNode(el, ctx);
    case 'table':
    case 'chart':
    case 'group':
      ctx.warn(`${el.kind === 'table' ? 'Tables' : el.kind === 'chart' ? 'Charts' : 'Groups'} made by copying are not written`);
      return null;
  }
}

/** Applies what differs between the model element and its imported form to one shape node. */
function patchElement(node: Element, el: SlideElement, was: SlideElement | undefined, ctx: SlideWriteContext): void {
  const geometryChanged =
    !was ||
    ['left', 'top', 'width', 'height', 'rotate', 'flipH', 'flipV'].some(
      key => (el as unknown as Record<string, unknown>)[key] !== (was as unknown as Record<string, unknown>)[key],
    );
  if (geometryChanged) patchTransform(node, el);
  switch (el.kind) {
    case 'shape': {
      const spPr = child(node, P, 'spPr');
      if (spPr && (!was || was.kind !== 'shape' || !sameModel(el.fill, was.fill))) patchFill(spPr, el.fill, ctx);
      if (spPr && (!was || was.kind !== 'shape' || !sameModel(el.border, was.border))) patchLine(spPr, el.border, ctx);
      const wasText = was?.kind === 'shape' ? was.text : null;
      if (el.text && !sameModel(el.text, wasText)) {
        const txBody = ensureTextBody(node);
        patchBodyProperties(txBody, el.text, wasText ?? undefined);
        if (!sameModel(el.text.paragraphs, wasText?.paragraphs)) patchParagraphs(txBody, el.text.paragraphs, wasText?.paragraphs, ctx);
      }
      return;
    }
    case 'line':
    case 'image': {
      const spPr = child(node, P, 'spPr');
      const wasBorder = was && (was.kind === 'line' || was.kind === 'image') ? was.border : undefined;
      if (spPr && (!was || !sameModel(el.border, wasBorder))) patchLine(spPr, el.border, ctx);
      return;
    }
    case 'table':
      patchTable(node, el, was?.kind === 'table' ? was : undefined, ctx);
      return;
    case 'group':
      if (!was || !sameModel(el.children, was.kind === 'group' ? was.children : null))
        ctx.warn('Group contents are kept as in the original');
      return;
    case 'chart':
      return;
  }
}

function patchTable(node: Element, el: TableElement, was: TableElement | undefined, ctx: SlideWriteContext): void {
  const tbl = node.getElementsByTagNameNS(A, 'tbl')[0];
  if (!tbl) return;
  const rows = children(tbl, A, 'tr');
  el.rows.forEach((row, r) => {
    const cells = children(rows[r], A, 'tc');
    row.forEach((cell, c) => {
      const tc = cells[c];
      if (!tc || cell.covered) return;
      const wasCell = was?.rows[r]?.[c];
      if (wasCell && sameModel(cell, wasCell)) return;
      if (!wasCell || !sameModel(cell.text.paragraphs, wasCell.text.paragraphs)) {
        let txBody = child(tc, A, 'txBody');
        if (!txBody) {
          txBody = ctx.doc.createElementNS(A, 'a:txBody');
          txBody.appendChild(ctx.doc.createElementNS(A, 'a:bodyPr'));
          txBody.appendChild(ctx.doc.createElementNS(A, 'a:lstStyle'));
          tc.insertBefore(txBody, tc.firstChild);
        }
        patchParagraphs(txBody, cell.text.paragraphs, wasCell?.text.paragraphs, ctx);
      }
      if (!wasCell || !sameModel(cell.fill, wasCell.fill)) {
        const tcPr = child(tc, A, 'tcPr') ?? tc.appendChild(ctx.doc.createElementNS(A, 'a:tcPr'));
        patchFill(tcPr, cell.fill, ctx, TC_PR_ORDER);
      }
    });
  });
  if (was && (el.colWidths.join() !== was.colWidths.join() || el.rowHeights.join() !== was.rowHeights.join()))
    ctx.warn('Table column widths and row heights are kept as in the original');
}

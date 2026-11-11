import { CommentPerson, CommentThread, DocComment, authorPersonId, userPersonId } from '../comments/comment-threads';
import { NS, OoxmlPackage, Relationship, escapeXml, resolveTarget } from '../shared/ooxml';
import { Deck, Slide, SlideElement } from './deck-model';

/**
 * Slide comments (ADR 0x045) between a .pptx and the editor's comment threads.
 *
 * PowerPoint writes "modern" comments (MS-PPTX 2.1.5, the p188 namespace): one comment part per
 * slide, a `cm` per thread with its replies in `replyLst`, anchored to the slide (`pc:sldMkLst`)
 * or to a shape on it (`ac:deMkLst`), with a resolved `status`; the people are in one authors part
 * for the presentation. Older files have "legacy" comments (`p:cmLst`), threaded through
 * `p15:threadingInfo`, with authors in commentAuthors.xml. Both are read; a slide whose comments
 * change is written in the modern form, and a slide whose comments did not is left as it was.
 */

export interface SlideCommentAnchor {
  /** The deck slide the thread is on. */
  slideId: string;
  /** Where its pin is, in points from the slide's top-left. */
  x: number;
  y: number;
  /** The element it is about, when it is about one. */
  elementId?: string;
}

export type SlideCommentThread = CommentThread<SlideCommentAnchor>;

export const P188 = 'http://schemas.microsoft.com/office/powerpoint/2018/8/main';
const PC = 'http://schemas.microsoft.com/office/powerpoint/2013/main/command';
const AC = 'http://schemas.microsoft.com/office/drawing/2013/main/command';
const P = 'http://schemas.openxmlformats.org/presentationml/2006/main';
const P14 = 'http://schemas.microsoft.com/office/powerpoint/2010/main';
const P15 = 'http://schemas.microsoft.com/office/powerpoint/2012/main';
const A = 'http://schemas.openxmlformats.org/drawingml/2006/main';
export const REL_MODERN_COMMENTS = 'http://schemas.microsoft.com/office/2018/10/relationships/comments';
export const REL_AUTHORS = 'http://schemas.microsoft.com/office/2018/10/relationships/authors';
export const REL_LEGACY_COMMENTS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments';
const REL_LEGACY_AUTHORS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/commentAuthors';
export const CT_MODERN_COMMENTS = 'application/vnd.ms-powerpoint.comments+xml';
export const CT_AUTHORS = 'application/vnd.ms-powerpoint.authors+xml';
/** The slide extension that links a slide to its modern comment part. */
export const COMMENT_REL_EXT = '{6950BFC3-D8DA-4A85-94F7-54DA5524770B}';
/** The presence provider authors.xml names for WhatIff users; their userId is the WhatIff user id. */
export const WHATIFF_PROVIDER = 'WhatIff';
const EMU_PER_PT = 12700;
/** Legacy comment positions are in PowerPoint's master units, 576 per inch. */
const LEGACY_UNITS_PER_PT = 8;
const GUID = /^\{[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}\}$/i;

/** A new comment or author id, as PowerPoint writes them: a braced GUID. */
export function newCommentGuid(): string {
  const random =
    globalThis.crypto?.randomUUID?.() ??
    'xxxxxxxx-xxxx-4xxx-8xxx-xxxxxxxxxxxx'.replace(/x/g, () => Math.floor(Math.random() * 16).toString(16));
  return `{${random.toUpperCase()}}`;
}

function kids(parent: Element | null | undefined, ns: string, localName: string): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter(
    (n): n is Element => n.nodeType === 1 && (n as Element).namespaceURI === ns && (n as Element).localName === localName,
  );
}

function kid(parent: Element | null | undefined, ns: string, localName: string): Element | null {
  return kids(parent, ns, localName)[0] ?? null;
}

/** The text of a DrawingML text body, a line per paragraph. */
function bodyText(txBody: Element | null): string {
  return kids(txBody, A, 'p')
    .map(p =>
      Array.from(p.getElementsByTagNameNS(A, '*'))
        .map(el => (el.localName === 't' ? (el.textContent ?? '') : el.localName === 'br' ? '\n' : ''))
        .join(''),
    )
    .join('\n');
}

function isoDate(value: string | null): string {
  const date = value ? new Date(value) : null;
  return date && !Number.isNaN(date.getTime()) ? date.toISOString() : new Date(0).toISOString();
}

// --- reading -----------------------------------------------------------------------------------

export interface PptxAuthor {
  id: string;
  name: string;
  initials?: string;
  userId?: string;
  providerId?: string;
}

/** The comment parts of a presentation, as read: what a save compares against and keeps. */
export interface PptxComments {
  threads: SlideCommentThread[];
  /** Modern authors, by id. */
  authors: PptxAuthor[];
  /** The authors part, package-absolute, if there is one. */
  authorsPath?: string;
}

function personOf(author: PptxAuthor | undefined, fallbackName: string): CommentPerson {
  if (author?.providerId === WHATIFF_PROVIDER && author.userId) return { id: userPersonId(author.userId), name: author.name };
  const name = author?.name || fallbackName || 'Unknown';
  return { id: authorPersonId(name), name };
}

/** Mentions are `@Name` in the text: whoever of the people the file knows is named that way. */
function withMentions(comment: DocComment, known: Iterable<CommentPerson>): DocComment {
  const mentions = [...known].filter(p => p.name && comment.text.includes(`@${p.name}`)).map(p => ({ id: p.id, name: p.name }));
  return mentions.length ? { ...comment, mentions } : comment;
}

/** Reads the comments of every slide of `deck` that came from a slide part. */
export async function readPptxComments(pkg: OoxmlPackage, presPath: string, deck: Deck): Promise<PptxComments> {
  const presRels = await pkg.rels(presPath);
  const relPath = (type: string) => {
    const rel = [...presRels.values()].find(r => r.type === type && !r.external);
    return rel ? resolveTarget(presPath, rel.target) : undefined;
  };
  const authorsPath = relPath(REL_AUTHORS);
  const authors: PptxAuthor[] = kids((await (authorsPath ? pkg.xml(authorsPath) : null))?.documentElement, P188, 'author').map(el => {
    const author: PptxAuthor = { id: el.getAttribute('id') ?? '', name: el.getAttribute('name') ?? '' };
    for (const key of ['initials', 'userId', 'providerId'] as const) {
      const value = el.getAttribute(key);
      if (value !== null) author[key] = value;
    }
    return author;
  });
  const legacyAuthorsPath = relPath(REL_LEGACY_AUTHORS);
  const legacyAuthors = new Map(
    kids((await (legacyAuthorsPath ? pkg.xml(legacyAuthorsPath) : null))?.documentElement, P, 'cmAuthor').map(el => [
      el.getAttribute('id') ?? '',
      el.getAttribute('name') ?? '',
    ]),
  );
  const known = new Map<string, CommentPerson>();
  for (const a of authors) known.set(personOf(a, a.name).id, personOf(a, a.name));
  for (const name of legacyAuthors.values()) known.set(authorPersonId(name), { id: authorPersonId(name), name });

  const threads: SlideCommentThread[] = [];
  for (const slide of deck.slides) {
    if (!slide.sourcePath) continue;
    const rels = await pkg.rels(slide.sourcePath);
    for (const rel of rels.values()) {
      if (rel.external) continue;
      const doc =
        rel.type === REL_MODERN_COMMENTS || rel.type === REL_LEGACY_COMMENTS
          ? await pkg.xml(resolveTarget(slide.sourcePath, rel.target))
          : null;
      if (!doc) continue;
      const found =
        rel.type === REL_MODERN_COMMENTS
          ? modernThreads(doc, slide, new Map(authors.map(a => [a.id.toUpperCase(), a])))
          : legacyThreads(doc, slide, legacyAuthors);
      threads.push(...found.map(t => ({ ...t, comments: t.comments.map(c => withMentions(c, known.values())) })));
    }
  }
  const out: PptxComments = { threads, authors };
  if (authorsPath) out.authorsPath = authorsPath;
  return out;
}

/** Where a thread about `element` puts its pin when the file gives no position: the element's top-right corner. */
function cornerOf(element: SlideElement): { x: number; y: number } {
  return { x: element.left + element.width, y: element.top };
}

function modernThreads(doc: Document, slide: Slide, authors: Map<string, PptxAuthor>): SlideCommentThread[] {
  const comment = (el: Element): DocComment => {
    const author = authors.get((el.getAttribute('authorId') ?? '').toUpperCase());
    const person = personOf(author, '');
    return {
      id: el.getAttribute('id') ?? newCommentGuid(),
      authorId: person.id,
      authorName: person.name,
      createdAt: isoDate(el.getAttribute('created')),
      text: bodyText(kid(el, P188, 'txBody')),
    };
  };
  return kids(doc.documentElement, P188, 'cm').map(cm => {
    // A shape anchor names the shape by its id on the slide (ac:spMk, or another moniker with an id).
    const shapeMk = Array.from(kid(cm, AC, 'deMkLst')?.getElementsByTagNameNS(AC, '*') ?? []).find(
      el => el.localName.endsWith('Mk') && el.getAttribute('id'),
    );
    const element = shapeMk ? slide.elements.find(e => e.sourceId === shapeMk.getAttribute('id')) : undefined;
    const pos = kid(cm, P188, 'pos');
    const offset = pos ? { x: Number(pos.getAttribute('x') ?? 0) / EMU_PER_PT, y: Number(pos.getAttribute('y') ?? 0) / EMU_PER_PT } : null;
    // The position is from the top-left of what the comment is anchored to.
    const at = element
      ? offset
        ? { x: element.left + offset.x, y: element.top + offset.y }
        : cornerOf(element)
      : (offset ?? { x: 0, y: 0 });
    const thread: SlideCommentThread = {
      id: cm.getAttribute('id') ?? newCommentGuid(),
      anchor: { slideId: slide.id, x: at.x, y: at.y, ...(element ? { elementId: element.id } : {}) },
      comments: [comment(cm), ...kids(kid(cm, P188, 'replyLst'), P188, 'reply').map(comment)],
    };
    if (element) thread.quote = element.name;
    const status = cm.getAttribute('status');
    if (status === 'resolved' || status === 'closed') thread.resolved = true;
    return thread;
  });
}

/** A stable GUID for a legacy comment, so it keeps its id once written in the modern form. */
function legacyGuid(authorId: string, idx: string): string {
  const hex = (n: string, width: number) => (Number(n) || 0).toString(16).toUpperCase().padStart(width, '0').slice(-width);
  return `{00000000-0000-4000-8000-${hex(authorId, 4)}${hex(idx, 8)}}`;
}

function legacyThreads(doc: Document, slide: Slide, authors: Map<string, string>): SlideCommentThread[] {
  const roots = new Map<string, SlideCommentThread>();
  const replies: { parent: string; comment: DocComment }[] = [];
  for (const cm of kids(doc.documentElement, P, 'cm')) {
    const authorId = cm.getAttribute('authorId') ?? '0';
    const idx = cm.getAttribute('idx') ?? '0';
    const person = personOf(undefined, authors.get(authorId) ?? '');
    const comment: DocComment = {
      id: legacyGuid(authorId, idx),
      authorId: person.id,
      authorName: person.name,
      createdAt: isoDate(cm.getAttribute('dt')),
      text: kid(cm, P, 'text')?.textContent ?? '',
    };
    const parent = cm.getElementsByTagNameNS(P15, 'parentCm')[0];
    if (parent) {
      replies.push({ parent: legacyGuid(parent.getAttribute('authorId') ?? '0', parent.getAttribute('idx') ?? '0'), comment });
      continue;
    }
    const pos = kid(cm, P, 'pos');
    roots.set(comment.id, {
      id: comment.id,
      anchor: {
        slideId: slide.id,
        x: Number(pos?.getAttribute('x') ?? 0) / LEGACY_UNITS_PER_PT,
        y: Number(pos?.getAttribute('y') ?? 0) / LEGACY_UNITS_PER_PT,
      },
      comments: [comment],
    });
  }
  for (const { parent, comment } of replies) roots.get(parent)?.comments.push(comment);
  return [...roots.values()];
}

// --- writing -----------------------------------------------------------------------------------

/** What writing needs of the package being saved (pptx-patch's Package). */
export interface CommentPackage {
  text(path: string): Promise<string | null>;
  xml(path: string): Promise<Document | null>;
  put(path: string, content: string): void;
  putXml(path: string, doc: Document): void;
  rels(partPath: string): Promise<Relationship[]>;
  putRels(partPath: string, rels: Relationship[]): void;
  remove(path: string): void;
  addOverride(path: string, contentType: string): void;
  has(path: string): boolean;
}

/** A slide of the saved presentation and the threads on it. */
export interface CommentedSlide {
  path: string;
  sldId: string;
  /** The deck slide it holds. */
  slide: Slide;
  threads: readonly SlideCommentThread[];
  /** The threads it had when read, or null when the part is new in this save. */
  before: readonly SlideCommentThread[] | null;
}

const XML_DECLARATION = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\r\n';

/** The parts of a thread a save has to keep, in a form that compares. */
function shapeOf(threads: readonly SlideCommentThread[]): string {
  return JSON.stringify(
    threads.map(t => [
      t.id,
      !!t.resolved,
      Math.round(t.anchor.x),
      Math.round(t.anchor.y),
      t.anchor.elementId ?? '',
      t.comments.map(c => [c.id, c.authorId, c.authorName, c.text]),
    ]),
  );
}

function initialsOf(name: string): string {
  return (
    name
      .split(/\s+/)
      .filter(Boolean)
      .map(w => w[0]!.toUpperCase())
      .join('')
      .slice(0, 3) || '?'
  );
}

/** "../comments/x.xml" for a target seen from a part's folder. */
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

function freshRelId(rels: readonly Relationship[]): string {
  const ids = new Set(rels.map(r => r.id));
  let n = 1;
  while (ids.has(`rId${n}`)) n++;
  return `rId${n}`;
}

/**
 * Writes the comments of every slide whose threads changed, in the modern form, and the authors
 * part they refer to. Slides whose threads are as they were keep their comment parts untouched.
 */
export async function writePptxComments(
  pkg: CommentPackage,
  presPath: string,
  slides: readonly CommentedSlide[],
  original: PptxComments | null,
): Promise<void> {
  const changed = slides.filter(s => (s.before === null ? s.threads.length > 0 : shapeOf(s.threads) !== shapeOf(s.before)));
  if (!changed.length) return;
  const authors = new AuthorList(original?.authors ?? []);
  for (const item of changed) await writeSlide(pkg, item, authors);
  if (!authors.changed) return;
  const presRels = await pkg.rels(presPath);
  const existing = presRels.find(r => r.type === REL_AUTHORS && !r.external);
  const path = existing ? resolveTarget(presPath, existing.target) : (original?.authorsPath ?? 'ppt/authors.xml');
  pkg.put(path, XML_DECLARATION + authors.xml());
  if (!existing) {
    presRels.push({ id: freshRelId(presRels), type: REL_AUTHORS, target: relativeTarget(presPath, path), external: false });
    pkg.putRels(presPath, presRels);
    pkg.addOverride(path, CT_AUTHORS);
  }
}

class AuthorList {
  changed = false;
  private readonly list: PptxAuthor[];

  constructor(authors: readonly PptxAuthor[]) {
    this.list = authors.map(a => ({ ...a }));
  }

  /** The author id for a comment's author, adding them when the list has no such author. */
  idFor(comment: DocComment): string {
    const userId = comment.authorId.startsWith('user:') ? comment.authorId.slice('user:'.length) : null;
    const found = userId
      ? this.list.find(a => a.providerId === WHATIFF_PROVIDER && a.userId === userId)
      : this.list.find(a => a.name === comment.authorName && a.providerId !== WHATIFF_PROVIDER);
    if (found) return found.id;
    const author: PptxAuthor = {
      id: newCommentGuid(),
      name: comment.authorName,
      initials: initialsOf(comment.authorName),
      userId: userId ?? comment.authorName,
      providerId: userId ? WHATIFF_PROVIDER : 'None',
    };
    this.list.push(author);
    this.changed = true;
    return author.id;
  }

  xml(): string {
    const authors = this.list
      .map(
        a =>
          `<p188:author id="${escapeXml(a.id)}" name="${escapeXml(a.name)}"${a.initials ? ` initials="${escapeXml(a.initials)}"` : ''} userId="${escapeXml(a.userId ?? a.name)}" providerId="${escapeXml(a.providerId ?? 'None')}"/>`,
      )
      .join('');
    return `<p188:authorLst xmlns:a="${A}" xmlns:r="${NS.r}" xmlns:p188="${P188}">${authors}</p188:authorLst>`;
  }
}

/** The anchors PowerPoint wrote for each existing comment, by id, kept verbatim when a thread did not move. */
function keptAnchors(text: string | null): Map<string, string> {
  const out = new Map<string, string>();
  if (!text) return out;
  for (const m of text.matchAll(/<p188:cm\b[^>]*?\bid="([^"]+)"[^>]*>([\s\S]*?)(?=<p188:(?:pos|replyLst|txBody|extLst)\b|<\/p188:cm>)/g))
    out.set(m[1].toUpperCase(), m[2]);
  return out;
}

async function writeSlide(pkg: CommentPackage, item: CommentedSlide, authors: AuthorList): Promise<void> {
  const slideDoc = await pkg.xml(item.path);
  if (!slideDoc) return;
  const rels = await pkg.rels(item.path);
  const modernRel = rels.find(r => r.type === REL_MODERN_COMMENTS && !r.external);
  const modernPath = modernRel ? resolveTarget(item.path, modernRel.target) : null;
  const anchors = keptAnchors(modernPath ? await pkg.text(modernPath) : null);
  const before = new Map((item.before ?? []).map(t => [t.id, t]));
  // Legacy comments are rewritten in the modern form, so their part goes.
  const legacy = rels.filter(r => r.type === REL_LEGACY_COMMENTS && !r.external);
  for (const rel of legacy) pkg.remove(resolveTarget(item.path, rel.target));
  let nextRels = rels.filter(r => !legacy.includes(r));

  const cId = slideDoc.getElementsByTagNameNS(P14, 'creationId')[0]?.getAttribute('val') ?? null;
  const sldMk = `<pc:sldMkLst xmlns:pc="${PC}"><pc:docMk/><pc:sldMk${cId ? ` cId="${escapeXml(cId)}"` : ''} sldId="${escapeXml(item.sldId)}"/></pc:sldMkLst>`;
  const cms = item.threads.map(thread => {
    const was = before.get(thread.id);
    const kept =
      was && Math.round(was.anchor.x) === Math.round(thread.anchor.x) && Math.round(was.anchor.y) === Math.round(thread.anchor.y)
        ? anchors.get(thread.id.toUpperCase())
        : undefined;
    const element = thread.anchor.elementId ? item.slide.elements.find(e => e.id === thread.anchor.elementId) : undefined;
    // A kept shape anchor positions the pin from the shape; a written slide anchor from the slide.
    const keptShape = !!kept && kept.includes('deMkLst');
    const origin = keptShape && element ? { x: element.left, y: element.top } : { x: 0, y: 0 };
    const pos = `<p188:pos x="${Math.round((thread.anchor.x - origin.x) * EMU_PER_PT)}" y="${Math.round((thread.anchor.y - origin.y) * EMU_PER_PT)}"/>`;
    const [first, ...rest] = thread.comments;
    const replies = rest.length
      ? `<p188:replyLst>${rest.map(c => `<p188:reply ${attrs(c, authors.idFor(c))}>${txBody(c.text)}</p188:reply>`).join('')}</p188:replyLst>`
      : '';
    const status = thread.resolved ? ' status="resolved"' : '';
    return `<p188:cm ${attrs(first, authors.idFor(first))}${status}>${kept ?? sldMk}${pos}${replies}${txBody(first.text)}</p188:cm>`;
  });

  if (!cms.length) {
    if (modernPath) pkg.remove(modernPath);
    nextRels = nextRels.filter(r => r !== modernRel);
    removeCommentRel(slideDoc);
    pkg.putXml(item.path, slideDoc);
    pkg.putRels(item.path, nextRels);
    return;
  }
  const xml = `<p188:cmLst xmlns:a="${A}" xmlns:r="${NS.r}" xmlns:p188="${P188}">${cms.join('')}</p188:cmLst>`;
  const path =
    modernPath ??
    freePath(
      pkg,
      `ppt/comments/modernComment_${Number(item.sldId).toString(16).toUpperCase()}_${(Number(cId) || 0).toString(16).toUpperCase()}`,
    );
  pkg.put(path, XML_DECLARATION + xml);
  if (!modernRel) {
    const id = freshRelId(nextRels);
    nextRels.push({ id, type: REL_MODERN_COMMENTS, target: relativeTarget(item.path, path), external: false });
    pkg.addOverride(path, CT_MODERN_COMMENTS);
    setCommentRel(slideDoc, id);
    pkg.putXml(item.path, slideDoc);
  }
  if (!modernRel || legacy.length) pkg.putRels(item.path, nextRels);
}

function freePath(pkg: CommentPackage, base: string): string {
  let path = `${base}.xml`;
  for (let n = 2; pkg.has(path); n++) path = `${base}_${n}.xml`;
  return path;
}

function attrs(c: DocComment, authorId: string): string {
  const id = GUID.test(c.id) ? c.id : newCommentGuid();
  const created = new Date(c.createdAt);
  return `id="${escapeXml(id)}" authorId="${escapeXml(authorId)}" created="${(Number.isNaN(created.getTime()) ? new Date() : created).toISOString()}"`;
}

function txBody(text: string): string {
  const paragraphs = text
    .split(/\r\n|\r|\n/)
    .map(line =>
      line
        ? `<a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>${escapeXml(line)}</a:t></a:r></a:p>`
        : '<a:p><a:endParaRPr lang="en-US" dirty="0"/></a:p>',
    )
    .join('');
  return `<p188:txBody><a:bodyPr/><a:lstStyle/>${paragraphs}</p188:txBody>`;
}

/** Links the slide to its comment part: the p188:commentRel extension, the last child of p:sld. */
function setCommentRel(slideDoc: Document, relId: string): void {
  removeCommentRel(slideDoc);
  const sld = slideDoc.documentElement;
  let extLst = kid(sld, P, 'extLst');
  if (!extLst) extLst = sld.appendChild(slideDoc.createElementNS(P, 'p:extLst'));
  const ext = extLst.appendChild(slideDoc.createElementNS(P, 'p:ext'));
  ext.setAttribute('uri', COMMENT_REL_EXT);
  const rel = ext.appendChild(slideDoc.createElementNS(P188, 'p188:commentRel'));
  rel.setAttributeNS(NS.r, 'r:id', relId);
}

function removeCommentRel(slideDoc: Document): void {
  const extLst = kid(slideDoc.documentElement, P, 'extLst');
  for (const ext of kids(extLst, P, 'ext')) if (ext.getAttribute('uri') === COMMENT_REL_EXT) extLst!.removeChild(ext);
  if (extLst && !kids(extLst, P, 'ext').length) extLst.parentNode?.removeChild(extLst);
}

/** The threads on one slide, for writing and comparing. */
export function threadsOn(threads: readonly SlideCommentThread[], slideId: string): SlideCommentThread[] {
  return threads.filter(t => t.anchor.slideId === slideId);
}

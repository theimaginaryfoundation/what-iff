import {
  CommentPerson,
  CommentThread,
  DocComment,
  authorPersonId,
  personIdFromProvider,
  providerUserIdOf,
} from '../comments/comment-threads';
import { NS, OoxmlPackage, Relationship, attr, escapeXml, resolveTarget, serializeXml } from '../shared/ooxml';
import { CUSTOM_DECORATION_COMMENT, CUSTOM_RANGE, DocBody, DocDocument } from './univer-doc-types';

/**
 * Word comments (ADR 0x045) between a .docx and the editor's comment threads.
 *
 * A Word comment is a `w:comment` in comments.xml whose place in the text is marked by
 * commentRangeStart/End and a commentReference with its `w:id`. Threads, replies and resolved
 * state are in commentsExtended.xml, keyed by the comment's last paragraph's `w14:paraId`; a
 * durable id per comment is in commentsIds.xml and its UTC date in commentsExtensible.xml; the
 * authors are in people.xml.
 *
 * In the editor a thread is one Univer comment decoration over its text (the first comment's
 * range; Word gives every reply the same one) plus a CommentThread kept beside the document.
 * Thread and comment ids are Word's durable ids, so a comment keeps its id across saves.
 */

export interface DocxCommentAnchor {
  /** The story the comment is in: '' for the body, else a header or footer id. */
  segmentId: string;
}

export type DocxCommentThread = CommentThread<DocxCommentAnchor>;

export const REL_COMMENTS_EXTENDED = 'http://schemas.microsoft.com/office/2011/relationships/commentsExtended';
export const REL_COMMENTS_IDS = 'http://schemas.microsoft.com/office/2016/09/relationships/commentsIds';
export const REL_COMMENTS_EXTENSIBLE = 'http://schemas.microsoft.com/office/2018/08/relationships/commentsExtensible';
export const REL_PEOPLE = 'http://schemas.microsoft.com/office/2011/relationships/people';
const CT = 'application/vnd.openxmlformats-officedocument.wordprocessingml';
export const COMMENT_PARTS = [
  {
    key: 'comments',
    rel: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments',
    file: 'comments.xml',
    contentType: `${CT}.comments+xml`,
  },
  { key: 'extended', rel: REL_COMMENTS_EXTENDED, file: 'commentsExtended.xml', contentType: `${CT}.commentsExtended+xml` },
  { key: 'ids', rel: REL_COMMENTS_IDS, file: 'commentsIds.xml', contentType: `${CT}.commentsIds+xml` },
  { key: 'extensible', rel: REL_COMMENTS_EXTENSIBLE, file: 'commentsExtensible.xml', contentType: `${CT}.commentsExtensible+xml` },
  { key: 'people', rel: REL_PEOPLE, file: 'people.xml', contentType: `${CT}.people+xml` },
] as const;
export type CommentPartKey = (typeof COMMENT_PARTS)[number]['key'];

const W14 = 'http://schemas.microsoft.com/office/word/2010/wordml';
const W15 = 'http://schemas.microsoft.com/office/word/2012/wordml';
const W16CID = 'http://schemas.microsoft.com/office/word/2016/wordml/cid';
const W16CEX = 'http://schemas.microsoft.com/office/word/2018/wordml/cex';
/** The presence provider people.xml names for WhatIff users; their userId is the WhatIff user id. */
export const WHATIFF_PROVIDER = 'WhatIff';
/** paraId and durableId are 8 hex digits below 0x80000000. */
const WORD_ID = /^[0-9A-F]{8}$/;

/** A new comment id: a Word durable id, so it is the comment's id in the saved file too. */
export function newWordCommentId(): string {
  const random = globalThis.crypto?.getRandomValues?.(new Uint32Array(1))[0] ?? Math.floor(Math.random() * 0xffffffff);
  return Math.max(1, random % 0x7fffffff)
    .toString(16)
    .toUpperCase()
    .padStart(8, '0');
}

/** One w:comment as the file has it. */
export interface WordComment {
  wordId: string;
  /** The editor's id for it: its durable id, else its last paragraph id, else made from w:id. */
  id: string;
  author: string;
  initials?: string;
  /** ISO 8601. */
  date: string;
  text: string;
  lastParaId?: string;
  durableId?: string;
  parentParaId?: string;
  done: boolean;
  /** The element as written, kept for a save that leaves the comment unchanged. */
  xml: string;
}

export interface WordPerson {
  author: string;
  providerId?: string;
  userId?: string;
  xml: string;
}

export interface WordComments {
  comments: WordComment[];
  people: WordPerson[];
  /** The parts, by key, package-absolute, as the main document relates to them. */
  paths: Partial<Record<CommentPartKey, string>>;
  /** comments.xml's root start tag, with the namespaces its comments use. */
  commentsRoot?: string;
  /** people.xml's root start tag. */
  peopleRoot?: string;
}

function elements(root: Element | null | undefined, ns: string, localName: string): Element[] {
  return root ? Array.from(root.getElementsByTagNameNS(ns, localName)) : [];
}

function paragraphText(p: Element): string {
  let text = '';
  for (const el of Array.from(p.getElementsByTagNameNS(NS.w, '*'))) {
    if (el.localName === 't') text += el.textContent ?? '';
    else if (el.localName === 'tab') text += '\t';
    else if (el.localName === 'br' || el.localName === 'cr') text += '\n';
  }
  return text;
}

/** Reads the comment parts the main document relates to; empty when it has none. */
export async function readWordComments(pkg: OoxmlPackage, mainPath: string, rels: Map<string, Relationship>): Promise<WordComments> {
  const paths: WordComments['paths'] = {};
  for (const part of COMMENT_PARTS) {
    const rel = [...rels.values()].find(r => r.type === part.rel && !r.external);
    if (rel) paths[part.key] = resolveTarget(mainPath, rel.target);
  }
  const xml = async (key: CommentPartKey) => (paths[key] ? pkg.xml(paths[key]!) : null);
  const [commentsDoc, extendedDoc, idsDoc, extensibleDoc, peopleDoc] = await Promise.all(
    (['comments', 'extended', 'ids', 'extensible', 'people'] as const).map(xml),
  );

  const extended = new Map<string, { parent?: string; done: boolean }>();
  for (const el of elements(extendedDoc?.documentElement, W15, 'commentEx')) {
    const paraId = attr(el, W15, 'paraId')?.toUpperCase();
    if (!paraId) continue;
    const parent = attr(el, W15, 'paraIdParent')?.toUpperCase();
    extended.set(paraId, { ...(parent ? { parent } : {}), done: attr(el, W15, 'done') === '1' });
  }
  const durable = new Map<string, string>();
  for (const el of elements(idsDoc?.documentElement, W16CID, 'commentId')) {
    const paraId = attr(el, W16CID, 'paraId')?.toUpperCase();
    const id = attr(el, W16CID, 'durableId')?.toUpperCase();
    if (paraId && id) durable.set(paraId, id);
  }
  const utc = new Map<string, string>();
  for (const el of elements(extensibleDoc?.documentElement, W16CEX, 'commentExtensible')) {
    const id = attr(el, W16CEX, 'durableId')?.toUpperCase();
    const date = attr(el, W16CEX, 'dateUtc');
    const parsed = date ? new Date(date) : null;
    if (id && parsed && !Number.isNaN(parsed.getTime())) utc.set(id, parsed.toISOString());
  }

  // Comments kept unchanged are written back from the file's own text: re-serialising them would
  // rename the prefixes their root declares.
  const commentsText = paths.comments ? ((await pkg.text(paths.comments)) ?? '') : '';
  const rawComments = new Map<string, string>();
  for (const m of commentsText.matchAll(/<w:comment\b[^>]*>[\s\S]*?<\/w:comment>/g)) {
    const id = /\bw:id="([^"]*)"/.exec(m[0])?.[1];
    if (id !== undefined && !rawComments.has(id)) rawComments.set(id, m[0]);
  }
  const used = new Set<string>();
  const comments: WordComment[] = [];
  for (const el of elements(commentsDoc?.documentElement, NS.w, 'comment')) {
    const wordId = attr(el, NS.w, 'id');
    if (wordId === null) continue;
    const paragraphs = elements(el, NS.w, 'p');
    const lastParaId = paragraphs.length ? (attr(paragraphs[paragraphs.length - 1], W14, 'paraId')?.toUpperCase() ?? undefined) : undefined;
    const durableId = lastParaId ? durable.get(lastParaId) : undefined;
    let id = durableId ?? lastParaId ?? `W${wordId}`;
    if (used.has(id)) id = `W${wordId}`;
    used.add(id);
    const ex = lastParaId ? extended.get(lastParaId) : undefined;
    const comment: WordComment = {
      wordId,
      id,
      author: attr(el, NS.w, 'author') ?? '',
      date: (durableId && utc.get(durableId)) || wordDate(attr(el, NS.w, 'date')),
      text: paragraphs.map(paragraphText).join('\n'),
      done: ex?.done ?? false,
      xml: rawComments.get(wordId) ?? serializeXml(el),
    };
    const initials = attr(el, NS.w, 'initials');
    if (initials) comment.initials = initials;
    if (lastParaId) comment.lastParaId = lastParaId;
    if (durableId) comment.durableId = durableId;
    if (ex?.parent) comment.parentParaId = ex.parent;
    comments.push(comment);
  }

  const peopleText = paths.people ? ((await pkg.text(paths.people)) ?? '') : '';
  const rawPeople = [...peopleText.matchAll(/<w15:person\b[^>]*?(?:\/>|>[\s\S]*?<\/w15:person>)/g)].map(m => m[0]);
  const people = elements(peopleDoc?.documentElement, W15, 'person').map((el, i) => {
    const presence = elements(el, W15, 'presenceInfo')[0];
    const author = attr(el, W15, 'author') ?? '';
    const raw = rawPeople[i]?.includes(`w15:author="${escapeXml(author)}"`) ? rawPeople[i] : serializeXml(el);
    const person: WordPerson = { author, xml: raw };
    const providerId = attr(presence, W15, 'providerId');
    const userId = attr(presence, W15, 'userId');
    if (providerId) person.providerId = providerId;
    if (userId) person.userId = userId;
    return person;
  });
  const out: WordComments = { comments, people, paths };
  const commentsRoot = /<w:comments\b[^>]*>/.exec(commentsText)?.[0];
  if (commentsRoot && !commentsRoot.endsWith('/>')) out.commentsRoot = commentsRoot;
  const peopleRoot = /<w15:people\b[^>]*>/.exec(peopleText)?.[0];
  if (peopleRoot && !peopleRoot.endsWith('/>')) out.peopleRoot = peopleRoot;
  return out;
}

/** A root start tag that declares every namespace in `needed`, adding those it lacks. */
function withNamespaces(root: string, needed: Record<string, string>): string {
  let out = root;
  for (const [prefix, uri] of Object.entries(needed)) {
    if (!out.includes(` xmlns:${prefix}=`)) out = out.replace(/>$/, ` xmlns:${prefix}="${uri}">`);
  }
  return out;
}

/**
 * Word writes w:date as local time marked "Z"; read as written it is the time the comment was
 * made where it was made, which is the best there is without commentsExtensible's dateUtc.
 */
function wordDate(value: string | null): string {
  const date = value ? new Date(value.replace(/Z$/, '')) : null;
  return date && !Number.isNaN(date.getTime()) ? date.toISOString() : new Date(0).toISOString();
}

/** A comment author as the editor knows them: a WhatIff user by id, anyone else by name. */
export function personFor(author: string, people: readonly WordPerson[]): CommentPerson {
  const person = people.find(p => p.author === author);
  if (person?.providerId === WHATIFF_PROVIDER && person.userId) return { id: personIdFromProvider(person.userId), name: author };
  return { id: authorPersonId(author), name: author };
}

/** Mentions are `@Name` in the text: whoever of the people the file knows is named that way. */
function mentionsIn(text: string, known: readonly CommentPerson[]): DocComment['mentions'] {
  const found = known.filter(p => p.name && text.includes(`@${p.name}`)).map(p => ({ id: p.id, name: p.name }));
  return found.length ? found : undefined;
}

/** The file's comments as threads, without their place in the text yet; and which thread each w:id belongs to. */
export function wordThreads(parsed: WordComments): {
  threads: Map<string, Omit<DocxCommentThread, 'anchor'>>;
  threadOf: Map<string, string>;
} {
  const byPara = new Map(parsed.comments.filter(c => c.lastParaId).map(c => [c.lastParaId!, c]));
  const known = new Map<string, CommentPerson>();
  for (const c of parsed.comments) {
    const person = personFor(c.author, parsed.people);
    known.set(person.id, person);
  }
  const rootOf = (c: WordComment): WordComment => {
    let at = c;
    for (let hops = 0; at.parentParaId && hops < 50; hops++) {
      const parent = byPara.get(at.parentParaId);
      if (!parent || parent === at) break;
      at = parent;
    }
    return at;
  };
  const threads = new Map<string, Omit<DocxCommentThread, 'anchor'>>();
  const threadOf = new Map<string, string>();
  for (const c of parsed.comments) {
    const root = rootOf(c);
    const person = personFor(c.author, parsed.people);
    const comment: DocComment = { id: c.id, authorId: person.id, authorName: c.author || 'Unknown', createdAt: c.date, text: c.text };
    const mentions = mentionsIn(c.text, [...known.values()]);
    if (mentions) comment.mentions = mentions;
    let thread = threads.get(root.id);
    if (!thread) {
      thread = { id: root.id, comments: [] };
      if (root.done) thread.resolved = true;
      threads.set(root.id, thread);
    }
    if (c === root) thread.comments.unshift(comment);
    else thread.comments.push(comment);
    threadOf.set(c.wordId, root.id);
  }
  return { threads, threadOf };
}

/** The text a range covers, as words: control characters out, whitespace collapsed. */
function quoteOf(body: DocBody, start: number, end: number): string {
  return body.dataStream
    .slice(start, end + 1)
    .replace(/[\u0000-\u001f]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

/**
 * Turns the importer's comment ranges into one Univer comment decoration per thread and returns
 * the threads that have a place in the text. A range of a reply, or of a comment in a story
 * comments cannot be shown in (a footnote), is dropped.
 */
export function anchorThreads(document: DocDocument, parsed: WordComments): { threads: DocxCommentThread[]; unplaced: number } {
  const { threads, threadOf } = wordThreads(parsed);
  const placed = new Map<string, DocxCommentThread>();
  const order: { threadId: string; segment: number; start: number }[] = [];
  const stories: { segmentId: string; body: DocBody; anchors: boolean }[] = [
    { segmentId: '', body: document.body, anchors: true },
    ...Object.values(document.headers ?? {}).map(h => ({ segmentId: h.headerId, body: h.body, anchors: true })),
    ...Object.values(document.footers ?? {}).map(f => ({ segmentId: f.footerId, body: f.body, anchors: true })),
    ...Object.values(document.notes ?? {}).map(n => ({ segmentId: n.noteId, body: n.body, anchors: false })),
  ];
  stories.forEach(({ segmentId, body, anchors }, segment) => {
    const ranges = body.customRanges ?? [];
    for (const range of ranges) {
      if (range.rangeType !== CUSTOM_RANGE.COMMENT || !anchors) continue;
      const threadId = threadOf.get(String(range.properties?.['commentId'] ?? ''));
      const thread = threadId ? threads.get(threadId) : undefined;
      if (!thread || placed.has(thread.id) || !thread.comments.length) continue;
      (body.customDecorations ??= []).push({
        id: thread.id,
        type: CUSTOM_DECORATION_COMMENT,
        startIndex: range.startIndex,
        endIndex: range.endIndex,
      });
      placed.set(thread.id, { ...thread, anchor: { segmentId }, quote: quoteOf(body, range.startIndex, range.endIndex) });
      order.push({ threadId: thread.id, segment, start: range.startIndex });
    }
    const rest = ranges.filter(r => r.rangeType !== CUSTOM_RANGE.COMMENT);
    if (rest.length) body.customRanges = rest;
    else delete body.customRanges;
  });
  order.sort((a, b) => a.segment - b.segment || a.start - b.start);
  return { threads: order.map(o => placed.get(o.threadId)!), unplaced: threads.size - placed.size };
}

// --- writing -----------------------------------------------------------------------------------

/** What each comment is in the saved file. */
export interface WrittenComment {
  wordId: string;
  paraId: string;
  durableId: string;
}

export interface CommentWritePlan {
  /** Word ids for each thread's comments, first comment first, by thread id. */
  wordIds: Map<string, string[]>;
  /** The parts to write by key, or null to leave the file's comment parts as they are. */
  parts: Partial<Record<CommentPartKey, string>> | null;
}

function sameComments(threads: readonly DocxCommentThread[], original: WordComments): boolean {
  const flat = threads.flatMap(t => t.comments.map((c, i) => ({ c, t, first: i === 0 })));
  if (flat.length !== original.comments.length) return false;
  const byId = new Map(original.comments.map(c => [c.id, c]));
  return flat.every(({ c, t, first }) => {
    const o = byId.get(c.id);
    return !!o && o.text === c.text && o.author === c.authorName && (!first || o.done === !!t.resolved);
  });
}

/** Word's w:date: local time, marked Z as Word itself writes it. */
function localWordDate(iso: string): string {
  const d = new Date(iso);
  const date = Number.isNaN(d.getTime()) ? new Date() : d;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}Z`;
}

function utcDate(iso: string): string {
  const d = new Date(iso);
  return (Number.isNaN(d.getTime()) ? new Date() : d).toISOString().replace(/\.\d{3}Z$/, 'Z');
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

/** Ids for a comment written fresh: its own when they are Word-shaped, else new ones. */
function freshIds(id: string, taken: Set<string>): { paraId: string; durableId: string } {
  let paraId = WORD_ID.test(id) && parseInt(id, 16) < 0x80000000 ? id : newWordCommentId();
  while (taken.has(paraId)) paraId = newWordCommentId();
  taken.add(paraId);
  return { paraId, durableId: paraId };
}

/**
 * Plans the comment parts for a save. When the threads are the file's own, unchanged, the parts
 * are kept byte for byte and the marks reuse the file's w:ids. Otherwise every part is written
 * again: comments kept as they were reuse their original XML, new and edited ones are written
 * from their text.
 */
export function planCommentParts(
  threads: readonly DocxCommentThread[],
  original: WordComments | null,
  opts: { me?: CommentPerson; styles?: { commentText: boolean; commentReference: boolean } } = {},
): CommentWritePlan {
  const wordIds = new Map<string, string[]>();
  if (original && original.comments.length && sameComments(threads, original)) {
    const byId = new Map(original.comments.map(c => [c.id, c.wordId]));
    for (const t of threads)
      wordIds.set(
        t.id,
        t.comments.map(c => byId.get(c.id)!),
      );
    return { wordIds, parts: null };
  }
  if (!threads.length && !original?.comments.length) return { wordIds, parts: null };

  const originals = new Map((original?.comments ?? []).map(c => [c.id, c]));
  const taken = new Set<string>();
  for (const c of original?.comments ?? []) if (c.lastParaId) taken.add(c.lastParaId);
  const comments: string[] = [];
  const extended: string[] = [];
  const ids: string[] = [];
  const extensible: string[] = [];
  const authors = new Map<string, CommentPerson>();
  let next = 0;
  for (const thread of threads) {
    const list: string[] = [];
    let rootParaId = '';
    thread.comments.forEach((c, i) => {
      const wordId = String(next++);
      list.push(wordId);
      const kept = originals.get(c.id);
      let written: WrittenComment;
      if (kept && kept.text === c.text && kept.lastParaId && kept.author === c.authorName) {
        comments.push(kept.xml.replace(/(<w:comment\b[^>]*?\bw:id=")[^"]*(")/, `$1${wordId}$2`));
        written = { wordId, paraId: kept.lastParaId, durableId: kept.durableId ?? freshIds(c.id, taken).durableId };
      } else {
        const { paraId, durableId } = kept?.lastParaId
          ? { paraId: kept.lastParaId, durableId: kept.durableId ?? kept.lastParaId }
          : freshIds(c.id, taken);
        written = { wordId, paraId, durableId };
        comments.push(commentXml(c, written, opts.styles));
      }
      if (!authors.has(c.authorName)) authors.set(c.authorName, { id: c.authorId, name: c.authorName });
      const parent = i === 0 ? '' : ` w15:paraIdParent="${rootParaId}"`;
      if (i === 0) rootParaId = written.paraId;
      extended.push(`<w15:commentEx w15:paraId="${written.paraId}"${parent} w15:done="${thread.resolved ? 1 : 0}"/>`);
      ids.push(`<w16cid:commentId w16cid:paraId="${written.paraId}" w16cid:durableId="${written.durableId}"/>`);
      extensible.push(`<w16cex:commentExtensible w16cex:durableId="${written.durableId}" w16cex:dateUtc="${utcDate(c.createdAt)}"/>`);
    });
    wordIds.set(thread.id, list);
  }
  const people = [...authors.values()].map(person => {
    const kept = original?.people.find(p => p.author === person.name);
    if (kept) return kept.xml;
    const userId = providerUserIdOf(person.id);
    const presence = userId
      ? `<w15:presenceInfo w15:providerId="${WHATIFF_PROVIDER}" w15:userId="${escapeXml(userId)}"/>`
      : `<w15:presenceInfo w15:providerId="None" w15:userId="${escapeXml(person.name)}"/>`;
    return `<w15:person w15:author="${escapeXml(person.name)}">${presence}</w15:person>`;
  });
  const head = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';
  const mc = `xmlns:mc="${NS.mc}"`;
  return {
    wordIds,
    parts: {
      comments: `${head}${
        original?.commentsRoot
          ? withNamespaces(original.commentsRoot, { w: NS.w, w14: W14 })
          : `<w:comments xmlns:w="${NS.w}" xmlns:w14="${W14}" xmlns:w15="${W15}" xmlns:r="${NS.r}" ${mc} mc:Ignorable="w14 w15">`
      }${comments.join('')}</w:comments>`,
      extended: `${head}<w15:commentsEx xmlns:w15="${W15}" ${mc} mc:Ignorable="w15">${extended.join('')}</w15:commentsEx>`,
      ids: `${head}<w16cid:commentsIds xmlns:w16cid="${W16CID}" ${mc} mc:Ignorable="w16cid">${ids.join('')}</w16cid:commentsIds>`,
      extensible: `${head}<w16cex:commentsExtensible xmlns:w16cex="${W16CEX}" ${mc} mc:Ignorable="w16cex">${extensible.join('')}</w16cex:commentsExtensible>`,
      people: `${head}${original?.peopleRoot ? withNamespaces(original.peopleRoot, { w15: W15 }) : `<w15:people xmlns:w15="${W15}" ${mc} mc:Ignorable="w15">`}${people.join('')}</w15:people>`,
    },
  };
}

function commentXml(c: DocComment, written: WrittenComment, styles?: { commentText: boolean; commentReference: boolean }): string {
  const lines = c.text.split(/\r\n|\r|\n/);
  const pPr = styles?.commentText ? '<w:pPr><w:pStyle w:val="CommentText"/></w:pPr>' : '';
  const refRPr = styles?.commentReference ? '<w:rPr><w:rStyle w:val="CommentReference"/></w:rPr>' : '';
  const paragraphs = lines.map((line, i) => {
    const last = i === lines.length - 1;
    // Only the last paragraph's id is the comment's; the others need ids of their own.
    const paraId = last ? written.paraId : (parseInt(written.paraId, 16) ^ ((i + 1) * 0x10000)).toString(16).toUpperCase().padStart(8, '0');
    const annotation = i === 0 ? `<w:r>${refRPr}<w:annotationRef/></w:r>` : '';
    const run = line ? `<w:r><w:t xml:space="preserve">${escapeXml(line)}</w:t></w:r>` : '';
    return `<w:p w14:paraId="${paraId}" w14:textId="77777777">${pPr}${annotation}${run}</w:p>`;
  });
  return (
    `<w:comment w:id="${written.wordId}" w:author="${escapeXml(c.authorName)}" w:date="${localWordDate(c.createdAt)}" w:initials="${escapeXml(initialsOf(c.authorName))}">` +
    `${paragraphs.join('')}</w:comment>`
  );
}

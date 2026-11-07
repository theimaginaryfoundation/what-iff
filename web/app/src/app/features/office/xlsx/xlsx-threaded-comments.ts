import { parseCellA1 } from '../shared/a1';
import { escapeXml, serializeXml } from '../shared/ooxml';

/**
 * Threaded comments between SpreadsheetML (xl/threadedComments, xl/persons) and Univer's comment
 * model (ADR 0x043). Excel also keeps a legacy note per thread, with placeholder text for older
 * versions and an author of "tc={id}" tying it to the thread; the exporter writes those too.
 */

export const RESOURCE_THREAD_COMMENTS = 'SHEET_UNIVER_THREAD_COMMENT_PLUGIN';
const TC_NS = 'http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments';
const MAIN_NS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
export const REL_THREADED_COMMENT = 'http://schemas.microsoft.com/office/2017/10/relationships/threadedComment';
export const REL_PERSON = 'http://schemas.microsoft.com/office/2017/10/relationships/person';
export const CT_THREADED_COMMENT = 'application/vnd.ms-excel.threadedcomments+xml';
export const CT_PERSON = 'application/vnd.ms-excel.person+xml';
const GUID = /^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$/;
const FALLBACK_AUTHOR = 'WhatIff user';

export interface ThreadPerson {
  id: string;
  displayName: string;
  userId?: string;
  providerId?: string;
}

interface CommentBody {
  dataStream: string;
  textRuns: unknown[];
  paragraphs: { startIndex: number }[];
}

export interface UniverComment {
  id: string;
  threadId: string;
  ref: string;
  dT: string;
  personId: string;
  authorName?: string;
  text: CommentBody;
  resolved?: boolean;
  parentId?: string;
  unitId: string;
  subUnitId: string;
  children?: UniverComment[];
}

export interface ThreadExtras {
  persons: ThreadPerson[];
  /** A comment's <mentions>, kept while its text stays the same. */
  mentions: Record<string, { text: string; xml: string }>;
}

function kids(parent: Element | null | undefined, localName: string): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter((n): n is Element => n.nodeType === 1 && (n as Element).localName === localName);
}

export function parsePersons(doc: Document | null): ThreadPerson[] {
  if (!doc) return [];
  return kids(doc.documentElement, 'person').flatMap(el => {
    const id = el.getAttribute('id');
    if (!id) return [];
    const person: ThreadPerson = { id, displayName: el.getAttribute('displayName') ?? '' };
    const userId = el.getAttribute('userId');
    if (userId) person.userId = userId;
    const providerId = el.getAttribute('providerId');
    if (providerId) person.providerId = providerId;
    return [person];
  });
}

function bodyOf(text: string): CommentBody {
  // Univer ends a body with a paragraph and a section; line breaks inside it are paragraphs.
  const inner = text.replace(/\r\n|\n/g, '\r');
  const paragraphs: { startIndex: number }[] = [];
  for (let i = 0; i < inner.length; i++) if (inner[i] === '\r') paragraphs.push({ startIndex: i });
  paragraphs.push({ startIndex: inner.length });
  return { dataStream: `${inner}\r\n`, textRuns: [], paragraphs };
}

function textOf(body: { dataStream?: string } | undefined): string {
  return (body?.dataStream ?? '').replace(/\r\n$/, '').replace(/\r/g, '\n').replace(/[\u001e\u001f]/g, '');
}

/** One sheet's threads as Univer's snapshot holds them: roots with their replies in `children`. */
export function parseThreadedComments(
  doc: Document | null,
  persons: readonly ThreadPerson[],
  unitId: string,
  subUnitId: string,
  mentions: ThreadExtras['mentions'],
): UniverComment[] {
  if (!doc) return [];
  const names = new Map(persons.map(p => [p.id.toUpperCase(), p.displayName]));
  const roots = new Map<string, UniverComment>();
  const replies: { parent: string; comment: UniverComment }[] = [];
  for (const el of kids(doc.documentElement, 'threadedComment')) {
    const id = el.getAttribute('id');
    const ref = el.getAttribute('ref');
    if (!id || !ref) continue;
    const parent = el.getAttribute('parentId');
    const personId = el.getAttribute('personId') ?? '';
    const text = kids(el, 'text')[0]?.textContent ?? '';
    const comment: UniverComment = {
      id,
      threadId: parent ?? id,
      ref,
      dT: el.getAttribute('dT') ?? '',
      personId,
      authorName: names.get(personId.toUpperCase()) || undefined,
      text: bodyOf(text),
      unitId,
      subUnitId,
    };
    const mention = kids(el, 'mentions')[0];
    if (mention) mentions[id] = { text, xml: serializeXml(mention).replace(/\sxmlns(:\w+)?="[^"]*"/g, '') };
    if (parent) {
      comment.parentId = parent;
      replies.push({ parent, comment });
    } else {
      if (el.getAttribute('done') === '1') comment.resolved = true;
      comment.children = [];
      roots.set(id, comment);
    }
  }
  for (const { parent, comment } of replies) {
    const root = roots.get(parent);
    if (root) root.children!.push(comment);
  }
  return [...roots.values()];
}

/** The placeholder Excel writes into a thread's legacy note. */
export function legacyNoteText(root: UniverComment): string {
  const replies = (root.children ?? []).map(c => `\nReply:\n    ${textOf(c.text)}`).join('\n');
  return (
    '[Threaded comment]\n\nYour version of Excel allows you to read this threaded comment; however, any edits to it will get removed if the file is opened in a newer version of Excel. Learn more: https://go.microsoft.com/fwlink/?linkid=870924\n\n' +
    `Comment:\n    ${textOf(root.text)}\n${replies}`
  );
}

function newGuid(): string {
  const random = globalThis.crypto?.randomUUID?.() ?? 'xxxxxxxx-xxxx-4xxx-8xxx-xxxxxxxxxxxx'.replace(/x/g, () => Math.floor(Math.random() * 16).toString(16));
  return `{${random.toUpperCase()}}`;
}

/** Ids as Excel requires them: comments and people made in Univer are given a GUID here. */
export class ThreadIds {
  private readonly map = new Map<string, string>();

  guid(id: string): string {
    if (GUID.test(id)) return id;
    let out = this.map.get(id);
    if (!out) {
      out = newGuid();
      this.map.set(id, out);
    }
    return out;
  }
}

function pad(n: number, width = 2): string {
  return String(n).padStart(width, '0');
}

/** Excel's dT: local time with no zone. */
function excelDate(value: string): string {
  const date = new Date(value);
  const d = Number.isNaN(date.getTime()) ? new Date() : date;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(Math.floor(d.getMilliseconds() / 10))}`;
}

export interface ThreadedWriteState {
  ids: ThreadIds;
  /** Everyone named by a comment written so far, and everyone the file already had. */
  persons: Map<string, ThreadPerson>;
  mentions: ThreadExtras['mentions'];
}

function personGuid(comment: UniverComment, state: ThreadedWriteState): string {
  const raw = comment.personId || comment.authorName || FALLBACK_AUTHOR;
  const id = state.ids.guid(raw);
  if (!state.persons.has(id)) {
    const name = comment.authorName || FALLBACK_AUTHOR;
    state.persons.set(id, { id, displayName: name, userId: name, providerId: 'None' });
  }
  return id;
}

function commentXml(comment: UniverComment, state: ThreadedWriteState, root: UniverComment): string {
  const id = state.ids.guid(comment.id);
  const text = textOf(comment.text);
  const parent = comment === root ? '' : ` parentId="${state.ids.guid(root.id)}"`;
  const done = comment === root && comment.resolved ? ' done="1"' : comment === root ? ' done="0"' : '';
  const kept = state.mentions[comment.id];
  return (
    `<threadedComment ref="${escapeXml(root.ref)}" dT="${excelDate(comment.dT)}" personId="${personGuid(comment, state)}" id="${id}"${parent}${done}>` +
    `<text>${escapeXml(text)}</text>${kept && kept.text === text ? kept.xml : ''}</threadedComment>`
  );
}

/** A threadedComments part for one sheet, or null with nothing to write. */
export function threadedCommentsXml(threads: readonly UniverComment[], state: ThreadedWriteState): string | null {
  const body = cellThreads(threads)
    .map(root => [root, ...(root.children ?? [])].map(c => commentXml(c, state, root)).join(''))
    .join('');
  return body
    ? `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><ThreadedComments xmlns="${TC_NS}" xmlns:x="${MAIN_NS}">${body}</ThreadedComments>`
    : null;
}

export function personsXml(persons: Iterable<ThreadPerson>): string {
  const body = [...persons]
    .map(p => `<person displayName="${escapeXml(p.displayName)}" id="${p.id}" userId="${escapeXml(p.userId ?? p.displayName)}" providerId="${escapeXml(p.providerId ?? 'None')}"/>`)
    .join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><personList xmlns="${TC_NS}" xmlns:x="${MAIN_NS}">${body}</personList>`;
}

/** Threads on a cell; comments anchored to a drawing or a range have no place in the Excel format. */
export function cellThreads(threads: readonly UniverComment[]): UniverComment[] {
  return threads.filter(t => parseCellA1(t.ref) !== null);
}

/** Ties the legacy notes ExcelJS wrote to their threads, as Excel expects. */
export function linkLegacyComments(xml: string, threads: readonly UniverComment[], ids: ThreadIds): string {
  const byRef = new Map(cellThreads(threads).map(t => [t.ref, ids.guid(t.id)]));
  if (!byRef.size) return xml;
  const authors = (/<authors>([\s\S]*?)<\/authors>/.exec(xml)?.[1] ?? '').match(/<author>[\s\S]*?<\/author>/g) ?? [];
  const list = [...authors];
  let out = xml.replace(/<comment\b([^>]*)>/g, (whole, attrs: string) => {
    const ref = /\bref="([^"]+)"/.exec(attrs)?.[1];
    const id = ref ? byRef.get(ref) : undefined;
    if (!id) return whole;
    list.push(`<author>tc=${id}</author>`);
    return `<comment${attrs.replace(/\bauthorId="\d+"/, `authorId="${list.length - 1}"`)} xr:uid="${id}">`;
  });
  out = out.replace(/<authors>[\s\S]*?<\/authors>/, `<authors>${list.join('')}</authors>`);
  if (!/xmlns:xr=/.test(out)) {
    out = out.replace(
      /<comments\b/,
      '<comments xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="xr" xmlns:xr="http://schemas.microsoft.com/office/spreadsheetml/2014/revision"',
    );
  }
  return out;
}

import { CommentPerson, CommentThread, DocComment, authorPersonId, userPersonId } from '../comments/comment-threads';
import { cellA1, parseCellA1 } from '../shared/a1';
import { UniverWorkbook, readResource, writeResource } from './univer-sheet-types';
import { RESOURCE_THREAD_COMMENTS, ThreadPerson, UniverComment, bodyOf, cellThreads, textOf } from './xlsx-threaded-comments';

/**
 * Spreadsheet comments (ADR 0x045) as the editor keeps them: threads beside the workbook, each
 * on one cell, which carries the thread's id in its custom data so the anchor moves with the cell
 * when rows and columns are inserted, deleted or moved. The xlsx reader and writer
 * (xlsx-threaded-comments.ts) work in Univer's thread-comment resource; this converts between the
 * two at load and save.
 */

export interface SheetCommentAnchor {
  sheetId: string;
}

export type SheetCommentThread = CommentThread<SheetCommentAnchor>;

/** The key a commented cell's custom data holds its thread's id under. */
export const COMMENT_CELL_KEY = 'whatiffCommentThread';
/** The presence provider persons.xml names for WhatIff users; their userId is the WhatIff user id. */
export const WHATIFF_PROVIDER = 'WhatIff';

function personOf(person: ThreadPerson | undefined, fallbackName: string): CommentPerson {
  if (person?.providerId === WHATIFF_PROVIDER && person.userId) return { id: userPersonId(person.userId), name: person.displayName };
  const name = person?.displayName || fallbackName || 'Unknown';
  return { id: authorPersonId(name), name };
}

function isoDate(value: string): string {
  // Excel's dT is local time with no zone, which Date reads as local time.
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? new Date(0).toISOString() : date.toISOString();
}

/**
 * Takes the threads out of a workbook as read: the thread-comment resource becomes threads, and
 * each thread's cell is tagged with its id.
 */
export function takeSheetComments(workbook: UniverWorkbook, persons: readonly ThreadPerson[]): SheetCommentThread[] {
  const bySheet = readResource<Record<string, UniverComment[]>>(workbook, RESOURCE_THREAD_COMMENTS) ?? {};
  workbook.resources = workbook.resources.filter(r => r.name !== RESOURCE_THREAD_COMMENTS);
  const byId = new Map(persons.map(p => [p.id.toUpperCase(), p]));
  const known = persons.map(p => personOf(p, p.displayName));
  const threads: SheetCommentThread[] = [];
  for (const sheetId of workbook.sheetOrder) {
    const sheet = workbook.sheets[sheetId];
    for (const root of cellThreads(bySheet[sheetId] ?? [])) {
      const at = parseCellA1(root.ref);
      if (!sheet || !at) continue;
      const row = (sheet.cellData[at.row] ??= {});
      const cell = (row[at.col] ??= {});
      cell.custom = { ...(cell.custom ?? {}), [COMMENT_CELL_KEY]: root.id };
      const comment = (c: UniverComment): DocComment => {
        const person = personOf(byId.get(c.personId.toUpperCase()), c.authorName ?? '');
        const text = textOf(c.text);
        const out: DocComment = { id: c.id, authorId: person.id, authorName: person.name, createdAt: isoDate(c.dT), text };
        const mentions = known.filter(p => p.name && text.includes(`@${p.name}`)).map(p => ({ id: p.id, name: p.name }));
        if (mentions.length) out.mentions = mentions;
        return out;
      };
      const thread: SheetCommentThread = {
        id: root.id,
        anchor: { sheetId },
        quote: root.ref,
        comments: [root, ...(root.children ?? [])].map(comment),
      };
      if (root.resolved) thread.resolved = true;
      threads.push(thread);
    }
  }
  return threads;
}

/**
 * Where each thread's cell is now, by thread id. Where a copied cell carries the tag too, the cell
 * the thread names (its quote: the cell's A1 reference, on its sheet) wins, else the first.
 */
export function commentCells(
  workbook: UniverWorkbook,
  threads: readonly SheetCommentThread[] = [],
): Map<string, { sheetId: string; row: number; col: number }> {
  const named = new Map(threads.map(t => [t.id, t]));
  const out = new Map<string, { sheetId: string; row: number; col: number }>();
  for (const sheetId of workbook.sheetOrder) {
    const cells = workbook.sheets[sheetId]?.cellData ?? {};
    for (const [r, row] of Object.entries(cells)) {
      for (const [c, cell] of Object.entries(row ?? {})) {
        const id = cell?.custom?.[COMMENT_CELL_KEY];
        if (typeof id !== 'string') continue;
        const at = { sheetId, row: Number(r), col: Number(c) };
        const thread = named.get(id);
        const isNamed = !!thread && thread.anchor.sheetId === sheetId && thread.quote === cellA1(at.row, at.col);
        if (!out.has(id) || isNamed) out.set(id, at);
      }
    }
  }
  return out;
}

/**
 * Puts the threads back into a workbook to save: each thread whose cell is still tagged is written
 * on that cell, in the thread-comment resource the xlsx writer reads; a thread whose cell is gone
 * is not written.
 */
export function putSheetComments(workbook: UniverWorkbook, threads: readonly SheetCommentThread[]): void {
  const cells = commentCells(workbook, threads);
  const bySheet: Record<string, UniverComment[]> = {};
  for (const thread of threads) {
    const at = cells.get(thread.id);
    if (!at || !thread.comments.length) continue;
    const ref = cellA1(at.row, at.col);
    const [first, ...replies] = thread.comments;
    const comment = (c: DocComment, parent?: string): UniverComment => ({
      id: c.id,
      threadId: thread.id,
      ref,
      dT: c.createdAt,
      personId: c.authorId,
      authorName: c.authorName,
      text: bodyOf(c.text) as UniverComment['text'],
      unitId: workbook.id,
      subUnitId: at.sheetId,
      ...(parent ? { parentId: parent } : {}),
    });
    const root: UniverComment = { ...comment(first), children: replies.map(c => comment(c, thread.id)) };
    if (thread.resolved) root.resolved = true;
    (bySheet[at.sheetId] ??= []).push(root);
  }
  workbook.resources = workbook.resources.filter(r => r.name !== RESOURCE_THREAD_COMMENTS);
  if (Object.keys(bySheet).length) writeResource(workbook, RESOURCE_THREAD_COMMENTS, bySheet);
}

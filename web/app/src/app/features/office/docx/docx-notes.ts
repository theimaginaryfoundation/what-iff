import { NS, attr, children } from '../shared/ooxml';
import { BodyBuilder, BodyContext } from './docx-body';
import { DocDrawing, DocNote } from './univer-doc-types';

/**
 * Footnote and endnote stories (footnotes.xml, endnotes.xml) to and from Univer's `notes`. The
 * body refers to a story through a FOOTNOTE or ENDNOTE range over one object character; Univer
 * numbers the references and lays the stories out. Word's separator stories are not kept: the
 * standard ones are written back.
 */

export interface ParsedNotes {
  notes: Record<string, DocNote>;
  drawings: Record<string, DocDrawing>;
  drawingsOrder: string[];
}

/** The model id of a note: its kind and Word id, so references resolve without a lookup table. */
export function noteIdFor(kind: 'footnote' | 'endnote', wordId: string): string {
  return `${kind === 'footnote' ? 'fn' : 'en'}${wordId}`;
}

export function parseNotes(doc: Document, kind: 'footnote' | 'endnote', builder: (ctx: Partial<BodyContext>) => BodyBuilder): ParsedNotes {
  const out: ParsedNotes = { notes: {}, drawings: {}, drawingsOrder: [] };
  for (const el of children(doc.documentElement, NS.w, kind)) {
    const type = attr(el, NS.w, 'type');
    if (type && type !== 'normal') continue;
    const wordId = attr(el, NS.w, 'id');
    if (!wordId) continue;
    const b = builder({});
    b.blocks(el);
    const body = b.finish();
    const noteId = noteIdFor(kind, wordId);
    const note: DocNote = { noteId, type: kind, body };
    if (Object.keys(b.tableSource).length) note.tableSource = b.tableSource;
    if (b.drawingsOrder.length) {
      note.drawings = b.drawings;
      note.drawingsOrder = b.drawingsOrder;
      Object.assign(out.drawings, b.drawings);
      out.drawingsOrder.push(...b.drawingsOrder);
    }
    out.notes[noteId] = note;
  }
  return out;
}

const SEPARATORS = (kind: 'footnote' | 'endnote') =>
  `<w:${kind} w:type="separator" w:id="-1"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:separator/></w:r></w:p></w:${kind}>` +
  `<w:${kind} w:type="continuationSeparator" w:id="0"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:continuationSeparator/></w:r></w:p></w:${kind}>`;

/** The notes part: the standard separators, then each referenced story with its Word id. */
export function notesPartXml(kind: 'footnote' | 'endnote', stories: { wordId: string; xml: string }[], namespaces: string): string {
  const tag = kind === 'footnote' ? 'w:footnotes' : 'w:endnotes';
  const body = stories.map(s => `<w:${kind} w:id="${s.wordId}">${s.xml}</w:${kind}>`).join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><${tag} ${namespaces}>${SEPARATORS(kind)}${body}</${tag}>`;
}

/** A Word note id for a story: the numeric part of its model id when it has one, else a fresh number. */
export function wordNoteIds(noteIds: string[]): Map<string, string> {
  const out = new Map<string, string>();
  const used = new Set<number>();
  for (const id of noteIds) {
    const m = /^(?:fn|en)(\d+)$/.exec(id);
    const n = m ? Number(m[1]) : NaN;
    if (Number.isInteger(n) && n > 0 && !used.has(n)) {
      used.add(n);
      out.set(id, String(n));
    }
  }
  let next = 1;
  for (const id of noteIds) {
    if (out.has(id)) continue;
    while (used.has(next)) next++;
    used.add(next);
    out.set(id, String(next++));
  }
  return out;
}

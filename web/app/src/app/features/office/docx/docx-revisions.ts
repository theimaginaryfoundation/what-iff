import { NS, child, elementChildren, escapeXml, wval } from '../shared/ooxml';
import { stripRootNamespaces } from './docx-styles';
import { DocCustomRange, TOKEN } from './univer-doc-types';

/**
 * Content controls and tracked changes, which ride through an edit as custom ranges (ADR 0x043):
 * their XML is kept and the ranges move with the text, so the exporter can wrap the same content again.
 *
 * - A content control is an SDT range: `rawSdtPr` and `rawSdtEndPr` are Univer's own slots for this.
 * - A tracked insertion is a CUSTOM range holding its opening tag. Univer draws CUSTOM ranges coloured
 *   and underlined, which marks the insertion on screen without touching the run's real formatting.
 * - A run's `w:rPrChange` is a CUSTOM range too, made invisible with `active: false` and `textColorMode`.
 * - A tracked deletion has no text in the stream; it is an opaque run, as text boxes are (docx-body.ts).
 */

export const REVISIONS_NOTICE = 'Tracked changes are kept; they can be accepted or rejected in Word';
export const CONTROLS_NOTICE = 'Content controls keep their settings; their pickers open in Word';
export const BOUND_CONTROLS_NOTICE = "Content controls bound to the document's data refresh from it in Word, so edits to them here may not stick";

/** Serialises an element without the namespace declarations the part's root already has. */
export function fragment(el: Element): string {
  return stripRootNamespaces(new XMLSerializer().serializeToString(el));
}

/** An element's opening tag with its attributes as written, such as `<w:ins w:id="1" w:author="A">`. */
export function openTagOf(el: Element): string {
  const attrs = Array.from(el.attributes)
    .filter(a => a.name !== 'xmlns' && !a.name.startsWith('xmlns:'))
    .map(a => ` ${a.name}="${escapeXml(a.value)}"`)
    .join('');
  return `<w:${el.localName}${attrs}>`;
}

const SDT_KINDS: [string, string][] = [
  ['checkbox', 'checkbox'],
  ['dropDownList', 'dropDownList'],
  ['comboBox', 'comboBox'],
  ['date', 'date'],
  ['text', 'text'],
  ['picture', 'picture'],
  ['group', 'group'],
  ['docPartObj', 'docPartObj'],
  ['docPartList', 'docPartObj'],
  ['repeatingSection', 'repeatingSection'],
  ['repeatingSectionItem', 'repeatingSectionItem'],
];

/** The properties of an SDT range for a `w:sdt`: enough for Univer's lock rules, and the XML to write back. */
export function sdtProperties(sdt: Element, placement: 'inline' | 'block' | 'row' | 'cell'): Record<string, unknown> {
  const pr = child(sdt, NS.w, 'sdtPr');
  const endPr = child(sdt, NS.w, 'sdtEndPr');
  const names = pr ? elementChildren(pr).map(c => c.localName) : [];
  const kind = SDT_KINDS.find(([local]) => names.includes(local))?.[1] ?? 'richText';
  const out: Record<string, unknown> = { kind, placement };
  if (pr) {
    out['rawSdtPr'] = fragment(pr);
    const alias = wval(child(pr, NS.w, 'alias'));
    const tag = wval(child(pr, NS.w, 'tag'));
    const id = wval(child(pr, NS.w, 'id'));
    const lock = wval(child(pr, NS.w, 'lock'));
    if (alias) out['alias'] = alias;
    if (tag) out['tag'] = tag;
    if (id) out['ooxmlId'] = id;
    if (lock) out['lock'] = lock;
    if (child(pr, NS.w, 'showingPlcHdr')) out['showingPlaceholder'] = true;
  }
  if (endPr) out['rawSdtEndPr'] = fragment(endPr);
  return out;
}

export function isBound(sdt: Element): boolean {
  return !!child(child(sdt, NS.w, 'sdtPr'), NS.w, 'dataBinding');
}

const STRUCTURAL = new Set<string>([
  TOKEN.PARAGRAPH,
  TOKEN.SECTION_BREAK,
  TOKEN.TABLE_START,
  TOKEN.TABLE_ROW_START,
  TOKEN.TABLE_CELL_START,
  TOKEN.TABLE_CELL_END,
  TOKEN.TABLE_ROW_END,
  TOKEN.TABLE_END,
]);

/** The stretches of a range lying within single paragraphs: run-level XML cannot cross a paragraph. */
export function paragraphSegments(stream: string, start: number, end: number): { start: number; end: number }[] {
  const out: { start: number; end: number }[] = [];
  let from = -1;
  for (let i = start; i <= end && i < stream.length; i++) {
    if (STRUCTURAL.has(stream[i])) {
      if (from !== -1) out.push({ start: from, end: i - 1 });
      from = -1;
    } else if (from === -1) from = i;
  }
  if (from !== -1) out.push({ start: from, end: Math.min(end, stream.length - 1) });
  return out;
}

/** The text a range shows, without structural tokens. */
function rangeText(stream: string, range: DocCustomRange): string {
  return Array.from(stream.slice(range.startIndex, range.endIndex + 1))
    .filter(ch => !STRUCTURAL.has(ch))
    .join('');
}

/**
 * The tags around a content control. Text typed over a placeholder is no longer the placeholder, so
 * `w:showingPlcHdr` goes, or Word would treat the typed text as a prompt and discard it. `keepId` is
 * false for a control an edit has split, whose copies must not share an id.
 */
export function sdtTags(stream: string, range: DocCustomRange, keepId = true): { open: string; close: string } {
  let pr = String(range.properties?.['rawSdtPr'] ?? '');
  const placeholder = range.properties?.['docxPlaceholderText'];
  if (placeholder !== undefined && rangeText(stream, range) !== placeholder) pr = pr.replace(/<w:showingPlcHdr\s*\/>/, '');
  if (!keepId) pr = pr.replace(/<w:id\b[^>]*\/>/, '');
  const endPr = String(range.properties?.['rawSdtEndPr'] ?? '');
  return { open: `<w:sdt>${pr}${endPr}<w:sdtContent>`, close: '</w:sdtContent></w:sdt>' };
}

/** Records what a placeholder shows, to tell later whether it was typed over. */
export function notePlaceholder(stream: string, range: DocCustomRange): void {
  if (range.properties?.['showingPlaceholder']) range.properties['docxPlaceholderText'] = rangeText(stream, range);
}

/** Puts kept properties in a run's rPr, keeping the schema's order: revision marks first, `w:rPrChange` last. */
export function withRunExtras(rPr: string, first: string, last: string): string {
  if (!first && !last) return rPr;
  const inner = rPr.replace(/^<w:rPr>/, '').replace(/<\/w:rPr>$/, '');
  return `<w:rPr>${first}${inner}${last}</w:rPr>`;
}

const MARK_REVISIONS = new Set(['ins', 'del', 'moveFrom', 'moveTo']);

/** The revision marks on a paragraph mark (`w:pPr/w:rPr`), which the text style cannot hold. */
export function markExtras(pPr: Element | null | undefined): { first?: string; last?: string } {
  const rPr = child(pPr, NS.w, 'rPr');
  if (!rPr) return {};
  const pick = (test: (name: string) => boolean) =>
    elementChildren(rPr)
      .filter(c => c.namespaceURI === NS.w && test(c.localName))
      .map(fragment)
      .join('');
  const first = pick(n => MARK_REVISIONS.has(n));
  const last = pick(n => n === 'rPrChange');
  return { ...(first ? { first } : {}), ...(last ? { last } : {}) };
}

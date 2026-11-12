/**
 * Comment anchors over formatted Markdown (ADR 0x045). The rendering has no character-for-character
 * relation to the source (`**bold**` is shown as "bold", a link as its label), so a range is found
 * by its words: the source range's words, in order, with anything that is not a word between them
 * (markup, punctuation, line breaks), at the place nearest where the range is in the source. The
 * same works backwards for a selection in the rendering.
 */

import { CommentTextMap } from './text-dom-map';

const WORD = /[\p{L}\p{N}_]+/gu;

interface Piece {
  node: Text;
  /** Where the node's text starts in the joined text. */
  at: number;
}

/** The words of a text, as a pattern that matches them in order with any non-words between. */
function wordsPattern(text: string): RegExp | null {
  const words = text.match(WORD);
  if (!words?.length) return null;
  const escaped = words.map(w => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  return new RegExp(escaped.join('[^\\p{L}\\p{N}_]+'), 'gu');
}

/** The match of pattern in text nearest to the position `near` (a fraction of the text). */
function nearest(pattern: RegExp, text: string, near: number): { start: number; end: number } | null {
  let best: { start: number; end: number } | null = null;
  const target = near * text.length;
  for (const m of text.matchAll(pattern)) {
    const start = m.index ?? 0;
    if (!best || Math.abs(start - target) < Math.abs(best.start - target)) best = { start, end: start + m[0].length };
  }
  return best;
}

export class RenderedTextMap implements CommentTextMap {
  private readonly pieces: Piece[] = [];
  private readonly joined: string;

  /** The source with what is never shown as text (link targets, HTML tags) blanked, offsets kept. */
  private readonly source: string;

  constructor(root: HTMLElement, source: string) {
    this.source = source
      .replace(/\]\([^)]*\)/g, m => `]${' '.repeat(m.length - 1)}`)
      .replace(/^\s*\[[^\]]+\]:\s*\S+.*$/gm, m => ' '.repeat(m.length))
      .replace(/<[^>\n]+>/g, m => ' '.repeat(m.length));
    let joined = '';
    const walker = root.ownerDocument.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
      this.pieces.push({ node, at: joined.length });
      joined += node.data;
    }
    this.joined = joined;
  }

  private point(at: number, end: boolean): { node: Text; offset: number } | null {
    for (let i = this.pieces.length - 1; i >= 0; i--) {
      const piece = this.pieces[i];
      if (at > piece.at || (at === piece.at && !end)) return { node: piece.node, offset: Math.min(at - piece.at, piece.node.data.length) };
    }
    return this.pieces[0] ? { node: this.pieces[0].node, offset: 0 } : null;
  }

  private joinedOffset(node: Node, offset: number): number | null {
    const piece = this.pieces.find(p => p.node === node);
    return piece ? piece.at + offset : null;
  }

  /** The rendering of the source range [start, end): its words where they are shown. */
  range(start: number, end: number): Range | null {
    const pattern = wordsPattern(this.source.slice(start, end));
    const at = pattern && nearest(pattern, this.joined, start / Math.max(1, this.source.length));
    if (!at) return null;
    const from = this.point(at.start, false);
    const to = this.point(at.end, true);
    if (!from || !to) return null;
    const range = from.node.ownerDocument.createRange();
    range.setStart(from.node, from.offset);
    range.setEnd(to.node, to.offset);
    return range.collapsed ? null : range;
  }

  /** The source range a selection in the rendering is of: its words, where they are in the source. */
  sourceRange(selection: Range): { start: number; end: number } | null {
    const from = this.joinedOffset(selection.startContainer, selection.startOffset);
    const to = this.joinedOffset(selection.endContainer, selection.endOffset);
    if (from === null || to === null || to <= from) return null;
    const pattern = wordsPattern(this.joined.slice(from, to));
    return pattern ? nearest(pattern, this.source, from / Math.max(1, this.joined.length)) : null;
  }

  /** The source offset of a point in the rendering (a click): the word there, found in the source. */
  offsetOf(node: Node, offset: number): number | null {
    const at = this.joinedOffset(node, offset);
    if (at === null) return null;
    let start = at;
    let end = at;
    const isWord = (c: string | undefined) => !!c && /[\p{L}\p{N}_]/u.test(c);
    while (start > 0 && isWord(this.joined[start - 1])) start--;
    while (end < this.joined.length && isWord(this.joined[end])) end++;
    if (end <= start) return null;
    const pattern = wordsPattern(this.joined.slice(start, end));
    const found = pattern && nearest(pattern, this.source, at / Math.max(1, this.joined.length));
    return found ? found.start + Math.min(at - start, found.end - found.start - 1) : null;
  }
}

/**
 * Text positions for comments on a range of a shape's text (ADR 0x045), counted as PowerPoint
 * counts them and as `plainText` writes them: the characters of each run, one character for a line
 * break, and one between paragraphs. These map that count to and from the text the canvas renders
 * (text-dom.ts): `<p>` per paragraph, `<br>` for a line break, and a lone `<br>` in an empty
 * paragraph that is not text at all.
 */

interface Segment {
  node: Node;
  /** Where it starts in the plain text. */
  start: number;
  /** Characters it stands for: a text node's length, or 1 for a line break. */
  length: number;
  text: boolean;
}

function segments(root: HTMLElement): { list: Segment[]; total: number } {
  const list: Segment[] = [];
  let at = 0;
  const paragraphs = Array.from(root.querySelectorAll('p'));
  paragraphs.forEach((p, i) => {
    if (i > 0) at += 1;
    const placeholder = p.childNodes.length === 1 && (p.firstChild as Element).nodeName === 'BR';
    if (placeholder) return;
    const walker = root.ownerDocument.createTreeWalker(p, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (node.nodeType === Node.TEXT_NODE) {
        const length = node.textContent?.length ?? 0;
        list.push({ node, start: at, length, text: true });
        at += length;
      } else if (node.nodeName === 'BR') {
        list.push({ node, start: at, length: 1, text: false });
        at += 1;
      }
    }
  });
  return { list, total: at };
}

/** The plain-text position of a DOM point (a selection's end) inside `root`. */
export function textOffset(root: HTMLElement, node: Node, offset: number): number {
  const { list, total } = segments(root);
  const hit = list.find(s => s.text && s.node === node);
  if (hit) return hit.start + Math.min(offset, hit.length);
  // A point between nodes: the position of the first piece of text at or after it.
  const point = root.ownerDocument.createRange();
  point.setStart(node, offset);
  const after = list.find(s => point.comparePoint(s.node, 0) >= 0);
  return after ? after.start : total;
}

/** The DOM range over [start, end) of the plain text inside `root`, or null when it holds no such text. */
export function textRange(root: HTMLElement, start: number, end: number): Range | null {
  const { list } = segments(root);
  const texts = list.filter(s => s.text && s.length > 0);
  const at = (pos: number, preferNext: boolean) => {
    const seg = texts.find(s => (preferNext ? pos >= s.start && pos < s.start + s.length : pos > s.start && pos <= s.start + s.length));
    if (seg) return { node: seg.node, offset: pos - seg.start };
    const next = preferNext ? texts.find(s => s.start >= pos) : [...texts].reverse().find(s => s.start + s.length <= pos);
    return next ? { node: next.node, offset: preferNext ? 0 : next.length } : null;
  };
  const from = at(start, true);
  const to = at(end, false);
  if (!from || !to) return null;
  const range = root.ownerDocument.createRange();
  range.setStart(from.node, from.offset);
  range.setEnd(to.node, to.offset);
  return range.collapsed ? null : range;
}

/**
 * Where a commented range is after its text changed: the same words, at the place nearest where
 * they were; null when they are gone.
 */
export function followText(before: string, after: string, start: number, length: number): number | null {
  if (before === after) return start;
  const words = before.slice(start, start + length);
  if (!words) return null;
  let best: number | null = null;
  for (let i = after.indexOf(words); i >= 0; i = after.indexOf(words, i + 1)) {
    if (best === null || Math.abs(i - start) < Math.abs(best - start)) best = i;
  }
  return best;
}

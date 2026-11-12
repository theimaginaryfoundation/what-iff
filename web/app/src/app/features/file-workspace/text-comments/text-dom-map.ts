/**
 * Where each character of a file's text is in the viewer's rendering of it (ADR 0x045), so a
 * comment's range of the text can be highlighted and a selection read back as a range. The text is
 * shown either as is (a <pre> of one text node) or syntax-coloured by Monaco, which renders a
 * space as a non-breaking space, a tab as non-breaking spaces to the next tab stop, and a line
 * break as <br>. Anything else that does not line up means the map cannot be made.
 */

const NBSP = '\u00a0';
/** East Asian wide and full-width characters, as Monaco's isFullWidthCharacter has them. */
const FULL_WIDTH = /[\u1100-\u115f\u2e80-\ua4cf\uac00-\ud7a3\uf900-\ufaff\ufe30-\ufe4f\uff00-\uff60\uffe0-\uffe6]/;

interface Run {
  /** The first text offset of the run. */
  at: number;
  node: Text;
  /** Where the run starts in the node. */
  from: number;
  /** Text characters in the run. */
  length: number;
  /** Rendered characters per text character: 1, or a tab's width. */
  width: number;
}

/** How a rendering of a file's text maps to the text: the raw text exactly, formatted Markdown by its words. */
export interface CommentTextMap {
  range(start: number, end: number): Range | null;
  sourceRange(selection: Range): { start: number; end: number } | null;
  offsetOf(node: Node, offset: number): number | null;
}

export class TextDomMap implements CommentTextMap {
  private constructor(
    private readonly runs: Run[],
    readonly length: number,
  ) {}

  /** The map for root's rendering of text, or null when the two do not line up. */
  static build(root: HTMLElement, text: string, tabSize = 4): TextDomMap | null {
    const runs: Run[] = [];
    let p = 0;
    let column = 0;
    const walker = root.ownerDocument.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (node.nodeName === 'BR') {
        while (text[p] === '\r') p++;
        // Monaco ends every line with <br>, the empty one after a final line break too.
        if (p >= text.length) continue;
        if (text[p] !== '\n') return null;
        p++;
        column = 0;
        continue;
      }
      if (node.nodeType !== Node.TEXT_NODE) continue;
      const shown = node.textContent ?? '';
      let i = 0;
      let run: Run | null = null;
      while (i < shown.length) {
        while (text[p] === '\r' && shown[i] !== '\r') p++;
        const c = text[p];
        if (c === undefined) return null;
        const d = shown[i];
        if (c === '\t' && d !== '\t') {
          const width = tabSize - (column % tabSize);
          for (let k = 0; k < width; k++) if (shown[i + k] !== NBSP && shown[i + k] !== ' ') return null;
          runs.push({ at: p, node: node as Text, from: i, length: 1, width });
          run = null;
          p++;
          i += width;
          column += width;
          continue;
        }
        if (!(d === c || (c === ' ' && d === NBSP))) return null;
        if (run && run.at + run.length === p) run.length++;
        else {
          run = { at: p, node: node as Text, from: i, length: 1, width: 1 };
          runs.push(run);
        }
        // Monaco counts a full-width character as two columns, which moves later tab stops.
        column = c === '\n' ? 0 : column + (FULL_WIDTH.test(c) ? 2 : 1);
        p++;
        i++;
      }
    }
    while (text[p] === '\r' || text[p] === '\n') p++;
    return p === text.length ? new TextDomMap(runs, text.length) : null;
  }

  /** The rendered place of a text offset (the start of the next character shown, at a line end). */
  private point(offset: number, end: boolean): { node: Text; offset: number } | null {
    for (const run of this.runs) {
      if (offset < run.at || offset > run.at + run.length) continue;
      if (offset === run.at + run.length && !end) continue;
      return { node: run.node, offset: run.from + (offset - run.at) * run.width };
    }
    const next = this.runs.find(r => r.at >= offset);
    if (next && !end) return { node: next.node, offset: next.from };
    const previous = [...this.runs].reverse().find(r => r.at + r.length <= offset);
    return previous ? { node: previous.node, offset: previous.from + previous.length * previous.width } : null;
  }

  /** The DOM range of [start, end) of the text. */
  range(start: number, end: number): Range | null {
    const from = this.point(start, false);
    const to = this.point(end, true);
    if (!from || !to) return null;
    const range = from.node.ownerDocument.createRange();
    range.setStart(from.node, from.offset);
    range.setEnd(to.node, to.offset);
    return range.collapsed ? null : range;
  }

  /** The text range a DOM selection covers. */
  sourceRange(selection: Range): { start: number; end: number } | null {
    const start = this.offsetOf(selection.startContainer, selection.startOffset);
    const end = this.offsetOf(selection.endContainer, selection.endOffset);
    return start !== null && end !== null && end > start ? { start, end } : null;
  }

  /** The text offset of a DOM point (a selection's end, a click), or null when it is not in the text. */
  offsetOf(node: Node, offset: number): number | null {
    for (const run of this.runs) {
      if (run.node !== node) continue;
      const span = run.length * run.width;
      if (offset < run.from || offset > run.from + span) continue;
      return run.at + Math.round((offset - run.from) / run.width);
    }
    // A point between nodes (an element and child index): the first run after it.
    const doc = node.ownerDocument;
    if (!doc) return null;
    const at = doc.createRange();
    at.setStart(node, offset);
    const after = this.runs.find(r => at.comparePoint(r.node, r.from) >= 0);
    return after ? after.at : this.length;
  }
}

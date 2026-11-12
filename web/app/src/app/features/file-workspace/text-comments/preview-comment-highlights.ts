import { addCommentRange, clearCommentRanges } from '../../office/comments/comment-highlights';
import { RenderedTextMap } from './rendered-text-map';

type Span = { start: number; end: number };

/**
 * The comment threads shown over the editor's live Markdown preview (ADR 0x045). The editor holds
 * the threads (Monaco decorations follow the typing); the preview only shows them, found by their
 * words as in the viewer's formatted view. The preview lags the typing a little, which finding by
 * words tolerates, so the map is made from the text as it is at each paint.
 */
export class PreviewCommentHighlights {
  private readonly painted: Range[] = [];
  private map: RenderedTextMap | null = null;

  /**
   * Marks each thread at its range of text, the active one more strongly; root is the preview's
   * rendering (null while there is none).
   */
  paint(
    root: HTMLElement | null,
    text: string,
    positions: ReadonlyMap<string, Span | null>,
    state: { activeId: string | null; hidden: ReadonlySet<string> },
  ): void {
    clearCommentRanges(this.painted);
    this.map = root ? new RenderedTextMap(root, text) : null;
    if (!this.map) return;
    for (const [id, at] of positions) {
      if (!at || state.hidden.has(id)) continue;
      const range = this.map.range(at.start, at.end);
      if (range) this.painted.push(addCommentRange(range, id === state.activeId));
    }
  }

  /** The text offset of a point in the preview (a click), as of the last paint. */
  offsetOf(node: Node, offset: number): number | null {
    return this.map?.offsetOf(node, offset) ?? null;
  }

  /** The text range a selection in the preview is of. */
  sourceRange(selection: Range): Span | null {
    return this.map?.sourceRange(selection) ?? null;
  }

  /** Scrolls the preview to a range of the text, when it is shown there. */
  reveal(range: Span): void {
    const dom = this.map?.range(range.start, range.end);
    dom?.startContainer.parentElement?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
  }

  dispose(): void {
    clearCommentRanges(this.painted);
    this.map = null;
  }
}

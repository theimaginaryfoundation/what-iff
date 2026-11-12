import { addCommentRange, clearCommentRanges } from '../../office/comments/comment-highlights';
import { TextCommentSurface } from './text-comments-state';
import { TextDomMap } from './text-dom-map';

type Span = { start: number; end: number };

/**
 * Comment threads on the viewer's text (ADR 0x045): the text does not change while it is viewed,
 * so a thread's range stays where it was placed, and is painted as a highlight over the rendering.
 */
export class ViewerCommentSurface implements TextCommentSurface {
  private readonly anchors = new Map<string, Span>();
  private readonly painted: Range[] = [];
  private state: { activeId: string | null; hidden: ReadonlySet<string> } = { activeId: null, hidden: new Set() };

  constructor(
    private readonly source: string,
    /** The rendering's map, which is replaced whenever the text is shown again. */
    private readonly map: () => TextDomMap | null,
  ) {}

  place(threadId: string, range: Span | null): void {
    if (range && range.end > range.start) this.anchors.set(threadId, range);
    else this.anchors.delete(threadId);
    this.repaint();
  }

  ranges(): Map<string, Span | null> {
    return new Map(this.anchors);
  }

  paint(state: { activeId: string | null; hidden: ReadonlySet<string> }): void {
    this.state = state;
    this.repaint();
  }

  /** Paints the threads again, as after the text was rendered anew. */
  repaint(): void {
    clearCommentRanges(this.painted);
    const map = this.map();
    if (!map) return;
    for (const [id, at] of this.anchors) {
      if (this.state.hidden.has(id)) continue;
      const range = map.range(at.start, at.end);
      if (range) this.painted.push(addCommentRange(range, id === this.state.activeId));
    }
  }

  reveal(range: Span): void {
    const dom = this.map()?.range(range.start, range.end);
    const el = dom?.startContainer.parentElement;
    el?.scrollIntoView?.({ block: 'center', behavior: 'smooth' });
  }

  text(range: Span): string {
    return this.source.slice(range.start, range.end);
  }

  dispose(): void {
    clearCommentRanges(this.painted);
  }
}

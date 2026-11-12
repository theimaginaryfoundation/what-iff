/**
 * Commented text painted with the CSS Custom Highlight API (ADR 0x045), so text that is shown (or
 * being edited) is never changed to mark it. Every surface adds its ranges to two document-wide
 * highlights, which the global stylesheet paints: open threads, and the active one.
 */

const NAMES = { open: 'whatiff-comment', active: 'whatiff-comment-active' };

interface HighlightLike {
  add(range: Range): void;
  delete(range: Range): void;
}

function highlightNamed(name: string): HighlightLike | null {
  const registry = (globalThis.CSS as unknown as { highlights?: Map<string, unknown> } | undefined)?.highlights;
  const Highlight = (globalThis as unknown as { Highlight?: new () => HighlightLike }).Highlight;
  if (!registry || !Highlight) return null;
  let highlight = registry.get(name) as HighlightLike | undefined;
  if (!highlight) {
    highlight = new Highlight();
    registry.set(name, highlight);
  }
  return highlight;
}

/** Marks a range as commented text (the active thread's more strongly) and returns it, to clear later. */
export function addCommentRange(range: Range, active: boolean): Range {
  highlightNamed(active ? NAMES.active : NAMES.open)?.add(range);
  return range;
}

/** Clears a surface's ranges, and empties the list. */
export function clearCommentRanges(ranges: Range[]): void {
  for (const range of ranges) {
    highlightNamed(NAMES.open)?.delete(range);
    highlightNamed(NAMES.active)?.delete(range);
  }
  ranges.length = 0;
}

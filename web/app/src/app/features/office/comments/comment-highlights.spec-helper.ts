/**
 * A stand-in for the CSS Custom Highlight API, which jsdom lacks: returns the registry the comment
 * highlights are added to, and a function that takes the stand-in away again.
 */
export function stubCommentHighlights(): { shown(name: 'whatiff-comment' | 'whatiff-comment-active'): string[]; restore(): void } {
  const g = globalThis as unknown as { CSS?: Record<string, unknown>; Highlight?: unknown };
  const hadCss = 'CSS' in g;
  const css = (g.CSS ??= {});
  const before = { highlights: css['highlights'], Highlight: g.Highlight };
  const registry = new Map<string, Set<Range>>();
  css['highlights'] = registry;
  g.Highlight = Set;
  return {
    shown: name => [...(registry.get(name) ?? [])].map(r => r.toString()),
    restore: () => {
      if (!hadCss) delete g.CSS;
      else if (before.highlights === undefined) delete css['highlights'];
      else css['highlights'] = before.highlights;
      if (before.Highlight === undefined) delete g.Highlight;
      else g.Highlight = before.Highlight;
    },
  };
}

import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { stubCommentHighlights } from '../../office/comments/comment-highlights.spec-helper';
import { PreviewCommentHighlights } from './preview-comment-highlights';

describe('PreviewCommentHighlights', () => {
  let highlights: ReturnType<typeof stubCommentHighlights>;
  let root: HTMLElement;
  const text = '# Plan\n\nShip the **new editor** first, then [docs](https://x.test/new).\n';

  beforeEach(() => {
    highlights = stubCommentHighlights();
    root = document.createElement('div');
    root.innerHTML = '<h1>Plan</h1><p>Ship the <strong>new editor</strong> first, then <a href="#">docs</a>.</p>';
    document.body.appendChild(root);
  });
  afterEach(() => {
    root.remove();
    highlights.restore();
  });

  it('marks threads on their words in the rendering, the active one apart, and not hidden ones', () => {
    const preview = new PreviewCommentHighlights();
    const at = (words: string) => ({ start: text.indexOf(words), end: text.indexOf(words) + words.length });
    const positions = new Map([
      ['a', at('the **new editor** first')],
      ['b', at('Plan')],
      ['c', at('docs')],
      ['gone', null],
    ]);
    preview.paint(root, text, positions, { activeId: 'b', hidden: new Set(['c']) });
    expect(highlights.shown('whatiff-comment')).toEqual(['the new editor first']);
    expect(highlights.shown('whatiff-comment-active')).toEqual(['Plan']);

    // A click maps back to the text, and a selection to its range.
    const strong = root.querySelector('strong')!.firstChild!;
    const offset = preview.offsetOf(strong, 2)!;
    expect(text.slice(offset, offset + 1)).toBe('w');
    const selection = document.createRange();
    selection.setStart(strong, 0);
    selection.setEnd(strong, 3);
    expect(preview.sourceRange(selection)).toEqual(at('new'));

    preview.paint(null, text, positions, { activeId: null, hidden: new Set() });
    expect(highlights.shown('whatiff-comment')).toEqual([]);
    expect(preview.offsetOf(strong, 2)).toBeNull();
  });
});

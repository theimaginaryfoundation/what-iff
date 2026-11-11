import { describe, expect, it } from 'vitest';

import { plainText } from '../pptx/deck-model';
import { followText, textOffset, textRange } from './comment-text';
import { paragraphsToHtml } from './text-dom';

/** Text positions for comments count as plainText does: one character per line break and between paragraphs. */
describe('comment text positions', () => {
  const paragraphs = [
    { align: 'left' as const, runs: [{ text: 'Revenue ' }, { text: 'up 18%', bold: true }] },
    { align: 'left' as const, runs: [] },
    { align: 'left' as const, runs: [{ text: 'Churn\nfell' }] },
  ];
  const render = () => {
    const root = document.createElement('div');
    root.innerHTML = paragraphsToHtml(paragraphs);
    return root;
  };
  const text = plainText({ paragraphs, vAlign: 'top', inset: [0, 0, 0, 0], wrap: true, autoFit: 'none' });

  it('maps a range of the plain text to the rendered DOM and back', () => {
    const root = render();
    for (const words of ['up 18%', 'Revenue up', 'fell', 'Churn\nfell']) {
      const start = text.indexOf(words);
      const range = textRange(root, start, start + words.length)!;
      expect(range.toString().replace(/\s/g, '')).toBe(words.replace(/\s/g, ''));
      expect(textOffset(root, range.startContainer, range.startOffset)).toBe(start);
      expect(textOffset(root, range.endContainer, range.endOffset)).toBe(start + words.length);
    }
  });

  it('follows commented words through an edit, to the nearest place they still are', () => {
    expect(followText('a b c b', 'x a b c b', 2, 1)).toBe(4);
    expect(followText('a b c b', 'a b c b', 6, 1)).toBe(6);
    expect(followText('up 18%', 'up 20%', 3, 3)).toBeNull();
  });
});

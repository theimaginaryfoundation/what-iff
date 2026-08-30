import { EmojiData } from '@ctrl/ngx-emoji-mart/ngx-emoji';

import { completedEmojiShortcode, resolveEmojiSearchResult } from './emoji-shortcode.helpers';

function emoji(overrides: Partial<EmojiData> & Record<string, unknown>): EmojiData {
  return overrides as EmojiData;
}

describe('emoji shortcode helpers', () => {
  it('finds only the completed shortcode immediately before the caret', () => {
    expect(completedEmojiShortcode('Hello :fox: friend', 11)).toEqual({ query: 'fox', start: 6, end: 11 });
    expect(completedEmojiShortcode('Hello :fox: friend', 18)).toBeNull();

    const url = 'https://example.test/:fox:';
    expect(completedEmojiShortcode(url, url.length)).toBeNull();
  });

  it('prefers exact short names over fuzzy search order', () => {
    const exact = emoji({ id: 'smile', shortNames: ['smile'], native: '😄' });
    const fuzzy = emoji({ id: 'smiley', shortNames: ['smiley'], native: '😃', keywords: ['smile'] });

    expect(resolveEmojiSearchResult('smile', [fuzzy, exact])).toBe(exact);
  });

  it('accepts a unique word alias such as fox for fox_face', () => {
    const fox = emoji({ id: 'fox_face', shortNames: ['fox_face'], name: 'Fox Face', native: '🦊', keywords: ['animal', 'fox'] });

    expect(resolveEmojiSearchResult('fox', [fox])).toBe(fox);
  });

  it('rejects ambiguous fuzzy aliases and unknown searches', () => {
    const grin = emoji({ id: 'grinning', name: 'Grinning Face', native: '😀', keywords: ['face'] });
    const smile = emoji({ id: 'smiley', name: 'Smiling Face', native: '😃', keywords: ['face'] });

    expect(resolveEmojiSearchResult('face', [grin, smile])).toBeNull();
    expect(resolveEmojiSearchResult('definitely_not_an_emoji', [])).toBeNull();
  });
});

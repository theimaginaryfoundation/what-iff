import { EmojiSearch } from '@ctrl/ngx-emoji-mart';
import { EmojiData } from '@ctrl/ngx-emoji-mart/ngx-emoji';

import {
  activeEmojiShortcodeQuery,
  emojiCharFromData,
  searchEmojiShortcodes,
} from './emoji-shortcode.helpers';

function emoji(overrides: Partial<EmojiData> & Record<string, unknown>): EmojiData {
  return overrides as EmojiData;
}

describe('emoji shortcode helpers', () => {
  describe('activeEmojiShortcodeQuery', () => {
    it('reports the in-progress shortcode immediately before the caret', () => {
      expect(activeEmojiShortcodeQuery('Hello :fo', 9)).toEqual({ query: 'fo', start: 6, end: 9 });
    });

    it('lowercases the typed query', () => {
      expect(activeEmojiShortcodeQuery('Hi :FoX', 7)).toEqual({ query: 'fox', start: 3, end: 7 });
    });

    it('ignores a shortcode that is not adjacent to the caret', () => {
      expect(activeEmojiShortcodeQuery('Hello :fox world', 16)).toBeNull();
    });

    it('does not trigger below the minimum query length', () => {
      expect(activeEmojiShortcodeQuery(':f', 2)).toBeNull();
    });

    it('does not trigger on a completed shortcode (trailing colon)', () => {
      // The closing colon ends the fragment, so nothing is being typed anymore.
      expect(activeEmojiShortcodeQuery('Hello :fox:', 11)).toBeNull();
    });

    it('does not treat URL or identifier colons as shortcodes', () => {
      expect(activeEmojiShortcodeQuery('https://example.test/:fox', 25)).toBeNull();
      expect(activeEmojiShortcodeQuery('scope:value', 11)).toBeNull();
    });

    it('triggers after opening punctuation', () => {
      expect(activeEmojiShortcodeQuery('(:fox', 5)).toEqual({ query: 'fox', start: 1, end: 5 });
    });
  });

  describe('searchEmojiShortcodes', () => {
    function stubSearch(results: EmojiData[] | null): EmojiSearch {
      return { search: () => results } as unknown as EmojiSearch;
    }

    it('returns nothing below the minimum query length', () => {
      const search = stubSearch([emoji({ id: 'fox_face', native: '🦊' })]);
      expect(searchEmojiShortcodes(search, 'f')).toEqual([]);
    });

    it('maps search results to insertable suggestions', () => {
      const search = stubSearch([
        emoji({ id: 'fox_face', native: '🦊', colons: ':fox_face:' }),
        emoji({ id: 'grinning', unified: '1F600' }),
      ]);

      expect(searchEmojiShortcodes(search, 'fo')).toEqual([
        { id: 'fox_face', colons: ':fox_face:', native: '🦊' },
        { id: 'grinning', colons: ':grinning:', native: '😀' },
      ]);
    });

    it('drops results with no derivable native character and de-dupes by id', () => {
      const search = stubSearch([
        emoji({ id: 'mystery' }),
        emoji({ id: 'fox_face', native: '🦊' }),
        emoji({ id: 'fox_face', native: '🦊' }),
      ]);

      expect(searchEmojiShortcodes(search, 'fo')).toEqual([
        { id: 'fox_face', colons: ':fox_face:', native: '🦊' },
      ]);
    });

    it('tolerates a null search result', () => {
      expect(searchEmojiShortcodes(stubSearch(null), 'fo')).toEqual([]);
    });
  });

  describe('emojiCharFromData', () => {
    it('prefers a native character', () => {
      expect(emojiCharFromData(emoji({ native: '🦊', unified: '1F98A' }))).toBe('🦊');
    });

    it('falls back to composing from unified codepoints', () => {
      expect(emojiCharFromData(emoji({ unified: '1F600' }))).toBe('😀');
    });

    it('returns empty when neither is available', () => {
      expect(emojiCharFromData(emoji({}))).toBe('');
    });
  });
});

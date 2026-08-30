import { EmojiData } from '@ctrl/ngx-emoji-mart/ngx-emoji';

export interface CompletedEmojiShortcode {
  query: string;
  start: number;
  end: number;
}

/**
 * Return the completed :shortcode: immediately before the caret.
 *
 * A shortcode must begin at the start of the draft or after whitespace/opening
 * punctuation. That keeps URL-ish and ordinary colon-delimited text from being
 * rewritten while the user is typing.
 */
export function completedEmojiShortcode(value: string, caret: number): CompletedEmojiShortcode | null {
  if (caret < 3 || caret > value.length || value[caret - 1] !== ':') {
    return null;
  }

  const beforeCaret = value.slice(0, caret);
  const match = /:([A-Za-z0-9_+\-]{1,64}):$/.exec(beforeCaret);
  if (!match || match.index < 0) {
    return null;
  }

  const start = match.index;
  if (start > 0 && !/[\s([{]/.test(value[start - 1])) {
    return null;
  }

  return { query: match[1].toLowerCase(), start, end: caret };
}

type SearchableEmoji = EmojiData & {
  id?: string;
  name?: string;
  shortNames?: string[];
  keywords?: string[];
};

/**
 * Resolve a shortcode from Emoji Mart search results without accepting an
 * arbitrary fuzzy first hit. Exact IDs/short names win. For convenience aliases
 * such as :fox: -> fox_face, accept only a single result whose name/id/keyword
 * contains the query as a whole word.
 */
export function resolveEmojiSearchResult(query: string, results: readonly EmojiData[]): EmojiData | null {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return null;

  const searchable = results as readonly SearchableEmoji[];
  const exact = searchable.find(emoji => emojiNames(emoji).includes(normalized));
  if (exact) return exact;

  const wordMatches = searchable.filter(emoji => emojiWords(emoji).has(normalized));
  return wordMatches.length === 1 ? wordMatches[0] : null;
}

function emojiNames(emoji: SearchableEmoji): string[] {
  return [emoji.id ?? '', ...(emoji.shortNames ?? [])]
    .map(name => name.trim().toLowerCase())
    .filter(Boolean);
}

function emojiWords(emoji: SearchableEmoji): Set<string> {
  const fields = [emoji.id ?? '', emoji.name ?? '', ...(emoji.shortNames ?? []), ...(emoji.keywords ?? [])];
  const words = fields
    .flatMap(field => field.toLowerCase().split(/[^a-z0-9+\-]+/))
    .filter(Boolean);
  return new Set(words);
}

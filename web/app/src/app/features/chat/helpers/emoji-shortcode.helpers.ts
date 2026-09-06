import { EmojiSearch } from '@ctrl/ngx-emoji-mart';
import { EmojiData } from '@ctrl/ngx-emoji-mart/ngx-emoji';

/**
 * Shortest `:query` we search for. One character (`:a`) matches almost every
 * emoji and is pure noise, so the popup stays quiet until there is something
 * worth narrowing on.
 */
export const MIN_EMOJI_QUERY_LENGTH = 2;

/** Default number of suggestions shown in the composer autocomplete popup. */
export const EMOJI_SUGGESTION_LIMIT = 8;

/**
 * The in-progress `:shortcode` fragment immediately before the caret. Unlike a
 * completed `:shortcode:`, this has no trailing colon — it is what the user is
 * still typing, and drives the suggestion popup.
 */
export interface ActiveEmojiShortcode {
  /** Lowercased text typed after the opening colon (colon excluded). */
  query: string;
  /** Index of the opening colon within the draft. */
  start: number;
  /** Caret index — the exclusive end of the fragment to replace on accept. */
  end: number;
}

/** A single emoji offered in the composer autocomplete popup. */
export interface EmojiSuggestion {
  /** Stable Emoji Mart id, used for list tracking. */
  id: string;
  /** Canonical `:short_name:` label shown beside the glyph. */
  colons: string;
  /** The native emoji character inserted when the suggestion is accepted. */
  native: string;
}

/**
 * A shortcode may only begin at the start of the draft or after whitespace or
 * opening punctuation. That keeps URL-ish and identifier-ish colons (e.g.
 * `https://host/:id`) from ever opening the popup.
 */
const ACTIVE_SHORTCODE = /(^|[\s([{])(:)([A-Za-z0-9_+\-]{1,64})$/;

/**
 * Return the in-progress `:shortcode` immediately before the caret, or null when
 * the caret is not inside one. This never rewrites the draft — it only reports
 * what the user is typing so the composer can offer suggestions.
 */
export function activeEmojiShortcodeQuery(value: string, caret: number): ActiveEmojiShortcode | null {
  if (caret < 0 || caret > value.length) {
    return null;
  }

  const match = ACTIVE_SHORTCODE.exec(value.slice(0, caret));
  if (!match) {
    return null;
  }

  const query = match[3].toLowerCase();
  if (query.length < MIN_EMOJI_QUERY_LENGTH) {
    return null;
  }

  // match[1] is the boundary char (or empty at start); the colon follows it.
  const start = match.index + match[1].length;
  return { query, start, end: caret };
}

/**
 * Look up emoji suggestions for an in-progress shortcode query. Returns
 * insertable suggestions (those we can render as a native character), de-duped
 * by id and capped at {@link EMOJI_SUGGESTION_LIMIT}.
 */
export function searchEmojiShortcodes(
  emojiSearch: EmojiSearch,
  query: string,
  limit: number = EMOJI_SUGGESTION_LIMIT,
): EmojiSuggestion[] {
  const normalized = query.trim().toLowerCase();
  if (normalized.length < MIN_EMOJI_QUERY_LENGTH) {
    return [];
  }

  const results = emojiSearch.search(normalized, undefined, limit) ?? [];
  const suggestions: EmojiSuggestion[] = [];
  const seen = new Set<string>();

  for (const emoji of results) {
    const native = emojiCharFromData(emoji);
    if (!native) {
      continue;
    }
    const id = (emoji as { id?: string }).id ?? native;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    suggestions.push({ id, colons: emojiColons(emoji, id), native });
  }

  return suggestions;
}

/** Resolve the native character for an emoji, from `native` or its codepoints. */
export function emojiCharFromData(emoji: EmojiData): string {
  if (emoji.native) {
    return emoji.native;
  }
  if (!emoji.unified) {
    return '';
  }
  try {
    return emoji.unified
      .split('-')
      .map(hex => String.fromCodePoint(parseInt(hex, 16)))
      .join('');
  } catch {
    return '';
  }
}

function emojiColons(emoji: EmojiData, id: string): string {
  const colons = (emoji as { colons?: string }).colons;
  if (colons) {
    return colons;
  }
  return `:${id}:`;
}

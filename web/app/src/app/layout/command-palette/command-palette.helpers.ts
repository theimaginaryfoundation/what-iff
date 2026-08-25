import { SearchSection, SearchSectionType, SECTION_ORDER } from '../../core/models/search.model';

/**
 * Score tiers mirror the server-side scoring in `internal/handlers/search/score.go`
 * so client-side static commands rank consistently with server hits.
 */
export const SCORE_EXACT = 100;
export const SCORE_PREFIX = 80;
export const SCORE_WORD_BOUNDARY = 60;
export const SCORE_SUBSTRING = 40;
export const SCORE_NO_MATCH = 0;

/**
 * Plain rankable item for the static-command handler. Components map this to a
 * `SearchResult`-shaped row before merging with server sections.
 */
export interface RankableItem {
  readonly label: string;
  readonly description?: string;
}

/**
 * Returns the highest scoring tier that `query` matches against `text`. Empty
 * `query` returns the no-match score so callers fall back to default ordering.
 */
export function scoreText(query: string, text: string): number {
  if (!query) return SCORE_NO_MATCH;
  if (!text) return SCORE_NO_MATCH;
  const q = query.trim().toLowerCase();
  if (!q) return SCORE_NO_MATCH;
  const t = text.toLowerCase();
  if (t === q) return SCORE_EXACT;
  if (t.startsWith(q)) return SCORE_PREFIX;
  if (hasWordBoundaryMatch(t, q)) return SCORE_WORD_BOUNDARY;
  if (t.includes(q)) return SCORE_SUBSTRING;
  return SCORE_NO_MATCH;
}

/**
 * Picks the maximum tier across `label` and `description` so a substring match
 * in description doesn't outrank a prefix in label.
 */
export function scoreItem(query: string, item: RankableItem): number {
  const labelScore = scoreText(query, item.label);
  const descScore = item.description ? scoreText(query, item.description) : SCORE_NO_MATCH;
  return Math.max(labelScore, descScore);
}

/**
 * Filters and ranks a list of items by query. Stable: equal-scored items keep
 * their input order (alphabetical sort is the caller's choice when desired).
 */
export function rankResults<T extends RankableItem>(
  items: readonly T[],
  query: string,
): Array<T & { score: number }> {
  const scored = items.map(item => ({ ...item, score: scoreItem(query, item) }));
  return scored
    .filter(s => s.score > SCORE_NO_MATCH)
    .sort((a, b) => b.score - a.score);
}

/**
 * Reorders sections to the canonical type order (chat / personality / ritual /
 * memory / image). Unknown types are dropped (the client never renders them).
 */
export function mergeSections(sections: readonly SearchSection[]): SearchSection[] {
  const byType = new Map<SearchSectionType, SearchSection>();
  for (const section of sections) {
    if (SECTION_ORDER.includes(section.type)) {
      byType.set(section.type, section);
    }
  }
  return SECTION_ORDER.map(type => byType.get(type)).filter(
    (section): section is SearchSection => section !== undefined,
  );
}

function hasWordBoundaryMatch(haystack: string, needle: string): boolean {
  if (!needle) return false;
  if (haystack.startsWith(needle)) return true;
  for (let i = 1; i < haystack.length; i++) {
    if (!isWordBoundary(haystack.charCodeAt(i - 1))) continue;
    if (haystack.startsWith(needle, i)) return true;
  }
  return false;
}

function isWordBoundary(code: number): boolean {
  // Whitespace, hyphen, underscore, slash, dot, comma, paren, colon, semicolon.
  return (
    code === 32 || // space
    code === 9 || // tab
    code === 10 || // \n
    code === 13 || // \r
    code === 45 || // -
    code === 95 || // _
    code === 47 || // /
    code === 46 || // .
    code === 44 || // ,
    code === 40 || // (
    code === 41 || // )
    code === 58 || // :
    code === 59 // ;
  );
}

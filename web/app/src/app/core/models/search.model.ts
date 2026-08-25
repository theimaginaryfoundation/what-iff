/**
 * Cross-resource search response shape. Mirrors the OpenAPI schemas defined in
 * `openapi.yaml` (`SearchResult`, `SearchSection`, `SearchResponse`) which back
 * the `GET /search` endpoint introduced in Gap 06. Keep these definitions in
 * sync with the spec; the regression suite asserts the wire shape.
 */

/** Canonical resource sections returned by `GET /search`. */
export type SearchSectionType = 'chat' | 'personality' | 'ritual' | 'memory' | 'image';

/** Canonical render order for the command palette and gallery views. */
export const SECTION_ORDER: ReadonlyArray<SearchSectionType> = [
  'chat',
  'personality',
  'ritual',
  'memory',
  'image',
];

/** Single hit inside a search section. `route` is an SPA path. */
export interface SearchResult {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
  readonly route: string;
  readonly icon_type: SearchSectionType;
  readonly score: number;
  readonly snippet?: string;
}

/** One resource-type section. Always emitted (with empty `results`) per the API. */
export interface SearchSection {
  readonly type: SearchSectionType;
  readonly results: ReadonlyArray<SearchResult>;
}

/** Body of `GET /search`. Sections come back in the canonical order above. */
export interface SearchResponse {
  readonly query: string;
  readonly sections: ReadonlyArray<SearchSection>;
}

/** Optional inputs accepted by the search endpoint. */
export interface SearchOptions {
  readonly types?: ReadonlyArray<SearchSectionType>;
  /** 1..25 inclusive per the API contract. Defaults to 5 server-side. */
  readonly limitPerType?: number;
}

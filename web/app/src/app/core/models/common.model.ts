/**
 * The error envelope every API error response carries (ADR 0x020).
 *
 * This is the shape found on `HttpErrorResponse.error` — the parsed body — not
 * the `HttpErrorResponse` itself. Angular's own `.message` is a transport
 * string ("Http failure response for <url>: 400 Bad Request") and is never
 * what a user should be shown.
 */
export interface ApiErrorEnvelope {
  /**
   * Human-readable copy chosen by the handler. This is the field to display.
   */
  message?: string;
  /**
   * Stable machine-readable identifier. Always present on a real envelope.
   * Typed as `string` rather than a union of every code because generic codes
   * are not a branch target — see {@link SpecificErrorCode}.
   */
  code?: string;
  /**
   * Deprecated duplicate of `message`, retained until ADR 0x020 phase 4 drops
   * it from the API. Read only as a fallback; never introduce a new reader.
   */
  error?: string;
  /** Optional structured metadata. Shape varies by endpoint. */
  details?: unknown;
}

/**
 * The error codes that are a contract and may be branched on.
 *
 * ADR 0x020 splits codes into two tiers. Specific codes name a single failure
 * condition and are stable. Generic codes (`bad_request`, `not_found`, …) are
 * derived from the HTTP status and may be narrowed to a specific code at any
 * time without that being a breaking change — branching on one is a latent bug.
 *
 * Only specific codes are listed here, so `hasErrorCode` cannot be called with
 * a generic one. That restriction is the point: it makes the ADR's rule a
 * compile error rather than a comment asking politely.
 */
export type SpecificErrorCode = 'message_too_long' | 'quota_exceeded' | 'system_prompt_too_long';

export interface PaginatedResponse<T> {
  results: T[];
  total_count: number;
  page: number;
}

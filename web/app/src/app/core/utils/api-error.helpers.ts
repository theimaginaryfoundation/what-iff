import { HttpErrorResponse } from '@angular/common/http';
import { ApiErrorEnvelope, SpecificErrorCode } from '../models/common.model';

/**
 * Twin of `web/admin-app/src/app/core/utils/api-error.helpers.ts`.
 *
 * The two Angular apps have separate source trees, separate tsconfigs, and no
 * path mapping between them, so there is nowhere shared to put this yet. Change
 * one and change the other — a shared `web/` library is the real fix.
 */

/**
 * Reads the ADR 0x020 error envelope off a failed request.
 *
 * Returns `null` when the failure produced no envelope — a network drop, a
 * gateway that answered with HTML, or a client-side exception. Callers should
 * treat that as "the server never got to explain itself" and use their own copy.
 */
export function apiErrorEnvelope(error: unknown): ApiErrorEnvelope | null {
  if (!(error instanceof HttpErrorResponse)) return null;
  const body = error.error;
  // A string body is a proxy/gateway page or a plain-text 502, never our
  // envelope — the API has emitted JSON on every error path since ADR 0x020
  // phase 2. Showing it raw would put markup in a toast.
  if (typeof body !== 'object' || body === null) return null;
  // A DOM event body (ProgressEvent, or ErrorEvent on older stacks) is a
  // client-side transport failure rather than a response. ErrorEvent even has a
  // `message`, but it holds a browser string like "Network request failed" — not
  // copy written for this user — so the caller's fallback is the better answer.
  if (typeof Event !== 'undefined' && body instanceof Event) return null;
  return body as ApiErrorEnvelope;
}

/**
 * The message to show a user for a failed request.
 *
 * Precedence is envelope `message`, then the deprecated `error` duplicate,
 * then the caller's fallback.
 *
 * Note what is *not* in that list: `HttpErrorResponse.message`. Angular builds
 * it as "Http failure response for <url>: 400 Bad Request", which names our
 * internal URL and tells the user nothing. The caller's fallback is better copy
 * in every case, so a request that fails without an envelope gets the fallback
 * rather than the transport string. The local helpers this replaces had the
 * opposite precedence and surfaced that string on every HTTP failure.
 */
export function apiErrorMessage(error: unknown, fallback: string): string {
  const envelope = apiErrorEnvelope(error);
  if (envelope) {
    const message = firstNonBlank(envelope.message, envelope.error);
    if (message) return message;
  }

  // Not an HTTP failure: a genuine client-side throw, where `message` is the
  // author's own text rather than something Angular generated.
  if (!(error instanceof HttpErrorResponse)) {
    if (error instanceof Error) {
      const message = firstNonBlank(error.message);
      if (message) return message;
    }
    if (typeof error === 'object' && error !== null && 'message' in error) {
      const message = firstNonBlank((error as { message?: unknown }).message);
      if (message) return message;
    }
  }

  return fallback;
}

/**
 * Whether a failed request carries a specific error code.
 *
 * Only accepts codes that ADR 0x020 designates as a contract. Generic codes are
 * derived from the HTTP status and may be narrowed at any time, so branching on
 * one silently breaks later — use `error.status` for those instead.
 */
export function hasErrorCode(error: unknown, code: SpecificErrorCode): boolean {
  return apiErrorEnvelope(error)?.code === code;
}

function firstNonBlank(...values: unknown[]): string | null {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value;
  }
  return null;
}

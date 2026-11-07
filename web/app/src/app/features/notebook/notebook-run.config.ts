import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

import { environment } from '../../../environments/environment';

/**
 * Where the JupyterLite that runs notebooks lives (ADR 0x043 §8), or null when Run is not offered.
 * It must be another origin: a same-origin iframe shares `sessionStorage` with this page, so a
 * notebook's code or output could read the session token. A URL on this origin is refused outright.
 */
export function usableRunUrl(url: string | null | undefined, appOrigin: string): string | null {
  if (!url) return null;
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null; // Includes a deployment's unsubstituted placeholder.
  }
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return null;
  if (parsed.origin === appOrigin) {
    console.error('JupyterLite must be served from its own origin; Run stays off.');
    return null;
  }
  return parsed.href;
}

export const JUPYTERLITE_URL = new InjectionToken<string | null>('JUPYTERLITE_URL', {
  providedIn: 'root',
  factory: () => usableRunUrl((environment as { jupyterliteUrl?: string }).jupyterliteUrl, inject(DOCUMENT).location.origin),
});

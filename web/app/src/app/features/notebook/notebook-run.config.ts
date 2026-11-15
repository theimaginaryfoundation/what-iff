import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

import { environment } from '../../../environments/environment';
import { usableOwnOriginSite } from '../../core/utils/own-origin-site.helpers';

/**
 * Where the JupyterLite that runs notebooks lives (ADR 0x043 §11), or null when Run is not offered:
 * a site on an origin of its own (usableOwnOriginSite), which is what makes `allow-same-origin`
 * on the Run iframe safe.
 */
export function usableRunUrl(url: string | null | undefined, appOrigin: string): string | null {
  return usableOwnOriginSite(url, appOrigin, 'JupyterLite');
}

export const JUPYTERLITE_URL = new InjectionToken<string | null>('JUPYTERLITE_URL', {
  providedIn: 'root',
  factory: () => usableRunUrl((environment as { jupyterliteUrl?: string }).jupyterliteUrl, inject(DOCUMENT).location.origin),
});

/**
 * Whether a notebook's script-driven output may be offered to run (in a sandboxed frame of the
 * page's own policy). False where the page's policy would block its scripts anyway (the share
 * site, ADR 0x051 §6): the output then says it cannot be shown, rather than a frame staying blank.
 */
export const NOTEBOOK_INTERACTIVE_OUTPUT = new InjectionToken<boolean>('NOTEBOOK_INTERACTIVE_OUTPUT', {
  providedIn: 'root',
  factory: () => true,
});

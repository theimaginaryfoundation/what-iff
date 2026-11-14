import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

import { environment } from '../../../environments/environment';
import { usableOtherSite } from '../../core/utils/own-origin-site.helpers';

/**
 * Where the preview site lives (ADR 0x050 §3), or null when projects cannot be served here: a site
 * on a domain of its own (usableOtherSite: the app's cookies on its domain never reach it). Without it an `.html` file still renders on its
 * own, with the files beside it (§1).
 */
export const PREVIEW_URL = new InjectionToken<string | null>('PREVIEW_URL', {
  providedIn: 'root',
  factory: () => usableOtherSite((environment as { previewUrl?: string }).previewUrl, inject(DOCUMENT).location.origin, 'The preview site'),
});

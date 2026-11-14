import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

import { environment } from '../../../environments/environment';
import { usableOwnOriginSite } from '../../core/utils/own-origin-site.helpers';

/**
 * Where the preview site lives (ADR 0x050 §3), or null when projects cannot be served here: a site
 * on an origin of its own (usableOwnOriginSite). Without it an `.html` file still renders on its
 * own, with the files beside it (§1).
 */
export const PREVIEW_URL = new InjectionToken<string | null>('PREVIEW_URL', {
  providedIn: 'root',
  factory: () =>
    usableOwnOriginSite((environment as { previewUrl?: string }).previewUrl, inject(DOCUMENT).location.origin, 'The preview site'),
});

import { DOCUMENT } from '@angular/common';
import { InjectionToken, inject } from '@angular/core';

import { environment } from '../../../environments/environment';
import { usableOwnOriginSite } from '../../core/utils/own-origin-site.helpers';

/**
 * Where the draw.io editor for .drawio files lives (ADR 0x049), or null when diagrams cannot be
 * opened here: a site on an origin of its own (usableOwnOriginSite), built from one pinned
 * release by `make diagrams-site`.
 */
export const DIAGRAMS_URL = new InjectionToken<string | null>('DIAGRAMS_URL', {
  providedIn: 'root',
  factory: () =>
    usableOwnOriginSite((environment as { diagramsUrl?: string }).diagramsUrl, inject(DOCUMENT).location.origin, 'The diagram editor'),
});

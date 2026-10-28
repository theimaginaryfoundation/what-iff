import { CanActivateChildFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { catchError, map, of } from 'rxjs';

import { PersonalityPresenceService } from '../services/personality-presence.service';

/** Routes reachable before the user has created their first personality. */
const EXEMPT_PATH_PREFIXES = [
  '/personality',
  '/personalities',
  '/profile',
  '/subscription',
  '/billing',
  '/usage',
  // Import/restore must be available before a user has a personality (fresh users
  // may want to restore an export or import history first). /experimental → /data.
  '/data',
  '/experimental',
  // Tools holds mullet mode (remote sync), the other way to bring history in: a fresh
  // self-hosted account may want its whatiff.chat threads before making anything.
  '/integrations',
];

function isExemptPath(url: string | undefined | null): boolean {
  if (!url) {
    return false;
  }
  const path = url.split('?')[0].split('#')[0];
  return EXEMPT_PATH_PREFIXES.some(prefix => path === prefix || path.startsWith(`${prefix}/`));
}

/**
 * Ensures users with zero personalities land on the personalities page so they can
 * create one before using chat and other app features.
 */
export const personalitySetupGuard: CanActivateChildFn = (_route, state) => {
  if (isExemptPath(state.url ?? '')) {
    return true;
  }

  const presence = inject(PersonalityPresenceService);
  const router = inject(Router);

  // Answers synchronously once a personality is known to exist, so ordinary navigations
  // do not wait on the network (#348).
  return presence.hasPersonality().pipe(
    map(has => (has ? true : router.createUrlTree(['/personality'], { queryParams: { setup: '1' } }))),
    catchError(err => {
      console.error('personalitySetupGuard: failed to load personalities', err);
      return of(router.createUrlTree(['/personality'], { queryParams: { setup: '1' } }));
    }),
  );
};

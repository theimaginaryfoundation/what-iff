import { CanActivateChildFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { catchError, map, of } from 'rxjs';

import { PersonalityService } from '../services/personality.service';

/** Routes reachable before the user has created their first personality. */
const EXEMPT_PATH_PREFIXES = [
  '/personality',
  '/personalities',
  '/profile',
  '/subscription',
  '/billing',
  '/usage',
  // Account restoration must be available before a user has a personality.
  '/experimental',
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

  const personalityService = inject(PersonalityService);
  const router = inject(Router);

  return personalityService.listPersonalities(1, 1).pipe(
    map(response => {
      const count = response.total_count ?? response.results?.length ?? 0;
      if (count > 0) {
        return true;
      }
      return router.createUrlTree(['/personality'], { queryParams: { setup: '1' } });
    }),
    catchError(err => {
      console.error('personalitySetupGuard: failed to load personalities', err);
      return of(router.createUrlTree(['/personality'], { queryParams: { setup: '1' } }));
    }),
  );
};

import { CanActivateChildFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { catchError, map, of } from 'rxjs';

import { PersonalityService } from '../services/personality.service';

/** Routes reachable before the user has created their first personality. */
const EXEMPT_PATH_PREFIXES = [
  '/personality',
  '/personalities',
  '/profile',
  // Same reason as the key guard: the explanation of what each provider is for
  // has to be readable while someone is still setting up.
  '/providers',
  '/subscription',
  '/billing',
  '/usage',
  // Account restoration must be available before a user has a personality.
  '/experimental',
  // Provider keys are configured here, and creating a personality is itself a
  // model call — so key setup has to come first. Without this exemption the
  // two setup guards bounce against each other: no key sends you to
  // /integrations, no personality sends you back to /personality, and no key
  // sends you to /integrations again.
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

import { CanActivateChildFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { catchError, map, of } from 'rxjs';

import { ProviderKeyService } from '../services/provider-key.service';

/**
 * Routes reachable before the account has a usable provider key.
 *
 * Settings and profile stay open so a signed-in user is never trapped: the key
 * screen itself lives under /integrations, and sign-out lives under /profile.
 */
const EXEMPT_PATH_PREFIXES = ['/integrations', '/profile'];

function isExemptPath(url: string | undefined | null): boolean {
  if (!url) {
    return false;
  }
  const path = url.split('?')[0].split('#')[0];
  return EXEMPT_PATH_PREFIXES.some(prefix => path === prefix || path.startsWith(`${prefix}/`));
}

/**
 * Sends an account with no usable OpenAI key to the integrations screen to add
 * one.
 *
 * OpenAI is required rather than merely useful: chat runs on whichever provider
 * you pick, but the supporting features — memory embeddings, summarization,
 * chat naming, mood, expressions, file attachments — call OpenAI regardless.
 * Without a key the app loads and then fails at the first message, which is a
 * worse experience than being told up front.
 *
 * This must run before personalitySetupGuard: creating the first personality is
 * itself an OpenAI call, so sending someone there first only moves the failure.
 *
 * A failed status lookup lets the user through rather than stranding them on a
 * setup screen over a transient error — the request they make next will surface
 * the real problem.
 */
export const apiKeySetupGuard: CanActivateChildFn = (_route, state) => {
  if (isExemptPath(state.url ?? '')) {
    return true;
  }

  const providerKeys = inject(ProviderKeyService);
  const router = inject(Router);

  return providerKeys.listStatuses().pipe(
    map(statuses => {
      const required = statuses.filter(s => s.required);
      // No provider marked required means nothing to gate on.
      if (required.length === 0 || required.every(s => s.configured)) {
        return true;
      }
      return router.createUrlTree(['/integrations'], { queryParams: { setup: 'api-key' } });
    }),
    catchError(err => {
      console.error('apiKeySetupGuard: failed to load provider key status', err);
      return of(true);
    }),
  );
};

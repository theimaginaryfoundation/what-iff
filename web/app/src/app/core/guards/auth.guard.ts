import { CanActivateFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { AuthService } from '../services/auth.service';
import { ExternalAuthProvider } from '../auth/external-auth.provider';

export const authGuard: CanActivateFn = async (route, state) => {
  const authService = inject(AuthService);
  const externalAuth = inject(ExternalAuthProvider);
  const router = inject(Router);

  let isAuthenticated = authService.isLoggedIn();

  // If not authenticated locally, check for an external provider session.
  if (!isAuthenticated && externalAuth.available) {
    try {
      const tokens = await externalAuth.fetchSession();
      isAuthenticated = !!tokens?.accessToken;
      if (isAuthenticated) {
        authService.checkAuthState();
      }
    } catch {
      isAuthenticated = false;
    }
  }

  if (isAuthenticated) {
    return true;
  }

  // Redirect to login page with return url
  router.navigate(['/auth/login'], { queryParams: { returnUrl: state.url } });
  return false;
};

export const guestGuard: CanActivateFn = async (route, state) => {
  const authService = inject(AuthService);
  const externalAuth = inject(ExternalAuthProvider);
  const router = inject(Router);

  // A guest route may opt out of the "signed-in users get bounced" redirect by
  // setting `data.allowAuthenticated` on the route being activated. This exists
  // for an in-progress sign-in return page that must be allowed to render even
  // though a session already exists (otherwise the guard would redirect it away
  // before its own logic — e.g. a post-sign-in setup step — could run). Walk to
  // the deepest activating child so the flag can live on the child route.
  let target = route;
  while (target.firstChild) {
    target = target.firstChild;
  }
  if (target.data?.['allowAuthenticated'] === true) {
    return true;
  }

  let isAuthenticated = authService.isLoggedIn();

  // Also check for an external provider session.
  if (!isAuthenticated && externalAuth.available) {
    try {
      const tokens = await externalAuth.fetchSession();
      isAuthenticated = !!tokens?.accessToken;
    } catch {
      isAuthenticated = false;
    }
  }

  if (!isAuthenticated) {
    return true;
  }

  // Redirect signed-in users away from auth pages into chat.
  router.navigate(['/chat']);
  return false;
};

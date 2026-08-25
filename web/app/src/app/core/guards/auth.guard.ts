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

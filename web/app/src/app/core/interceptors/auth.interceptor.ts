import { HttpInterceptorFn, HttpErrorResponse, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';
import { catchError, switchMap } from 'rxjs/operators';
import { throwError, from } from 'rxjs';
import { AuthService } from '../services/auth.service';
import { ExternalAuthProvider } from '../auth/external-auth.provider';

export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const authService = inject(AuthService);
  const externalAuth = inject(ExternalAuthProvider);

  // Try to get a usable token for this request
  return from(
    (async (): Promise<HttpRequest<unknown>> => {
      const storedToken = authService.getAccessToken();
      let accessToken: string | null = storedToken;

      if (externalAuth.available) {
        try {
          const tokens = await externalAuth.fetchSession();
          const sessionIdToken = tokens?.idToken ?? null;

          if (sessionIdToken) {
            authService.syncExternalTokens(tokens, 'while attaching auth header');
            accessToken = authService.getAccessToken() ?? sessionIdToken;
          }
        } catch {
          accessToken = storedToken;
        }
      } else {
        accessToken = storedToken;
      }

      if (!accessToken && authService.hasExternalSession()) {
        accessToken = authService.getAccessToken();
      }

      // Clone request and add authorization header when appropriate
      let authReq: HttpRequest<unknown> = req;
      if (accessToken && !req.url.includes('/user/login') && !req.url.includes('/user/register')) {
        authReq = req.clone({
          headers: req.headers.set('Authorization', `Bearer ${accessToken}`)
        });
      }

      return authReq;
    })()
  ).pipe(
    switchMap((authReq: HttpRequest<unknown>) => next(authReq)),
    catchError((error: HttpErrorResponse) => {
      // If we get a 401 error and it's not a login/register request, try to refresh the token
      if (error.status === 401 &&
          !req.url.includes('/user/login') &&
          !req.url.includes('/user/register') &&
          !req.url.includes('/user/refresh')) {

        if (authService.hasExternalSession()) {
          return from(externalAuth.fetchSession(true)).pipe(
            switchMap((tokens) => {
              authService.syncExternalTokens(tokens, 'after 401 refresh');
              const newToken = authService.getAccessToken();

              if (!newToken) {
                throw new Error('Missing external session token after refresh');
              }

              const retryReq = req.clone({
                headers: req.headers.set('Authorization', `Bearer ${newToken}`)
              });
              return next(retryReq);
            }),
            catchError(() => {
              void authService.logoutExternal();
              return throwError(() => error);
            })
          );
        }

        return authService.refreshToken().pipe(
          switchMap(() => {
            // Retry the original request with the new token
            const newToken = authService.getAccessToken();
            const retryReq = req.clone({
              headers: req.headers.set('Authorization', `Bearer ${newToken}`)
            });
            return next(retryReq);
          }),
          catchError(() => {
            // If refresh fails, logout and return the error
            authService.logout();
            return throwError(() => error);
          })
        );
      }

      return throwError(() => error);
    })
  );
};

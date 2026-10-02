import { HttpInterceptorFn } from '@angular/common/http';

/**
 * Extra HTTP interceptors (overlay swap-point).
 *
 * `app.config.ts` spreads this after the built-in `authInterceptor`, so a private
 * build can add cross-cutting request/response handling (e.g. routing an
 * onboarding-required response to the onboarding screen) without editing the
 * open-source config. Empty in the open-source build.
 */
export const extraHttpInterceptors: HttpInterceptorFn[] = [];

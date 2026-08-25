import { Provider } from '@angular/core';

import {
  ExternalAuthProvider,
  NoopExternalAuthProvider,
} from '../core/auth/external-auth.provider';

/**
 * DI providers for the external identity provider (swap-point file).
 *
 * This build binds the no-op provider, so the app uses its built-in
 * username/password authentication. Another build replaces this file to bind a
 * concrete provider. `app.config.ts` spreads these into the application config.
 */
export const externalAuthProviders: Provider[] = [
  { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
];

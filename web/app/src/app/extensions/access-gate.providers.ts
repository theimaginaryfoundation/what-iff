import { Provider } from '@angular/core';

import { AccessGate, NoopAccessGate } from '../core/services/access-gate';

/**
 * DI providers for the access gate (swap-point file).
 *
 * This build binds the no-op gate, so access-gated features are always
 * available. Another build replaces this file to bind an implementation that
 * restricts access. `app.config.ts` spreads these into the application config.
 */
export const accessGateProviders: Provider[] = [
  { provide: AccessGate, useClass: NoopAccessGate },
];

import { Provider } from '@angular/core';

import { IntegrationTabSource, NoIntegrationTabs } from '../core/services/integration-tab-source';

/**
 * DI providers for extra Integrations tabs (swap-point file).
 *
 * This build contributes no tabs. Another build replaces this file to bind a
 * source that adds its own. `app.config.ts` spreads these into the application
 * config.
 */
export const integrationTabProviders: Provider[] = [
  { provide: IntegrationTabSource, useClass: NoIntegrationTabs },
];

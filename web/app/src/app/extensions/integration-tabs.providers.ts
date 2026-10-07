import { Provider } from '@angular/core';

import { IntegrationTabSource } from '../core/services/integration-tab-source';
import { DiscordIntegrationTabSource } from '../features/discord/discord-sources';

/**
 * DI providers for extra Integrations tabs (swap-point file).
 *
 * This build contributes the Discord tab (shown when the server runs the relay).
 * Another build may replace this file to bind a source that adds its own tabs;
 * it should keep the Discord tab. `app.config.ts` spreads these into the
 * application config.
 */
export const integrationTabProviders: Provider[] = [{ provide: IntegrationTabSource, useClass: DiscordIntegrationTabSource }];

import { Provider } from '@angular/core';

import { CombinedIntegrationTabs, IntegrationTabSource } from '../core/services/integration-tab-source';
import { BuzzIntegrationTabSource } from '../features/buzz/buzz-sources';
import { DiscordIntegrationTabSource } from '../features/discord/discord-sources';

/**
 * DI providers for extra Integrations tabs (swap-point file).
 *
 * This build contributes the Discord and Buzz tabs (each shown when the server runs that relay),
 * combined into the one IntegrationTabSource the Integrations page reads. Another build may
 * replace this file to bind a source that adds its own tabs; it should keep these two, by adding
 * its source to the combined list. `app.config.ts` spreads these into the application config.
 */
export const integrationTabProviders: Provider[] = [
  DiscordIntegrationTabSource,
  BuzzIntegrationTabSource,
  {
    provide: IntegrationTabSource,
    useFactory: (discord: DiscordIntegrationTabSource, buzz: BuzzIntegrationTabSource) => new CombinedIntegrationTabs([discord, buzz]),
    deps: [DiscordIntegrationTabSource, BuzzIntegrationTabSource],
  },
];

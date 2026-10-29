import { Provider } from '@angular/core';

import { CombinedIntegrationTabs, IntegrationTabSource } from '../core/services/integration-tab-source';
import { BuzzIntegrationTabSource } from '../features/buzz/buzz-sources';
import { DiscordIntegrationTabSource } from '../features/discord/discord-sources';
import { RemoteSyncIntegrationTabSource } from '../features/remote-sync/remote-sync-sources';
import { SlackIntegrationTabSource } from '../features/slack/slack-sources';

/**
 * DI providers for extra Integrations tabs (swap-point file).
 *
 * This build contributes the Discord, Buzz and Slack tabs (each shown when that relay is on for
 * the user) and the Remote sync tab (shown when the server runs remote sync), combined into the
 * one IntegrationTabSource the Integrations page reads. Another build may replace this file to
 * bind a source that adds its own tabs; it should keep these four, by adding its source to the
 * combined list. `app.config.ts` spreads these into the application config.
 */
export const integrationTabProviders: Provider[] = [
  DiscordIntegrationTabSource,
  BuzzIntegrationTabSource,
  SlackIntegrationTabSource,
  RemoteSyncIntegrationTabSource,
  {
    provide: IntegrationTabSource,
    useFactory: (
      discord: DiscordIntegrationTabSource,
      buzz: BuzzIntegrationTabSource,
      slack: SlackIntegrationTabSource,
      remoteSync: RemoteSyncIntegrationTabSource,
    ) => new CombinedIntegrationTabs([discord, buzz, slack, remoteSync]),
    deps: [DiscordIntegrationTabSource, BuzzIntegrationTabSource, SlackIntegrationTabSource, RemoteSyncIntegrationTabSource],
  },
];

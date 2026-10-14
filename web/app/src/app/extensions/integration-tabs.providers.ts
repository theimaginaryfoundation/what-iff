import { Injectable, Provider, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { IntegrationTab, IntegrationTabSource, combineIntegrationTabs } from '../core/services/integration-tab-source';
import { DiscordIntegrationTabSource } from '../features/discord/discord-sources';
import { RemoteSyncIntegrationTabSource } from '../features/remote-sync/remote-sync-sources';

/** This build's own tabs: Discord, then Remote sync, each shown only when the server runs it. */
@Injectable()
export class FirstPartyIntegrationTabSource extends IntegrationTabSource {
  private readonly discord = inject(DiscordIntegrationTabSource);
  private readonly remoteSync = inject(RemoteSyncIntegrationTabSource);

  tabs(): Observable<IntegrationTab[]> {
    return combineIntegrationTabs(this.discord, this.remoteSync);
  }
}

/**
 * DI providers for extra Integrations tabs (swap-point file).
 *
 * This build contributes the Discord tab (shown when the server runs the relay)
 * and the Remote sync tab (shown when the server runs remote sync). Another build
 * may replace this file to bind a source that adds its own tabs; it should keep
 * these, e.g. by combining FirstPartyIntegrationTabSource with its own through
 * combineIntegrationTabs. `app.config.ts` spreads these into the application config.
 */
export const integrationTabProviders: Provider[] = [
  DiscordIntegrationTabSource,
  RemoteSyncIntegrationTabSource,
  { provide: IntegrationTabSource, useClass: FirstPartyIntegrationTabSource },
];

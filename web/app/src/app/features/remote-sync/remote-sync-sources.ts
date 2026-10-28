import { Injectable, inject } from '@angular/core';
import { Observable, map } from 'rxjs';

import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { RemoteSyncTabComponent } from './remote-sync-tab.component';
import { RemoteSyncService } from './remote-sync.service';

/** The Integrations "Mullet mode" tab (remote sync). */
export const REMOTE_SYNC_TAB: IntegrationTab = {
  id: 'remote-sync',
  label: 'Mullet mode',
  tooltip: 'Keep a copy of your whatiff.chat threads on this server, to continue here',
  component: RemoteSyncTabComponent,
};

/** Adds the Remote sync tab when the server runs remote sync (self-hosted installs). */
@Injectable()
export class RemoteSyncIntegrationTabSource extends IntegrationTabSource {
  private readonly remoteSync = inject(RemoteSyncService);

  tabs(): Observable<IntegrationTab[]> {
    return this.remoteSync.available().pipe(map(on => (on ? [REMOTE_SYNC_TAB] : [])));
  }
}

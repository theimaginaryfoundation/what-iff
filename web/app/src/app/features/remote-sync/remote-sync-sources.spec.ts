import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of } from 'rxjs';

import { REMOTE_SYNC_TAB, RemoteSyncIntegrationTabSource } from './remote-sync-sources';
import { RemoteSyncService } from './remote-sync.service';

describe('Remote sync tab source', () => {
  function source(available: boolean) {
    TestBed.configureTestingModule({
      providers: [RemoteSyncIntegrationTabSource, { provide: RemoteSyncService, useValue: { available: () => of(available) } }],
    });
    return TestBed.inject(RemoteSyncIntegrationTabSource);
  }

  it('adds the tab when the server runs remote sync', async () => {
    expect(await firstValueFrom(source(true).tabs())).toEqual([REMOTE_SYNC_TAB]);
  });

  it('adds nothing when it does not', async () => {
    expect(await firstValueFrom(source(false).tabs())).toEqual([]);
  });
});

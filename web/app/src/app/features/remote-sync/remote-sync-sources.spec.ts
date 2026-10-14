import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of, throwError } from 'rxjs';

import { IntegrationTab, IntegrationTabSource, combineIntegrationTabs } from '../../core/services/integration-tab-source';
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

describe('combineIntegrationTabs', () => {
  const tab = (id: string): IntegrationTab => ({ id, label: id, component: class {} });
  const fixed = (tabs: IntegrationTab[]): IntegrationTabSource => ({ tabs: () => of(tabs) });

  it('keeps the sources in order', async () => {
    const tabs = await firstValueFrom(combineIntegrationTabs(fixed([tab('a')]), fixed([]), fixed([tab('b'), tab('c')])));
    expect(tabs.map(t => t.id)).toEqual(['a', 'b', 'c']);
  });

  it('drops a failing source rather than hiding the others', async () => {
    const failing: IntegrationTabSource = { tabs: () => throwError(() => new Error('boom')) };
    const tabs = await firstValueFrom(combineIntegrationTabs(failing, fixed([tab('b')])));
    expect(tabs.map(t => t.id)).toEqual(['b']);
  });

  it('is empty with no sources', async () => {
    expect(await firstValueFrom(combineIntegrationTabs())).toEqual([]);
  });
});

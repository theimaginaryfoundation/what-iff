import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';

import { environment } from '../../../environments/environment';
import { RemoteSyncRunStats, RemoteSyncService, runSummary } from './remote-sync.service';

describe('RemoteSyncService', () => {
  let service: RemoteSyncService;
  let http: HttpTestingController;
  const url = `${environment.apiUrl}/remote-sync`;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(RemoteSyncService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('is available when the server answers, and asks only once', async () => {
    const first = firstValueFrom(service.available());
    http.expectOne(url).flush({ configured: false, running: false });
    expect(await first).toBe(true);
    expect(await firstValueFrom(service.available())).toBe(true);
    http.expectNone(url);
  });

  it('is unavailable when the server does not route /remote-sync', async () => {
    const result = firstValueFrom(service.available());
    http.expectOne(url).flush({ message: 'not found' }, { status: 404, statusText: 'Not Found' });
    expect(await result).toBe(false);
  });

  it('saves, runs and removes on the documented routes', async () => {
    const saved = firstValueFrom(service.save({ base_url: 'https://api.whatiff.chat', token: 'wht_x', enabled: true }));
    const put = http.expectOne(url);
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ base_url: 'https://api.whatiff.chat', token: 'wht_x', enabled: true });
    put.flush({ configured: true, running: true });
    expect((await saved).configured).toBe(true);

    const run = firstValueFrom(service.runNow());
    const post = http.expectOne(`${url}/run`);
    expect(post.request.method).toBe('POST');
    post.flush({ configured: true, running: true }, { status: 202, statusText: 'Accepted' });
    await run;

    const removed = firstValueFrom(service.remove());
    const del = http.expectOne(url);
    expect(del.request.method).toBe('DELETE');
    del.flush(null, { status: 204, statusText: 'No Content' });
    await removed;
  });
});

describe('runSummary', () => {
  const zero: RemoteSyncRunStats = {
    threads_checked: 0,
    threads_created: 0,
    threads_adopted: 0,
    threads_updated: 0,
    threads_forked: 0,
    threads_failed: 0,
    threads_deferred: 0,
    messages_added: 0,
  };

  it('says what a run did in words', () => {
    expect(
      runSummary({ ...zero, threads_created: 2, threads_adopted: 1, threads_forked: 1, messages_added: 1204, threads_deferred: 5 }),
    ).toBe('3 new threads, 1 fork, 1,204 messages, 5 more next time');
    expect(runSummary({ ...zero, threads_updated: 1, messages_added: 1 })).toBe('1 updated thread, 1 message');
  });

  it('is empty for a run that changed nothing', () => {
    expect(runSummary(zero)).toBe('');
    expect(runSummary(undefined)).toBe('');
  });
});

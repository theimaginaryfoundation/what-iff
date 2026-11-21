import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { environment } from '../../../environments/environment';
import { ShareLinkService } from './share-link.service';

describe('ShareLinkService', () => {
  let http: HttpTestingController;
  let service: ShareLinkService;
  const api = environment.apiUrl;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()] });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(ShareLinkService);
  });
  afterEach(() => http.verify());

  it('reads, makes, changes and stops a file’s link, and lists them all', async () => {
    const state = firstValueFrom(service.get('f 1'));
    http.expectOne(`${api}/share-links/files/f%201`).flush({ scope: { file_name: 'a', web_app: false }, link: null });
    expect((await state).link).toBeNull();

    const made = firstValueFrom(service.share('f1', false));
    const put = http.expectOne(`${api}/share-links/files/f1`);
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ passcode_required: false });
    put.flush({ key: 'k' });
    expect((await made).key).toBe('k');

    const changed = firstValueFrom(service.update('f1', { new_passcode: true }));
    const patch = http.expectOne(`${api}/share-links/files/f1`);
    expect(patch.request.method).toBe('PATCH');
    expect(patch.request.body).toEqual({ new_passcode: true });
    patch.flush({ key: 'k' });
    await changed;

    const stopped = firstValueFrom(service.stop('f1'));
    const del = http.expectOne(`${api}/share-links/files/f1`);
    expect(del.request.method).toBe('DELETE');
    del.flush(null);
    await stopped;

    const list = firstValueFrom(service.list());
    http.expectOne(`${api}/share-links`).flush({ links: [{ key: 'a' }, { key: 'b' }] });
    expect((await list).map(l => l.key)).toEqual(['a', 'b']);
  });

  it('makes a visitor’s link on the share site', () => {
    if (!service.enabled) return; // a build without a share site offers no links
    expect(service.urlOf({ key: 'abc' })).toBe(`${service.siteUrl}/s/abc`);
  });
});

import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';

import { ProviderKeysService } from './provider-keys.service';

describe('ProviderKeysService', () => {
  let service: ProviderKeysService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr()), provideHttpClientTesting(), ProviderKeysService],
    });
    service = TestBed.inject(ProviderKeysService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());

  it('knows nothing until access is loaded', () => {
    expect(service.access()).toBeNull();
    expect(service.accountsSupplyKeys()).toBe(false);
    expect(service.needsFirstKey()).toBe(false);
  });

  it('needs a first key when accounts supply keys and this one has none', () => {
    service.refreshAccess().subscribe();
    httpMock.expectOne(`${environment.apiUrl}/provider-access`).flush({ keys_from: 'account', providers: [] });

    expect(service.accountsSupplyKeys()).toBe(true);
    expect(service.needsFirstKey()).toBe(true);
  });

  it('does not ask for a key once the account has one', () => {
    service.refreshAccess().subscribe();
    httpMock.expectOne(`${environment.apiUrl}/provider-access`).flush({ keys_from: 'account', providers: ['openai'] });

    expect(service.needsFirstKey()).toBe(false);
  });

  it('never asks where the deployment supplies keys', () => {
    service.refreshAccess().subscribe();
    httpMock.expectOne(`${environment.apiUrl}/provider-access`).flush({ keys_from: 'deployment', providers: [] });

    expect(service.accountsSupplyKeys()).toBe(false);
    expect(service.needsFirstKey()).toBe(false);
  });

  // A network blip must not flash the first-run prompt at someone who has keys.
  it('keeps the last answer when a refresh fails', () => {
    service.refreshAccess().subscribe();
    httpMock.expectOne(`${environment.apiUrl}/provider-access`).flush({ keys_from: 'account', providers: ['openai'] });

    service.refreshAccess().subscribe();
    httpMock.expectOne(`${environment.apiUrl}/provider-access`).flush('down', { status: 503, statusText: 'Unavailable' });

    expect(service.access()).toEqual({ keys_from: 'account', providers: ['openai'] });
    expect(service.needsFirstKey()).toBe(false);
  });

  it('sets a key with the current password', () => {
    service.set('openai', 'sk-new', 'hunter2').subscribe();

    const req = httpMock.expectOne(`${environment.apiUrl}/provider-keys/openai`);
    expect(req.request.method).toBe('PUT');
    expect(req.request.body).toEqual({ key: 'sk-new', current_password: 'hunter2' });
    req.flush({ provider: 'openai', hint: '-new', updated_at: '2026-10-15T00:00:00Z' });
  });

  it('lists and removes keys', () => {
    service.list().subscribe();
    expect(httpMock.expectOne(`${environment.apiUrl}/provider-keys`).request.method).toBe('GET');

    service.remove('anthropic').subscribe();
    const req = httpMock.expectOne(`${environment.apiUrl}/provider-keys/anthropic`);
    expect(req.request.method).toBe('DELETE');
    req.flush(null);
  });
});

import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';

import { environment } from '../../../environments/environment';
import { BuzzJoinPolicy, BuzzService, buzzBindingLabel, termsRequired } from './buzz.service';

const base = `${environment.apiUrl}/buzz`;

describe('BuzzService', () => {
  let service: BuzzService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(BuzzService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  describe('available', () => {
    it('is on when the relay answers, and asks only once', async () => {
      const first = firstValueFrom(service.available());
      http.expectOne(`${base}/bindings`).flush([]);
      expect(await first).toBe(true);
      expect(await firstValueFrom(service.available())).toBe(true);
      http.expectNone(`${base}/bindings`);
    });

    it('is off when the server does not route /buzz', async () => {
      const result = firstValueFrom(service.available());
      http.expectOne(`${base}/bindings`).flush({ message: 'not found' }, { status: 404, statusText: 'Not Found' });
      expect(await result).toBe(false);
    });

    it('stays on for other errors, so they surface where the user can see them', async () => {
      const result = firstValueFrom(service.available());
      http.expectOne(`${base}/bindings`).flush({ message: 'boom' }, { status: 500, statusText: 'Server Error' });
      expect(await result).toBe(true);
    });
  });

  it('sends the routes and bodies handlers.go expects', () => {
    service.createAgent('p1').subscribe();
    let req = http.expectOne(`${base}/agents`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ personality_id: 'p1' });
    req.flush({});

    service.setAgentEnabled('a1', false).subscribe();
    req = http.expectOne(`${base}/agents/a1`);
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ enabled: false });
    req.flush({});

    service.addCommunity('a1', { relay_url: 'https://acme.communities.buzz.xyz', accept_terms_version: 'v2' }).subscribe();
    req = http.expectOne(`${base}/agents/a1/communities`);
    expect(req.request.body).toEqual({ relay_url: 'https://acme.communities.buzz.xyz', accept_terms_version: 'v2' });
    req.flush({});

    service.previewPolicy('https://acme.communities.buzz.xyz').subscribe();
    req = http.expectOne(r => r.url === `${base}/policy`);
    expect(req.request.params.get('relay_url')).toBe('https://acme.communities.buzz.xyz');
    req.flush({ name: 'Acme', relay_url: 'wss://acme', policy: null });

    service.joinChannel('c1', 'ch1').subscribe();
    req = http.expectOne(`${base}/communities/c1/channels/ch1/join`);
    expect(req.request.method).toBe('POST');
    req.flush(null);

    service.setPostNextReply('chat', 'b1', true).subscribe();
    expect(http.expectOne(`${base}/chats/chat/pending/b1`).request.method).toBe('PUT');
    service.setPostNextReply('chat', 'b1', false).subscribe();
    expect(http.expectOne(`${base}/chats/chat/pending/b1`).request.method).toBe('DELETE');
  });
});

describe('termsRequired', () => {
  const policy: BuzzJoinPolicy = { terms_markdown: '# Terms', privacy_markdown: '', age_attestation_required: false, version: 'v1' };

  it('reads the terms off a 409 buzz_terms_required', () => {
    const e = new HttpErrorResponse({ status: 409, error: { message: 'Read them', code: 'buzz_terms_required', details: { policy } } });
    expect(termsRequired(e)).toEqual(policy);
  });

  it('is null for any other failure, including other conflicts', () => {
    expect(
      termsRequired(
        new HttpErrorResponse({ status: 409, error: { message: 'That persona is already in that community.', code: 'conflict' } }),
      ),
    ).toBeNull();
    expect(termsRequired(new HttpErrorResponse({ status: 400, error: { code: 'buzz_terms_required', details: { policy } } }))).toBeNull();
    expect(termsRequired(new HttpErrorResponse({ status: 409, error: 'gateway' }))).toBeNull();
    expect(termsRequired(new Error('x'))).toBeNull();
  });
});

describe('buzzBindingLabel', () => {
  it('labels a binding with its community when known', () => {
    expect(buzzBindingLabel({ channel_name: 'general', channel_id: 'c', community_name: 'Acme' })).toBe('#general · Acme');
    expect(buzzBindingLabel({ channel_name: 'general', channel_id: 'c' }, 'Home')).toBe('#general · Home');
    expect(buzzBindingLabel({ channel_name: '', channel_id: 'abc' })).toBe('#abc');
  });
});

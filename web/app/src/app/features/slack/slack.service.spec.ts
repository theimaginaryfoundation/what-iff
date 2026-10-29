import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';

import { environment } from '../../../environments/environment';
import { SLACK_USER_IDS, SlackService, slackBindingLabel, tokenLooksWrong } from './slack.service';

const base = `${environment.apiUrl}/slack`;

describe('SlackService', () => {
  let service: SlackService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(SlackService);
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

    it('is off when no answer can be read (network error, CORS), so it fails closed', async () => {
      const result = firstValueFrom(service.available());
      http.expectOne(`${base}/bindings`).error(new ProgressEvent('error'), { status: 0, statusText: 'Unknown Error' });
      expect(await result).toBe(false);
    });

    it('is off when /slack answers 404 (switched off, or not on for this user)', async () => {
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
    service.manifest('p1').subscribe();
    let req = http.expectOne(r => r.url === `${base}/manifest`);
    expect(req.request.params.get('personality_id')).toBe('p1');
    req.flush({ manifest: {}, create_url: 'https://api.slack.com/apps?new_app=1', scopes: [], events: [] });

    service.addApp({ personality_id: 'p1', bot_token: 'xoxb-1', app_token: 'xapp-1' }).subscribe();
    req = http.expectOne(`${base}/apps`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ personality_id: 'p1', bot_token: 'xoxb-1', app_token: 'xapp-1' });
    req.flush({});

    service.updateApp('a1', { enabled: false }).subscribe();
    req = http.expectOne(`${base}/apps/a1`);
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ enabled: false });
    req.flush({});

    service.removeApp('a1').subscribe();
    expect(http.expectOne(`${base}/apps/a1`).request.method).toBe('DELETE');

    service.listChannels('a1').subscribe();
    expect(http.expectOne(`${base}/apps/a1/channels`).request.method).toBe('GET');

    service.joinChannel('a1', 'C1').subscribe();
    req = http.expectOne(`${base}/apps/a1/channels/C1/join`);
    expect(req.request.method).toBe('POST');
    req.flush({ id: 'C1', name: 'general', is_private: false, is_member: true, is_archived: false });

    service.addBinding({ app_id: 'a1', channel_id: 'C1', channel_name: 'general', is_private: false }).subscribe();
    req = http.expectOne(`${base}/bindings`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ app_id: 'a1', channel_id: 'C1', channel_name: 'general', is_private: false });
    req.flush({});

    service.updateBinding('b1', { reactivate: true }).subscribe();
    req = http.expectOne(`${base}/bindings/b1`);
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ reactivate: true });
    req.flush({});

    service.chatState('chat').subscribe();
    expect(http.expectOne(`${base}/chats/chat`).request.method).toBe('GET');

    service.setPostNextReply('chat', 'b1', true).subscribe();
    expect(http.expectOne(`${base}/chats/chat/pending/b1`).request.method).toBe('PUT');
    service.setPostNextReply('chat', 'b1', false).subscribe();
    expect(http.expectOne(`${base}/chats/chat/pending/b1`).request.method).toBe('DELETE');
  });
});

describe('slackBindingLabel', () => {
  it('labels a binding with its workspace when known', () => {
    expect(slackBindingLabel({ channel_name: 'general', channel_id: 'C1', team_name: 'Acme' })).toBe('#general · Acme');
    expect(slackBindingLabel({ channel_name: 'general', channel_id: 'C1' }, 'Home')).toBe('#general · Home');
    expect(slackBindingLabel({ channel_name: '', channel_id: 'C1' })).toBe('#C1');
  });
});

describe('SLACK_USER_IDS', () => {
  it('takes member ids and pasted mentions, upper-cased', () => {
    expect(SLACK_USER_IDS.parse('U0123ABC')).toBe('U0123ABC');
    expect(SLACK_USER_IDS.parse('w0123abc')).toBe('W0123ABC');
    expect(SLACK_USER_IDS.parse('<@U0123ABC>')).toBe('U0123ABC');
    expect(SLACK_USER_IDS.parse('<@U0123ABC|alice>')).toBe('U0123ABC');
    expect(SLACK_USER_IDS.parse('@U0123ABC')).toBe('U0123ABC');
  });

  it('refuses names, channel ids and bot ids', () => {
    expect(SLACK_USER_IDS.parse('alice')).toBeNull();
    expect(SLACK_USER_IDS.parse('C0123ABC')).toBeNull();
    expect(SLACK_USER_IDS.parse('B0123ABC')).toBeNull();
    expect(SLACK_USER_IDS.parse('U-12')).toBeNull();
    // Names that start with U or W are not ids: taken as one, an allow list would match nobody.
    expect(SLACK_USER_IDS.parse('will')).toBeNull();
    expect(SLACK_USER_IDS.parse('@walter')).toBeNull();
    expect(SLACK_USER_IDS.parse('Uma')).toBeNull();
    expect(SLACK_USER_IDS.parse('WENDYWEN')).toBeNull();
  });
});

describe('tokenLooksWrong', () => {
  it('flags a token of the wrong kind once it is long enough to tell', () => {
    expect(tokenLooksWrong('', 'xoxb-')).toBe(false);
    expect(tokenLooksWrong('xox', 'xoxb-')).toBe(false);
    expect(tokenLooksWrong(' xoxb-123 ', 'xoxb-')).toBe(false);
    expect(tokenLooksWrong('xapp-1-A1', 'xoxb-')).toBe(true);
    expect(tokenLooksWrong('xoxp-123', 'xoxb-')).toBe(true);
  });
});

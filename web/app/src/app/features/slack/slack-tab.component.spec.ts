import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { PersonalityService } from '../../core/services/personality.service';
import { SlackTabComponent } from './slack-tab.component';
import { SlackApp, SlackBinding, SlackChannel, SlackManifest, SlackService } from './slack.service';

const app: SlackApp = {
  id: 'a1',
  personality_id: 'p1',
  team_id: 'T1',
  team_name: 'Acme',
  team_url: 'https://acme.slack.com/',
  app_id: 'A1',
  bot_user_id: 'U0BOT',
  bot_id: 'B1',
  bot_name: 'vix',
  status: 'active',
  settings_url: 'https://api.slack.com/apps/A1',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '',
};
const binding: SlackBinding = {
  id: 'b1',
  app_id: 'a1',
  team_name: 'Acme',
  chat_id: 'chat-1',
  channel_id: 'C-general',
  channel_name: 'general',
  is_private: false,
  inbound_enabled: true,
  allow_user_ids: [],
  deny_user_ids: [],
  answer_agents: false,
  allow_unrestricted: false,
  chat_sandboxed: true,
  status: 'active',
  created_at: '',
  updated_at: '',
};
const manifest: SlackManifest = {
  manifest: { display_information: { name: 'Echo' } },
  create_url: 'https://api.slack.com/apps?new_app=1&manifest_json=%7B%7D',
  scopes: ['chat:write', 'channels:history'],
  events: ['message.channels'],
};
const channels: SlackChannel[] = [
  { id: 'C-general', name: 'general', is_private: false, is_member: true, is_archived: false, num_members: 12 },
  { id: 'C-dev', name: 'dev', is_private: false, is_member: true, is_archived: false },
  { id: 'C-lobby', name: 'lobby', is_private: false, is_member: false, is_archived: false },
  { id: 'G-secret', name: 'secret', is_private: true, is_member: false, is_archived: false },
];

async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await new Promise(r => setTimeout(r, 0));
}

describe('SlackTabComponent', () => {
  let slack: Record<string, ReturnType<typeof vi.fn>>;
  let confirm: ReturnType<typeof vi.fn>;

  async function create(opts: { apps?: SlackApp[]; bindings?: SlackBinding[] } = {}) {
    slack = {
      listApps: vi.fn(() => of(opts.apps ?? [app])),
      listBindings: vi.fn(() => of(opts.bindings ?? [binding])),
      manifest: vi.fn(() => of(manifest)),
      addApp: vi.fn(() => of({ ...app, id: 'a2', personality_id: 'p2', team_name: 'Beta', bot_name: 'echo' })),
      updateApp: vi.fn((_id: string, body: Record<string, unknown>) =>
        of({ ...app, ...(body['enabled'] === false ? { status: 'disabled' } : {}) }),
      ),
      removeApp: vi.fn(() => of(undefined)),
      listChannels: vi.fn(() => of(channels)),
      joinChannel: vi.fn(() => of({ ...channels[2], is_member: true })),
      addBinding: vi.fn(() => of({ ...binding, id: 'b-new', channel_id: 'C-dev', channel_name: 'dev' })),
      updateBinding: vi.fn((_id: string, body: Partial<SlackBinding>) => of({ ...binding, ...body })),
      removeBinding: vi.fn(() => of(undefined)),
    };
    confirm = vi.fn(async () => true);
    TestBed.configureTestingModule({
      imports: [SlackTabComponent],
      providers: [
        provideRouter([]),
        { provide: SlackService, useValue: slack },
        { provide: ConfirmationService, useValue: { confirm } },
        {
          provide: ChatService,
          useValue: {
            listChatsPage: vi.fn(() =>
              of({
                results: [
                  { id: 'chat-open', name: 'My main thread', context_scope: 'account' },
                  { id: 'chat-safe', name: 'Sandbox', context_scope: 'sandbox' },
                ],
                total_count: 2,
                page: 1,
              }),
            ),
          },
        },
        {
          provide: PersonalityService,
          useValue: {
            listPersonalities: () =>
              of({
                results: [
                  { id: 'p1', name: 'Vix' },
                  { id: 'p2', name: 'Echo' },
                ],
                total_count: 2,
                page: 1,
              }),
          },
        },
      ],
    });
    const fixture = TestBed.createComponent(SlackTabComponent);
    fixture.detectChanges();
    await settle();
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const q = <T extends HTMLElement = HTMLElement>(id: string, scope: ParentNode = root) =>
      scope.querySelector(`[data-testid=${id}]`) as T | null;
    const all = (id: string, scope: ParentNode = root) => Array.from(scope.querySelectorAll<HTMLElement>(`[data-testid=${id}]`));
    const refresh = async () => {
      await settle();
      fixture.detectChanges();
    };
    const click = async (id: string, scope?: ParentNode) => {
      q<HTMLButtonElement>(id, scope)!.click();
      await refresh();
    };
    const type = async (id: string, value: string, scope?: ParentNode) => {
      const el = q<HTMLInputElement>(id, scope)!;
      el.value = value;
      el.dispatchEvent(new Event('input'));
      await refresh();
    };
    const tick = async (id: string, checked = true, scope?: ParentNode) => {
      const el = q<HTMLInputElement>(id, scope)!;
      el.checked = checked;
      el.dispatchEvent(new Event('change'));
      await refresh();
    };
    const choose = async (id: string, value: string) => {
      const el = q<HTMLSelectElement>(id)!;
      el.value = value;
      el.dispatchEvent(new Event('change'));
      await refresh();
    };
    return { fixture, root, q, all, click, type, tick, choose, refresh };
  }

  it('lists each app with its persona, workspace, bot, status and settings link', async () => {
    const { q, all } = await create();
    expect(all('slack-app').length).toBe(1);
    expect(q('slack-app')!.textContent).toContain('Vix');
    expect(q('slack-app')!.textContent).toContain('in Acme');
    expect(q('slack-app-bot')!.textContent).toContain('@vix');
    expect(q('slack-app-status')!.textContent?.trim()).toBe('Active');
    expect(q<HTMLAnchorElement>('slack-app-settings')!.href).toBe('https://api.slack.com/apps/A1');
    expect(q<HTMLAnchorElement>('slack-app-settings')!.target).toBe('_blank');
    expect(q('slack-binding')!.textContent).toContain('#general · Acme');
  });

  it('shows an invalid token or paused app with its reason, and offers channels only while active', async () => {
    const { all, q } = await create({
      apps: [
        { ...app, status: 'invalid_token', last_error: 'invalid_auth: the token was revoked' },
        { ...app, id: 'a2', status: 'disabled' },
      ],
      bindings: [],
    });
    expect(all('slack-app-status').map(s => s.textContent?.trim())).toEqual(['Invalid token', 'Paused']);
    expect(q('slack-app-error')!.textContent).toContain('invalid_auth');
    expect(q('slack-channels-open')).toBeNull();
    expect(all('slack-app-pause').map(b => b.textContent?.trim())).toEqual(['Pause', 'Resume']);
  });

  describe('connecting a persona', () => {
    it('opens the manifest link, walks through the tokens, and adds the app', async () => {
      const { q, all, choose, type, click } = await create();
      expect(q('slack-steps')).toBeNull();
      // Every persona is offered: one app per workspace, so a persona may have several.
      expect(Array.from(q<HTMLSelectElement>('slack-persona-select')!.options).map(o => o.value)).toEqual(['', 'p1', 'p2']);

      await choose('slack-persona-select', 'p2');
      expect(slack['manifest']).toHaveBeenCalledWith('p2');
      const create_ = q<HTMLAnchorElement>('slack-create-app')!;
      expect(create_.getAttribute('href')).toBe(manifest.create_url);
      expect(create_.target).toBe('_blank');
      expect(create_.rel).toContain('noopener');
      const steps = q('slack-steps')!.textContent!;
      expect(steps).toContain('Install to Workspace');
      expect(steps).toContain('Bot User OAuth Token');
      expect(steps).toContain('connections:write');
      expect(q('slack-connect-notes')!.textContent).toContain('10 apps');
      expect(q('slack-connect-notes')!.textContent).toContain('portrait');

      expect(q<HTMLButtonElement>('slack-connect-submit')!.disabled).toBe(true);
      await type('slack-bot-token', ' xoxb-123 ');
      await type('slack-app-token', 'xapp-1-A2-456');
      expect(q<HTMLButtonElement>('slack-connect-submit')!.disabled).toBe(false);
      await click('slack-connect-submit');

      expect(slack['addApp']).toHaveBeenCalledWith({ personality_id: 'p2', bot_token: 'xoxb-123', app_token: 'xapp-1-A2-456' });
      expect(all('slack-app').length).toBe(2);
      expect(q('slack-notice')!.textContent).toContain('Echo is in Beta as @echo');
      expect(q('slack-steps')).toBeNull();
    });

    it('catches a token pasted into the wrong field before sending', async () => {
      const { q, choose, type } = await create();
      await choose('slack-persona-select', 'p1');
      await type('slack-bot-token', 'xapp-1-A1-123');
      await type('slack-app-token', 'xapp-1-A1-456');
      expect(q('slack-bot-token-hint')!.textContent).toContain('xoxb-');
      expect(q('slack-app-token-hint')).toBeNull();
      expect(q<HTMLButtonElement>('slack-connect-submit')!.disabled).toBe(true);
    });

    it("shows the server's reason when Slack or the server refuses", async () => {
      const { q, choose, type, click } = await create();
      slack['addApp'].mockReturnValueOnce(
        throwError(
          () =>
            new HttpErrorResponse({
              status: 409,
              error: { message: 'Vix already has an app in that workspace.', code: 'conflict' },
            }),
        ),
      );
      await choose('slack-persona-select', 'p1');
      await type('slack-bot-token', 'xoxb-1');
      await type('slack-app-token', 'xapp-1');
      await click('slack-connect-submit');
      expect(q('slack-error')!.textContent).toContain('already has an app in that workspace');
      // The form keeps what was typed, to fix and try again.
      expect(q<HTMLInputElement>('slack-bot-token')!.value).toBe('xoxb-1');
    });
  });

  describe('an app', () => {
    it('refreshes, pauses and replaces only the tokens given', async () => {
      const { click, type, q } = await create();
      await click('slack-app-refresh');
      expect(slack['updateApp']).toHaveBeenCalledWith('a1', { refresh: true });
      await click('slack-app-pause');
      expect(slack['updateApp']).toHaveBeenLastCalledWith('a1', { enabled: false });
      expect(q('slack-app-status')!.textContent?.trim()).toBe('Paused');

      await click('slack-app-tokens-open');
      expect(q<HTMLButtonElement>('slack-app-tokens-save')!.disabled).toBe(true);
      await type('slack-app-app-token', 'xapp-1-A1-new');
      await click('slack-app-tokens-save');
      expect(slack['updateApp']).toHaveBeenLastCalledWith('a1', { app_token: 'xapp-1-A1-new' });
      expect(q('slack-app-tokens-form')).toBeNull();
    });

    it('removes an app, with its bindings, once confirmed', async () => {
      const { click, all } = await create();
      await click('slack-app-remove');
      expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ title: 'Remove Slack app', type: 'danger' }));
      expect(slack['removeApp']).toHaveBeenCalledWith('a1');
      expect(all('slack-app').length).toBe(0);
      expect(all('slack-binding').length).toBe(0);
    });

    it('keeps the app when the removal is cancelled', async () => {
      const { click, all } = await create();
      confirm.mockResolvedValueOnce(false);
      await click('slack-app-remove');
      expect(slack['removeApp']).not.toHaveBeenCalled();
      expect(all('slack-app').length).toBe(1);
    });
  });

  describe('channels', () => {
    async function openChannels() {
      const t = await create();
      await t.click('slack-channels-open');
      return t;
    }
    const channelRow = (all: (id: string) => HTMLElement[], name: string) =>
      all('slack-channel').find(r => r.textContent?.includes(`#${name}`))!;

    it('lists channels: connected, connectable, joinable, and private', async () => {
      const { all } = await openChannels();
      expect(slack['listChannels']).toHaveBeenCalledWith('a1');
      expect(channelRow(all, 'general').textContent).toContain('Connected');
      expect(channelRow(all, 'general').textContent).toContain('12 members');
      expect(channelRow(all, 'dev').querySelector('[data-testid=slack-channel-member]')).toBeTruthy();
      expect(channelRow(all, 'dev').querySelector('[data-testid=slack-channel-connect]')).toBeTruthy();
      expect(channelRow(all, 'lobby').querySelector('[data-testid=slack-channel-join]')).toBeTruthy();
      expect(channelRow(all, 'secret').textContent).toContain('/invite @vix');
      expect(channelRow(all, 'secret').querySelector('[data-testid=slack-channel-join]')).toBeNull();
    });

    it('joins a public channel, after which it can be connected', async () => {
      const { all, click } = await openChannels();
      await click('slack-channel-join', channelRow(all, 'lobby'));
      expect(slack['joinChannel']).toHaveBeenCalledWith('a1', 'C-lobby');
      expect(channelRow(all, 'lobby').querySelector('[data-testid=slack-channel-connect]')).toBeTruthy();
    });

    it('shows why a join failed', async () => {
      const { all, click, q } = await openChannels();
      slack['joinChannel'].mockReturnValueOnce(
        throwError(
          () =>
            new HttpErrorResponse({
              status: 400,
              error: { message: 'That channel is private: invite the persona in Slack (/invite) instead.', code: 'bad_request' },
            }),
        ),
      );
      await click('slack-channel-join', channelRow(all, 'lobby'));
      expect(q('slack-error')!.textContent).toContain('/invite');
    });

    it('connects a channel to a new sandboxed relay thread by default', async () => {
      const { all, click, q } = await openChannels();
      await click('slack-channel-connect', channelRow(all, 'dev'));
      expect(q('slack-unrestricted-warning')).toBeNull();
      expect(q('slack-connect-empty-allow-note')!.textContent).toContain('Anyone in this channel can talk to this persona.');
      await click('slack-connect-channel');
      expect(slack['addBinding']).toHaveBeenCalledWith({
        app_id: 'a1',
        channel_id: 'C-dev',
        channel_name: 'dev',
        is_private: false,
        allow_user_ids: [],
      });
      expect(all('slack-binding').length).toBe(2);
    });

    it('needs the acknowledgement to connect an existing thread that is not sandboxed', async () => {
      const { all, click, tick, q, choose } = await openChannels();
      await click('slack-channel-connect', channelRow(all, 'dev'));
      await choose('slack-thread-select', 'chat-open');

      expect(q('slack-unrestricted-warning')!.textContent).toContain(
        'Anyone allowed to mention the persona can use what this thread reads',
      );
      expect(q<HTMLButtonElement>('slack-connect-channel')!.disabled).toBe(true);
      await tick('slack-ack-unrestricted');
      expect(q<HTMLButtonElement>('slack-connect-channel')!.disabled).toBe(false);
      await click('slack-connect-channel');
      expect(slack['addBinding']).toHaveBeenCalledWith(expect.objectContaining({ chat_id: 'chat-open', allow_unrestricted: true }));
    });

    it('connects an existing sandboxed thread without the acknowledgement', async () => {
      const { all, click, q, choose } = await openChannels();
      await click('slack-channel-connect', channelRow(all, 'dev'));
      await choose('slack-thread-select', 'chat-safe');
      expect(q('slack-unrestricted-warning')).toBeNull();
      await click('slack-connect-channel');
      const body = slack['addBinding'].mock.calls[0][0] as Record<string, unknown>;
      expect(body['chat_id']).toBe('chat-safe');
      expect('allow_unrestricted' in body).toBe(false);
    });
  });

  describe('a binding', () => {
    it('toggles mentions and answering other apps’ bots, which starts off with the loop-guard hint', async () => {
      const { tick, q } = await create();
      expect(q<HTMLInputElement>('slack-binding-agents')!.checked).toBe(false);
      expect(q('slack-binding')!.textContent).toContain('a person has to speak');
      await tick('slack-binding-agents');
      expect(slack['updateBinding']).toHaveBeenCalledWith('b1', { answer_agents: true });
      await tick('slack-binding-inbound', false);
      expect(slack['updateBinding']).toHaveBeenLastCalledWith('b1', { inbound_enabled: false });
    });

    it('links to the relay thread and says when anyone may mention the persona', async () => {
      const { q } = await create();
      expect(q<HTMLAnchorElement>('slack-binding-open-thread')!.getAttribute('href')).toBe('/chat/chat-1');
      expect(q('slack-binding-open-to-anyone')).toBeTruthy();
      expect(q('slack-binding-sandboxed')).toBeTruthy();
    });

    it('saves who can mention the persona, as Slack member ids', async () => {
      const { fixture, click, q } = await create();
      const inputs = fixture.nativeElement.querySelectorAll('[data-testid=slack-binding] app-relay-id-chips input');
      const allow = inputs[0] as HTMLInputElement;
      allow.value = '<@u0123abc> nobody';
      allow.dispatchEvent(new Event('blur'));
      fixture.detectChanges();
      expect(q('slack-empty-allow-note')).toBeNull();
      expect(allow.value).toBe('nobody');
      const deny = inputs[1] as HTMLInputElement;
      deny.value = 'W999';
      deny.dispatchEvent(new Event('blur'));
      fixture.detectChanges();
      await click('slack-binding-save-access');
      expect(slack['updateBinding']).toHaveBeenCalledWith('b1', { allow_user_ids: ['U0123ABC'], deny_user_ids: ['W999'] });
    });

    it('reactivates when broken, takes the acknowledgement when not sandboxed, and disconnects', async () => {
      const broken: SlackBinding = { ...binding, status: 'broken', last_error: 'not_in_channel', chat_sandboxed: false };
      const { click, tick, q, all } = await create({ bindings: [broken] });
      slack['updateBinding'].mockImplementation((_id: string, body: Partial<SlackBinding>) => of({ ...broken, ...body }));
      expect(q('slack-binding-status')!.textContent?.trim()).toBe('Broken');
      expect(q('slack-binding-error')!.textContent).toContain('not_in_channel');
      expect(q('slack-binding-unsandboxed')!.textContent).toContain('paused');
      await click('slack-binding-reactivate');
      expect(slack['updateBinding']).toHaveBeenCalledWith('b1', { reactivate: true });
      await tick('slack-binding-ack');
      expect(slack['updateBinding']).toHaveBeenLastCalledWith('b1', { allow_unrestricted: true });
      expect(q('slack-binding-unsandboxed')!.textContent).toContain('acknowledged');
      await click('slack-binding-remove');
      expect(slack['removeBinding']).toHaveBeenCalledWith('b1');
      expect(all('slack-binding').length).toBe(0);
    });
  });
});

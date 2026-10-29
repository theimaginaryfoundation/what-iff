import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideMarkdown } from 'ngx-markdown';
import { of, throwError } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { PersonalityService } from '../../core/services/personality.service';
import { BuzzTabComponent } from './buzz-tab.component';
import { BuzzAgent, BuzzBinding, BuzzCommunity, BuzzJoinPolicy, BuzzService } from './buzz.service';

const HEX = '7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e';
const NPUB = 'npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg';

const community: BuzzCommunity = {
  id: 'c1',
  agent_id: 'a1',
  relay_url: 'wss://acme.communities.buzz.xyz',
  name: 'Acme',
  relay_pubkey: 'relay',
  attested: false,
  status: 'active',
  created_at: '',
  updated_at: '',
};
const agent: BuzzAgent = {
  id: 'a1',
  personality_id: 'p1',
  pubkey: 'ab'.repeat(32),
  npub: 'npub1persona',
  status: 'active',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '',
  persona_name: 'Vix',
  communities: [community],
};
const binding: BuzzBinding = {
  id: 'b1',
  community_id: 'c1',
  chat_id: 'chat-1',
  channel_id: 'ch-general',
  channel_name: 'general',
  inbound_enabled: true,
  allow_pubkeys: [],
  deny_pubkeys: [],
  answer_agents: false,
  allow_unrestricted: false,
  chat_sandboxed: true,
  status: 'active',
  created_at: '',
  updated_at: '',
};
const policy: BuzzJoinPolicy = {
  terms_markdown: '# House rules\n\nBe kind.',
  privacy_markdown: '',
  age_attestation_required: false,
  version: 'v3',
};
const termsConflict = (p: BuzzJoinPolicy = policy) =>
  new HttpErrorResponse({
    status: 409,
    error: { message: 'This community has join terms.', code: 'buzz_terms_required', details: { policy: p } },
  });

async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await new Promise(r => setTimeout(r, 0));
}

describe('BuzzTabComponent', () => {
  let buzz: Record<string, ReturnType<typeof vi.fn>>;

  async function create(opts: { agents?: BuzzAgent[]; bindings?: BuzzBinding[] } = {}) {
    buzz = {
      listAgents: vi.fn(() => of(opts.agents ?? [agent])),
      listBindings: vi.fn(() => of(opts.bindings ?? [binding])),
      createAgent: vi.fn(() => of({ ...agent, id: 'a2', personality_id: 'p2', persona_name: 'Echo', npub: 'npub1echo', communities: [] })),
      setAgentEnabled: vi.fn((_id: string, enabled: boolean) => of({ ...agent, status: enabled ? 'active' : 'disabled' })),
      syncProfile: vi.fn(() => of({ published: 1, failed: [] })),
      removeAgent: vi.fn(() => of(undefined)),
      addCommunity: vi.fn(() => of({ ...community, id: 'c2', name: 'Beta', status: 'needs_access', last_error: 'Not a member yet.' })),
      updateCommunity: vi.fn((_id: string, body: Record<string, unknown>) =>
        of({
          ...community,
          ...(body['enabled'] === false ? { status: 'disabled' } : {}),
          ...(body['attestation'] ? { attested: true } : {}),
        }),
      ),
      removeCommunity: vi.fn(() => of(undefined)),
      listChannels: vi.fn(() =>
        of([
          { id: 'ch-general', name: 'general', public: true, member: true },
          { id: 'ch-dev', name: 'dev', public: true, member: true },
          { id: 'ch-lobby', name: 'lobby', public: true, member: false },
          { id: 'ch-secret', name: 'secret', public: false, member: false },
        ]),
      ),
      joinChannel: vi.fn(() => of(undefined)),
      addBinding: vi.fn(() => of({ ...binding, id: 'b-new', channel_id: 'ch-dev', channel_name: 'dev' })),
      updateBinding: vi.fn((_id: string, body: Partial<BuzzBinding>) => of({ ...binding, ...body })),
      removeBinding: vi.fn(() => of(undefined)),
    };
    TestBed.configureTestingModule({
      imports: [BuzzTabComponent],
      providers: [
        provideRouter([]),
        provideMarkdown(),
        { provide: BuzzService, useValue: buzz },
        { provide: ConfirmationService, useValue: { confirm: vi.fn(async () => true) } },
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
    const fixture = TestBed.createComponent(BuzzTabComponent);
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
    return { fixture, root, q, all, click, type, tick, refresh };
  }

  it('lists each identity with its npub, the copy hint, and its communities', async () => {
    const { root, q, all } = await create();
    expect(all('buzz-agent').length).toBe(1);
    expect(q('buzz-agent-npub')!.textContent?.trim()).toBe('npub1persona');
    expect(root.textContent).toContain('Share this with a community admin, or use an invite link');
    expect(q('buzz-community')!.textContent).toContain('Acme');
    expect(q('buzz-community-status')!.textContent?.trim()).toBe('Active');
    expect(q('buzz-binding')!.textContent).toContain('#general · Acme');
  });

  it('shows each community status, with the reason when it needs access', async () => {
    const { all, q } = await create({
      agents: [
        {
          ...agent,
          communities: [
            { ...community, id: 'x1', status: 'needs_access', last_error: 'Not a member yet. Ask an admin to add npub1persona.' },
            { ...community, id: 'x2', status: 'error', last_error: 'The community could not be reached.' },
            { ...community, id: 'x3', status: 'disabled' },
          ],
        },
      ],
      bindings: [],
    });
    expect(all('buzz-community-status').map(s => s.textContent?.trim())).toEqual(['Needs access', 'Error', 'Paused']);
    expect(q('buzz-community-error')!.textContent).toContain('Ask an admin to add npub1persona');
    // Channels are offered only once the persona is let in.
    expect(q('buzz-channels-open')).toBeNull();
  });

  it('offers only personas without an identity, and connects one', async () => {
    const { fixture, all, click, q } = await create();
    const options = Array.from(q<HTMLSelectElement>('buzz-persona-select')!.options).map(o => o.value);
    expect(options).toEqual(['', 'p2']);

    const select = q<HTMLSelectElement>('buzz-persona-select')!;
    select.value = 'p2';
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    await click('buzz-connect-persona');

    expect(buzz['createAgent']).toHaveBeenCalledWith('p2');
    expect(all('buzz-agent').length).toBe(2);
  });

  it('pauses an identity and republishes its profile', async () => {
    const { click, root } = await create();
    await click('buzz-agent-pause');
    expect(buzz['setAgentEnabled']).toHaveBeenCalledWith('a1', false);
    expect(root.querySelector('[data-testid=buzz-agent-pause]')!.textContent?.trim()).toBe('Resume');

    await click('buzz-agent-sync');
    expect(buzz['syncProfile']).toHaveBeenCalledWith('a1');
    expect(root.querySelector('[data-testid=buzz-notice]')!.textContent).toContain('published to 1 community');
  });

  describe('adding a community', () => {
    it('sends the address and, from Advanced, the owner attestation', async () => {
      const { click, type, q, all } = await create();
      await click('buzz-add-community-open');
      expect(q('buzz-attestation-help')!.textContent).toContain('never asks for your key');
      expect(q('buzz-attestation-help')!.textContent).toContain('npub1persona');
      await type('buzz-community-address', ' https://beta.communities.buzz.xyz ');
      await type('buzz-community-attestation', '["auth","owner","","sig"]');
      await click('buzz-add-community-submit');

      expect(buzz['addCommunity']).toHaveBeenCalledWith('a1', {
        relay_url: 'https://beta.communities.buzz.xyz',
        attestation: '["auth","owner","","sig"]',
      });
      expect(all('buzz-community').length).toBe(2);
      expect(q('buzz-add-community')).toBeNull();
    });

    it('shows the join terms on a 409 and joins once they are accepted for the persona', async () => {
      const { click, type, tick, q, all } = await create();
      buzz['addCommunity'].mockReturnValueOnce(throwError(() => termsConflict()));
      await click('buzz-add-community-open');
      await type('buzz-community-address', 'https://beta.communities.buzz.xyz/invite/abc');
      await click('buzz-add-community-submit');

      const terms = q('buzz-terms')!;
      expect(terms).toBeTruthy();
      expect(q('buzz-error')).toBeNull();
      expect(terms.textContent).toContain('House rules');
      expect(terms.querySelector('h1')).toBeTruthy(); // rendered, not raw Markdown
      expect(terms.textContent).toContain('I accept these terms on behalf of Vix');
      expect(q<HTMLButtonElement>('buzz-terms-submit')!.disabled).toBe(true);

      await tick('buzz-terms-accept');
      expect(q<HTMLButtonElement>('buzz-terms-submit')!.disabled).toBe(false);
      await click('buzz-terms-submit');

      expect(buzz['addCommunity']).toHaveBeenLastCalledWith('a1', {
        relay_url: 'https://beta.communities.buzz.xyz/invite/abc',
        accept_terms_version: 'v3',
        confirm_age: false,
      });
      expect(q('buzz-terms')).toBeNull();
      expect(all('buzz-community').length).toBe(2);
    });

    it('also needs the age confirmation when the community asks for it', async () => {
      const { click, type, tick, q } = await create();
      buzz['addCommunity'].mockReturnValueOnce(throwError(() => termsConflict({ ...policy, age_attestation_required: true })));
      await click('buzz-add-community-open');
      await type('buzz-community-address', 'https://beta.communities.buzz.xyz/invite/abc');
      await click('buzz-add-community-submit');

      await tick('buzz-terms-accept');
      expect(q<HTMLButtonElement>('buzz-terms-submit')!.disabled).toBe(true);
      await tick('buzz-terms-age');
      expect(q<HTMLButtonElement>('buzz-terms-submit')!.disabled).toBe(false);
      await click('buzz-terms-submit');
      expect(buzz['addCommunity']).toHaveBeenLastCalledWith('a1', {
        relay_url: 'https://beta.communities.buzz.xyz/invite/abc',
        accept_terms_version: 'v3',
        confirm_age: true,
      });
    });

    it('shows changed terms unticked when they change before they are accepted', async () => {
      const { click, type, tick, q } = await create();
      const age = { ...policy, age_attestation_required: true };
      buzz['addCommunity']
        .mockReturnValueOnce(throwError(() => termsConflict(age)))
        .mockReturnValueOnce(throwError(() => termsConflict({ ...age, terms_markdown: '# New rules', version: 'v4' })));
      await click('buzz-add-community-open');
      await type('buzz-community-address', 'https://beta.communities.buzz.xyz/invite/abc');
      await click('buzz-add-community-submit');
      await tick('buzz-terms-accept');
      await tick('buzz-terms-age');
      await click('buzz-terms-submit');

      expect(q('buzz-terms')!.textContent).toContain('New rules');
      expect(q<HTMLInputElement>('buzz-terms-accept')!.checked).toBe(false);
      expect(q<HTMLInputElement>('buzz-terms-age')!.checked).toBe(false);
      expect(q<HTMLButtonElement>('buzz-terms-submit')!.disabled).toBe(true);

      await tick('buzz-terms-accept');
      await tick('buzz-terms-age');
      await click('buzz-terms-submit');
      expect(buzz['addCommunity']).toHaveBeenLastCalledWith(
        'a1',
        expect.objectContaining({ accept_terms_version: 'v4', confirm_age: true }),
      );
    });

    it("shows the server's reason when the community refuses", async () => {
      const { click, type, q } = await create();
      buzz['addCommunity'].mockReturnValueOnce(
        throwError(
          () =>
            new HttpErrorResponse({
              status: 400,
              error: { message: 'That address is a Nostr relay, but not a Buzz community.', code: 'bad_request' },
            }),
        ),
      );
      await click('buzz-add-community-open');
      await type('buzz-community-address', 'https://nostr.example');
      await click('buzz-add-community-submit');
      expect(q('buzz-error')!.textContent).toContain('not a Buzz community');
      expect(q('buzz-add-community')).toBeTruthy();
    });
  });

  describe('a community', () => {
    it('checks again, pauses, and removes', async () => {
      const { click, all } = await create();
      await click('buzz-community-recheck');
      expect(buzz['updateCommunity']).toHaveBeenCalledWith('c1', { recheck: true });
      await click('buzz-community-pause');
      expect(buzz['updateCommunity']).toHaveBeenLastCalledWith('c1', { enabled: false });
      await click('buzz-community-remove');
      expect(buzz['removeCommunity']).toHaveBeenCalledWith('c1');
      expect(all('buzz-community').length).toBe(0);
      expect(all('buzz-binding').length).toBe(0);
    });

    it('claims an invite pasted later, through the terms when there are some', async () => {
      const { click, type, tick, q } = await create();
      buzz['updateCommunity'].mockReturnValueOnce(throwError(() => termsConflict()));
      await click('buzz-community-invite-open');
      await type('buzz-community-invite-input', 'https://acme.communities.buzz.xyz/invite/xyz');
      await click('buzz-community-invite-submit');
      expect(q('buzz-terms')!.textContent).toContain('Acme has join terms');

      await tick('buzz-terms-accept');
      await click('buzz-terms-submit');
      expect(buzz['updateCommunity']).toHaveBeenLastCalledWith('c1', {
        invite: 'https://acme.communities.buzz.xyz/invite/xyz',
        accept_terms_version: 'v3',
        confirm_age: false,
      });
      expect(q('buzz-community-invite-form')).toBeNull();
    });

    it('sets and removes the attestation', async () => {
      const { click, type, q } = await create({
        agents: [{ ...agent, communities: [{ ...community, attested: true, owner_pubkey: HEX }] }],
      });
      expect(q('buzz-community')!.textContent).toContain('Attested by npub10elfcs4…jptg');

      await click('buzz-community-attestation-open');
      await type('buzz-community-attestation-input', '["auth","o","","s"]');
      await click('buzz-community-attestation-save');
      expect(buzz['updateCommunity']).toHaveBeenLastCalledWith('c1', { attestation: '["auth","o","","s"]' });

      await click('buzz-community-attestation-open');
      await click('buzz-community-attestation-remove');
      expect(buzz['updateCommunity']).toHaveBeenLastCalledWith('c1', { attestation: '' });
    });
  });

  describe('channels', () => {
    async function openChannels() {
      const t = await create();
      await t.click('buzz-channels-open');
      return t;
    }
    const channelRow = (all: (id: string) => HTMLElement[], name: string) =>
      all('buzz-channel').find(r => r.textContent?.includes(`#${name}`))!;

    it('lists channels: connected, connectable, joinable, and private', async () => {
      const { all } = await openChannels();
      expect(buzz['listChannels']).toHaveBeenCalledWith('c1');
      expect(channelRow(all, 'general').textContent).toContain('Connected');
      expect(channelRow(all, 'dev').querySelector('[data-testid=buzz-channel-connect]')).toBeTruthy();
      expect(channelRow(all, 'lobby').querySelector('[data-testid=buzz-channel-join]')).toBeTruthy();
      expect(channelRow(all, 'secret').textContent).toContain('Ask a member to add the persona');
    });

    it('joins a public channel and lists the channels again', async () => {
      const { all, click } = await openChannels();
      await click('buzz-channel-join', channelRow(all, 'lobby'));
      expect(buzz['joinChannel']).toHaveBeenCalledWith('c1', 'ch-lobby');
      expect(buzz['listChannels']).toHaveBeenCalledTimes(2);
    });

    it('connects a channel to a new sandboxed relay thread by default', async () => {
      const { all, click, q } = await openChannels();
      await click('buzz-channel-connect', channelRow(all, 'dev'));
      expect(q('buzz-unrestricted-warning')).toBeNull();
      expect(q('buzz-connect-empty-allow-note')!.textContent).toContain('Anyone in this channel can talk to this persona.');
      await click('buzz-connect');
      expect(buzz['addBinding']).toHaveBeenCalledWith({ community_id: 'c1', channel_id: 'ch-dev', channel_name: 'dev', allow_pubkeys: [] });
      expect(all('buzz-binding').length).toBe(2);
    });

    it('needs the acknowledgement to connect an existing thread that is not sandboxed', async () => {
      const { fixture, all, click, tick, q, refresh } = await openChannels();
      await click('buzz-channel-connect', channelRow(all, 'dev'));
      const select = q<HTMLSelectElement>('buzz-thread-select')!;
      select.value = 'chat-open';
      select.dispatchEvent(new Event('change'));
      fixture.detectChanges();
      await refresh();

      expect(q('buzz-unrestricted-warning')!.textContent).toContain('Anyone allowed to mention the persona can use what this thread reads');
      expect(q<HTMLButtonElement>('buzz-connect')!.disabled).toBe(true);
      await tick('buzz-ack-unrestricted');
      expect(q<HTMLButtonElement>('buzz-connect')!.disabled).toBe(false);
      await click('buzz-connect');
      expect(buzz['addBinding']).toHaveBeenCalledWith(expect.objectContaining({ chat_id: 'chat-open', allow_unrestricted: true }));
    });
  });

  describe('a binding', () => {
    it('toggles mentions and answering other agents, which starts off', async () => {
      const { tick, q, root } = await create();
      expect(q<HTMLInputElement>('buzz-binding-agents')!.checked).toBe(false);
      expect(root.textContent).toContain('a person has to speak');
      await tick('buzz-binding-agents');
      expect(buzz['updateBinding']).toHaveBeenCalledWith('b1', { answer_agents: true });
      await tick('buzz-binding-inbound', false);
      expect(buzz['updateBinding']).toHaveBeenLastCalledWith('b1', { inbound_enabled: false });
    });

    it.each([
      ['buzz-binding-ack', false],
      ['buzz-binding-inbound', true],
      ['buzz-binding-agents', false],
    ])('puts %s back and says why when the save fails', async (id, saved) => {
      const { tick, q } = await create({ bindings: [{ ...binding, chat_sandboxed: false }] });
      buzz['updateBinding'].mockReturnValueOnce(
        throwError(() => new HttpErrorResponse({ status: 500, error: { message: 'Relay is down.' } })),
      );
      expect(q<HTMLInputElement>(id)!.checked).toBe(saved);
      await tick(id, !saved);
      expect(q<HTMLInputElement>(id)!.checked).toBe(saved);
      expect(q('buzz-error')!.textContent).toContain('Relay is down.');
    });

    it('saves who can mention the persona, as hex keys', async () => {
      const { fixture, click, q } = await create();
      const allow = fixture.nativeElement.querySelector('app-buzz-pubkey-chips input') as HTMLInputElement;
      allow.value = NPUB;
      allow.dispatchEvent(new Event('blur'));
      fixture.detectChanges();
      expect(q('buzz-empty-allow-note')).toBeNull();
      await click('buzz-binding-save-access');
      expect(buzz['updateBinding']).toHaveBeenCalledWith('b1', { allow_pubkeys: [HEX], deny_pubkeys: [] });
    });

    it('offers to try again when broken, the acknowledgement when not sandboxed, and disconnects', async () => {
      const broken: BuzzBinding = { ...binding, status: 'broken', last_error: 'restricted: not a member', chat_sandboxed: false };
      const { click, tick, q, all } = await create({ bindings: [broken] });
      buzz['updateBinding'].mockImplementation((_id: string, body: Partial<BuzzBinding>) => of({ ...broken, ...body }));
      expect(q('buzz-binding')!.textContent).toContain('restricted: not a member');
      expect(q('buzz-binding-unsandboxed')!.textContent).toContain('paused');
      await click('buzz-binding-reactivate');
      expect(buzz['updateBinding']).toHaveBeenCalledWith('b1', { reactivate: true });
      await tick('buzz-binding-ack');
      expect(buzz['updateBinding']).toHaveBeenLastCalledWith('b1', { allow_unrestricted: true });
      await click('buzz-binding-remove');
      expect(buzz['removeBinding']).toHaveBeenCalledWith('b1');
      expect(all('buzz-binding').length).toBe(0);
    });
  });

  it('removes an identity with its communities and bindings', async () => {
    const { click, all } = await create();
    await click('buzz-agent-remove');
    expect(buzz['removeAgent']).toHaveBeenCalledWith('a1');
    expect(all('buzz-agent').length).toBe(0);
  });
});

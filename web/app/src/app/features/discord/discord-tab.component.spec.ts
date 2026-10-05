import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { PersonalityService } from '../../core/services/personality.service';
import { DiscordTabComponent } from './discord-tab.component';
import { DiscordBinding, DiscordBot, DiscordService } from './discord.service';

const bot: DiscordBot = {
  id: 'bot1',
  personality_id: 'p1',
  application_id: 'app',
  bot_user_id: '111',
  bot_username: 'Vix',
  message_content: false,
  status: 'active',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '',
  invite_url: 'https://discord.com/oauth2/authorize?client_id=app',
};
const binding: DiscordBinding = {
  id: 'b1',
  bot_id: 'bot1',
  chat_id: 'chat-1',
  guild_id: 'g',
  channel_id: 'c',
  guild_name: 'Home',
  channel_name: 'general',
  inbound_enabled: true,
  allow_user_ids: [],
  deny_user_ids: [],
  allow_unrestricted: false,
  status: 'active',
  created_at: '',
  updated_at: '',
};

async function settle(): Promise<void> {
  for (let i = 0; i < 3; i++) await new Promise(r => setTimeout(r, 0));
}

describe('DiscordTabComponent', () => {
  let discord: Record<string, ReturnType<typeof vi.fn>>;

  async function create(bindings: DiscordBinding[] = [binding]) {
    discord = {
      listBots: vi.fn(() => of([bot])),
      listBindings: vi.fn(() => of(bindings)),
      addBot: vi.fn(() => of({ ...bot, id: 'bot2', personality_id: 'p2', bot_username: 'Echo' })),
      listGuilds: vi.fn(() => of([{ id: 'g', name: 'Home' }])),
      listChannels: vi.fn(() => of([{ id: 'c', name: 'general' }])),
      addBinding: vi.fn(() => of({ ...binding, id: 'b-new' })),
      updateBinding: vi.fn((_id: string, body: Partial<DiscordBinding>) => of({ ...bindings[0], ...body })),
    };
    TestBed.configureTestingModule({
      imports: [DiscordTabComponent],
      providers: [
        provideRouter([]),
        { provide: DiscordService, useValue: discord },
        { provide: ConfirmationService, useValue: { confirm: vi.fn(async () => true) } },
        {
          provide: ChatService,
          useValue: {
            listChatsPage: vi.fn(() =>
              of({
                results: [
                  { id: 'chat-open', name: 'My main thread', sandboxed: false },
                  { id: 'chat-unset', name: 'Old thread' },
                  { id: 'chat-safe', name: 'Sandbox', sandboxed: true },
                ],
                total_count: 3,
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
    const fixture = TestBed.createComponent(DiscordTabComponent);
    fixture.detectChanges();
    await settle();
    fixture.detectChanges();
    return fixture;
  }

  it('lists bots with their invite link and connected channels', async () => {
    const fixture = await create();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelectorAll('[data-testid=discord-bot]').length).toBe(1);
    expect(el.textContent).toContain('as Vix');
    expect(el.querySelector('a[href^="https://discord.com/oauth2/authorize"]')).toBeTruthy();
    expect(el.querySelector('[data-testid=discord-binding]')?.textContent).toContain('#general · Home');
  });

  it('only offers personas that have no bot yet, and adds a bot', async () => {
    const fixture = await create();
    const c = fixture.componentInstance;
    expect(c.availablePersonas().map(p => p.id)).toEqual(['p2']);

    c.newPersonaId.set('p2');
    c.newToken.set('  tok  ');
    await c.addBot();

    expect(discord['addBot']).toHaveBeenCalledWith('p2', 'tok');
    expect(c.bots().length).toBe(2);
    expect(c.newToken()).toBe('');
  });

  describe('threads that are not sandboxed', () => {
    type Fixture = Awaited<ReturnType<typeof create>>;
    async function openConnect(fixture: Fixture) {
      const c = fixture.componentInstance;
      await c.openConnect(c.bots()[0]);
      await c.pickGuild(c.bots()[0], 'g');
      c.pickChannel(c.bots()[0], 'c');
      fixture.detectChanges();
      return c;
    }
    const q = (fixture: Fixture, id: string) =>
      (fixture.nativeElement as HTMLElement).querySelector(`[data-testid=${id}]`) as HTMLElement | null;

    it('offers a new relay thread by default and needs no warning for it', async () => {
      const fixture = await create();
      const c = await openConnect(fixture);
      const form = c.connectFor('bot1')!;
      expect(form.chatId).toBe('');
      expect(q(fixture, 'discord-unrestricted-warning')).toBeNull();
      expect(c.canConnect(form)).toBe(true);
      await c.connect(c.bots()[0]);
      const sent = discord['addBinding'].mock.calls[0][0];
      expect(sent.chat_id).toBeUndefined();
      expect(sent.allow_unrestricted).toBeUndefined();
    });

    it('labels existing threads sandboxed or not', async () => {
      const fixture = await create();
      await openConnect(fixture);
      const options = Array.from(q(fixture, 'discord-thread-select')!.querySelectorAll('option')).map(o => o.textContent?.trim());
      expect(options).toContain('My main thread (not sandboxed)');
      expect(options).toContain('Old thread (not sandboxed)');
      expect(options).toContain('Sandbox (sandboxed)');
    });

    it('warns about a thread that is not sandboxed and will not connect it until the box is ticked', async () => {
      const fixture = await create();
      const c = await openConnect(fixture);
      c.pickThread(c.bots()[0], 'chat-open');
      fixture.detectChanges();

      const warning = q(fixture, 'discord-unrestricted-warning');
      expect(warning).toBeTruthy();
      expect(warning!.textContent).toContain('Anyone allowed to tag the bot can use what this thread reads');
      expect(c.canConnect(c.connectFor('bot1')!)).toBe(false);
      expect((q(fixture, 'discord-connect') as HTMLButtonElement).disabled).toBe(true);

      c.patchForm('bot1', { ackUnrestricted: true });
      fixture.detectChanges();
      expect((q(fixture, 'discord-connect') as HTMLButtonElement).disabled).toBe(false);

      await c.connect(c.bots()[0]);
      expect(discord['addBinding']).toHaveBeenCalledWith(expect.objectContaining({ chat_id: 'chat-open', allow_unrestricted: true }));
    });

    it('treats a thread with no flag as not sandboxed, and a sandboxed one as needing no box', async () => {
      const fixture = await create();
      const c = await openConnect(fixture);
      c.pickThread(c.bots()[0], 'chat-unset');
      expect(c.canConnect(c.connectFor('bot1')!)).toBe(false);

      c.pickThread(c.bots()[0], 'chat-safe');
      fixture.detectChanges();
      expect(q(fixture, 'discord-unrestricted-warning')).toBeNull();
      expect(c.canConnect(c.connectFor('bot1')!)).toBe(true);
      await c.connect(c.bots()[0]);
      const sent = discord['addBinding'].mock.calls[0][0];
      expect(sent.chat_id).toBe('chat-safe');
      expect(sent.allow_unrestricted).toBeUndefined();
    });

    it('resets the acknowledgement when another thread is chosen', async () => {
      const fixture = await create();
      const c = await openConnect(fixture);
      c.pickThread(c.bots()[0], 'chat-open');
      c.patchForm('bot1', { ackUnrestricted: true });
      c.pickThread(c.bots()[0], 'chat-unset');
      expect(c.connectFor('bot1')!.ackUnrestricted).toBe(false);
    });

    it('shows each binding as sandboxed or not and lets the owner acknowledge one', async () => {
      const paused: DiscordBinding = {
        ...binding,
        chat_sandboxed: false,
        allow_unrestricted: false,
        last_error: 'Paused: this thread is not sandboxed',
      };
      const fixture = await create([paused]);
      expect(q(fixture, 'discord-binding-unsandboxed')!.textContent).toContain('paused');
      expect(q(fixture, 'discord-binding-sandboxed')).toBeNull();

      const box = q(fixture, 'discord-binding-ack') as HTMLInputElement;
      expect(box.checked).toBe(false);
      box.checked = true;
      box.dispatchEvent(new Event('change'));
      await settle();
      expect(discord['updateBinding']).toHaveBeenCalledWith('b1', { allow_unrestricted: true });
    });

    it('marks a sandboxed binding and offers no acknowledgement for it', async () => {
      const fixture = await create([{ ...binding, chat_sandboxed: true }]);
      expect(q(fixture, 'discord-binding-sandboxed')).toBeTruthy();
      expect(q(fixture, 'discord-binding-ack')).toBeNull();
    });
  });

  describe('empty allow list', () => {
    it('says anyone in the channel can talk to the persona, on the connect form and on the binding', async () => {
      const fixture = await create();
      const el = fixture.nativeElement as HTMLElement;
      expect(el.querySelector('[data-testid=discord-binding-open-to-anyone]')).toBeTruthy();
      const details = el.querySelector('[data-testid=discord-binding] details')!;
      expect(details.textContent).toContain('Anyone in this channel can talk to this persona.');

      const c = fixture.componentInstance;
      await c.openConnect(c.bots()[0]);
      fixture.detectChanges();
      const notes = Array.from(el.querySelectorAll('[data-testid=discord-empty-allow-note]')).map(n => n.textContent ?? '');
      expect(notes.some(t => t.includes('No allow list') && t.includes('Anyone in this channel can talk to this persona.'))).toBe(true);

      c.patchForm('bot1', { allowIds: ['123456789012345678'] });
      fixture.detectChanges();
      const after = Array.from(el.querySelectorAll('[data-testid=discord-empty-allow-note]')).map(n => n.textContent ?? '');
      expect(after.some(t => t.includes('No allow list'))).toBe(false);
    });

    it('drops the note once an allow list is set', async () => {
      const fixture = await create([{ ...binding, allow_user_ids: ['123'] }]);
      const el = fixture.nativeElement as HTMLElement;
      expect(el.querySelector('[data-testid=discord-binding-open-to-anyone]')).toBeNull();
      expect(el.querySelector('[data-testid=discord-binding] details')!.textContent).not.toContain('Allow list is empty');
    });
  });
});

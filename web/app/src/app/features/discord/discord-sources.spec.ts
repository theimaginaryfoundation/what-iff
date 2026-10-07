import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of, throwError } from 'rxjs';

import { DISCORD_TAB, DiscordIntegrationTabSource, DiscordThreadAutomationSource, bindingAutomation } from './discord-sources';
import { DiscordBinding, DiscordService, bindingLabel } from './discord.service';

function binding(overrides: Partial<DiscordBinding> = {}): DiscordBinding {
  return {
    id: 'b1',
    bot_id: 'bot',
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
    ...overrides,
  };
}

describe('Discord sources', () => {
  function setup(available: boolean, listBindings = vi.fn(() => of([binding()]))) {
    TestBed.configureTestingModule({
      providers: [
        DiscordIntegrationTabSource,
        DiscordThreadAutomationSource,
        { provide: DiscordService, useValue: { available: () => of(available), listBindings } },
      ],
    });
    return listBindings;
  }

  it('adds the Discord tab when the server runs the relay', async () => {
    setup(true);
    expect(await firstValueFrom(TestBed.inject(DiscordIntegrationTabSource).tabs())).toEqual([DISCORD_TAB]);
    expect(DISCORD_TAB.label).toBe('Discord');
  });

  it('adds no tab when the relay is switched off', async () => {
    setup(false);
    expect(await firstValueFrom(TestBed.inject(DiscordIntegrationTabSource).tabs())).toEqual([]);
  });

  it('lists relay threads as automations, and asks nothing when the relay is off', async () => {
    const list = setup(true);
    const items = await firstValueFrom(TestBed.inject(DiscordThreadAutomationSource).list());
    expect(items.map(i => i.chatId)).toEqual(['chat-1']);
    expect(list).toHaveBeenCalledTimes(1);

    TestBed.resetTestingModule();
    const none = setup(false);
    expect(await firstValueFrom(TestBed.inject(DiscordThreadAutomationSource).list())).toEqual([]);
    expect(none).not.toHaveBeenCalled();
  });

  it('treats a failing binding list as no automations', async () => {
    setup(
      true,
      vi.fn(() => throwError(() => new Error('down'))),
    );
    expect(await firstValueFrom(TestBed.inject(DiscordThreadAutomationSource).list())).toEqual([]);
  });
});

describe('bindingAutomation', () => {
  it('reads live, post only and broken', () => {
    expect(bindingAutomation(binding())).toEqual(
      expect.objectContaining({ name: 'Discord · #general · Home', statusText: 'Live', tone: 'success' }),
    );
    expect(bindingAutomation(binding({ inbound_enabled: false }))).toEqual(
      expect.objectContaining({ statusText: 'Post only', tone: 'neutral' }),
    );
    expect(bindingAutomation(binding({ status: 'broken', last_error: 'kicked' }))).toEqual(
      expect.objectContaining({ statusText: 'Broken', tone: 'danger', statusHint: 'kicked' }),
    );
  });
});

describe('discord helpers', () => {
  it('labels bindings', () => {
    expect(bindingLabel({ channel_name: 'general', channel_id: 'c', guild_name: 'Home' })).toBe('#general · Home');
    expect(bindingLabel({ channel_name: '', channel_id: '123', guild_name: '' })).toBe('#123');
  });
});

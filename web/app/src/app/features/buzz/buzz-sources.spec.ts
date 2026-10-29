import { TestBed } from '@angular/core/testing';
import { Observable, firstValueFrom, of, throwError } from 'rxjs';

import { CombinedIntegrationTabs, IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { CombinedThreadAutomations, ThreadAutomation, ThreadAutomationSource } from '../../core/services/thread-automation-source';
import { integrationTabProviders } from '../../extensions/integration-tabs.providers';
import { threadAutomationProviders } from '../../extensions/thread-automations.providers';
import { DISCORD_TAB } from '../discord/discord-sources';
import { DiscordService } from '../discord/discord.service';
import { SlackService } from '../slack/slack.service';
import { BUZZ_TAB, BuzzIntegrationTabSource, BuzzThreadAutomationSource, buzzBindingAutomation } from './buzz-sources';
import { BuzzBinding, BuzzService } from './buzz.service';

function binding(overrides: Partial<BuzzBinding> = {}): BuzzBinding {
  return {
    id: 'b1',
    community_id: 'c1',
    community_name: 'Acme',
    chat_id: 'chat-1',
    channel_id: 'ch',
    channel_name: 'general',
    inbound_enabled: true,
    allow_pubkeys: [],
    deny_pubkeys: [],
    answer_agents: false,
    allow_unrestricted: false,
    status: 'active',
    created_at: '',
    updated_at: '',
    ...overrides,
  };
}

describe('Buzz sources', () => {
  function setup(available: boolean, listBindings = vi.fn(() => of([binding()]))) {
    TestBed.configureTestingModule({
      providers: [
        BuzzIntegrationTabSource,
        BuzzThreadAutomationSource,
        { provide: BuzzService, useValue: { available: () => of(available), listBindings } },
      ],
    });
    return listBindings;
  }

  it('adds the Buzz tab when the server runs the relay', async () => {
    setup(true);
    expect(await firstValueFrom(TestBed.inject(BuzzIntegrationTabSource).tabs())).toEqual([BUZZ_TAB]);
    expect(BUZZ_TAB.id).toBe('buzz');
  });

  it('adds no tab when the relay is switched off', async () => {
    setup(false);
    expect(await firstValueFrom(TestBed.inject(BuzzIntegrationTabSource).tabs())).toEqual([]);
  });

  it('lists relay threads as automations, and asks nothing when the relay is off', async () => {
    const list = setup(true);
    const items = await firstValueFrom(TestBed.inject(BuzzThreadAutomationSource).list());
    expect(items.map(i => i.chatId)).toEqual(['chat-1']);
    expect(list).toHaveBeenCalledTimes(1);

    TestBed.resetTestingModule();
    const none = setup(false);
    expect(await firstValueFrom(TestBed.inject(BuzzThreadAutomationSource).list())).toEqual([]);
    expect(none).not.toHaveBeenCalled();
  });

  it('treats a failing binding list as no automations', async () => {
    setup(
      true,
      vi.fn(() => throwError(() => new Error('down'))),
    );
    expect(await firstValueFrom(TestBed.inject(BuzzThreadAutomationSource).list())).toEqual([]);
  });
});

describe('buzzBindingAutomation', () => {
  it('reads live, post only and broken', () => {
    expect(buzzBindingAutomation(binding())).toEqual(
      expect.objectContaining({ name: 'Buzz · #general · Acme', statusText: 'Live', tone: 'success', link: ['/integrations'] }),
    );
    expect(buzzBindingAutomation(binding({ inbound_enabled: false }))).toEqual(
      expect.objectContaining({ statusText: 'Post only', tone: 'neutral' }),
    );
    expect(buzzBindingAutomation(binding({ status: 'broken', last_error: 'removed from channel' }))).toEqual(
      expect.objectContaining({ statusText: 'Broken', tone: 'danger', statusHint: 'removed from channel' }),
    );
  });
});

describe('combined sources', () => {
  const tabs = (list: IntegrationTab[] | Error): IntegrationTabSource =>
    ({ tabs: () => (list instanceof Error ? throwError(() => list) : of(list)) as Observable<IntegrationTab[]> }) as IntegrationTabSource;
  const automations = (list: ThreadAutomation[] | Error): ThreadAutomationSource =>
    ({
      list: () => (list instanceof Error ? throwError(() => list) : of(list)) as Observable<ThreadAutomation[]>,
    }) as ThreadAutomationSource;

  it('joins tabs in source order, and a failing source adds none', async () => {
    expect(await firstValueFrom(new CombinedIntegrationTabs([tabs([DISCORD_TAB]), tabs(new Error('x')), tabs([BUZZ_TAB])]).tabs())).toEqual(
      [DISCORD_TAB, BUZZ_TAB],
    );
    expect(await firstValueFrom(new CombinedIntegrationTabs([]).tabs())).toEqual([]);
  });

  it('joins automations, and a failing source adds none', async () => {
    const a = buzzBindingAutomation(binding());
    expect(await firstValueFrom(new CombinedThreadAutomations([automations(new Error('x')), automations([a])]).list())).toEqual([a]);
    expect(await firstValueFrom(new CombinedThreadAutomations([]).list())).toEqual([]);
  });

  it('provides Discord, Buzz and Slack from the extension files', async () => {
    TestBed.configureTestingModule({
      providers: [
        ...integrationTabProviders,
        ...threadAutomationProviders,
        {
          provide: DiscordService,
          useValue: { available: () => of(true), listBindings: () => of([{ ...binding(), chat_id: 'discord-chat', guild_name: 'Home' }]) },
        },
        { provide: BuzzService, useValue: { available: () => of(true), listBindings: () => of([binding()]) } },
        {
          provide: SlackService,
          useValue: { available: () => of(true), listBindings: () => of([{ ...binding(), chat_id: 'slack-chat', team_name: 'Acme' }]) },
        },
      ],
    });
    const shown = await firstValueFrom(TestBed.inject(IntegrationTabSource).tabs());
    expect(shown.map(t => t.id)).toEqual(['discord', 'buzz', 'slack']);
    const listed = await firstValueFrom(TestBed.inject(ThreadAutomationSource).list());
    expect(listed.map(t => t.chatId)).toEqual(['discord-chat', 'chat-1', 'slack-chat']);
  });
});

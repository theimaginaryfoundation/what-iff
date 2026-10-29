import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of, throwError } from 'rxjs';

import { SLACK_TAB, SlackIntegrationTabSource, SlackThreadAutomationSource, slackBindingAutomation } from './slack-sources';
import { SlackBinding, SlackService } from './slack.service';

function binding(overrides: Partial<SlackBinding> = {}): SlackBinding {
  return {
    id: 'b1',
    app_id: 'a1',
    team_name: 'Acme',
    chat_id: 'chat-1',
    channel_id: 'C1',
    channel_name: 'general',
    is_private: false,
    inbound_enabled: true,
    allow_user_ids: [],
    deny_user_ids: [],
    answer_agents: false,
    allow_unrestricted: false,
    status: 'active',
    created_at: '',
    updated_at: '',
    ...overrides,
  };
}

describe('Slack sources', () => {
  function setup(available: boolean, listBindings = vi.fn(() => of([binding()]))) {
    TestBed.configureTestingModule({
      providers: [
        SlackIntegrationTabSource,
        SlackThreadAutomationSource,
        { provide: SlackService, useValue: { available: () => of(available), listBindings } },
      ],
    });
    return listBindings;
  }

  it('adds the Slack tab when the relay is on for the user', async () => {
    setup(true);
    expect(await firstValueFrom(TestBed.inject(SlackIntegrationTabSource).tabs())).toEqual([SLACK_TAB]);
    expect(SLACK_TAB.id).toBe('slack');
    expect(SLACK_TAB.label).toBe('Slack');
  });

  it('adds no tab when the relay is off (the API answered 404)', async () => {
    setup(false);
    expect(await firstValueFrom(TestBed.inject(SlackIntegrationTabSource).tabs())).toEqual([]);
  });

  it('lists relay threads as automations, and asks nothing when the relay is off', async () => {
    const list = setup(true);
    const items = await firstValueFrom(TestBed.inject(SlackThreadAutomationSource).list());
    expect(items.map(i => i.chatId)).toEqual(['chat-1']);
    expect(list).toHaveBeenCalledTimes(1);

    TestBed.resetTestingModule();
    const none = setup(false);
    expect(await firstValueFrom(TestBed.inject(SlackThreadAutomationSource).list())).toEqual([]);
    expect(none).not.toHaveBeenCalled();
  });

  it('treats a failing binding list as no automations', async () => {
    setup(
      true,
      vi.fn(() => throwError(() => new Error('down'))),
    );
    expect(await firstValueFrom(TestBed.inject(SlackThreadAutomationSource).list())).toEqual([]);
  });
});

describe('slackBindingAutomation', () => {
  it('reads live, post only and broken', () => {
    expect(slackBindingAutomation(binding())).toEqual(
      expect.objectContaining({ name: 'Slack · #general · Acme', statusText: 'Live', tone: 'success', link: ['/integrations'] }),
    );
    expect(slackBindingAutomation(binding({ inbound_enabled: false }))).toEqual(
      expect.objectContaining({ statusText: 'Post only', tone: 'neutral' }),
    );
    expect(slackBindingAutomation(binding({ status: 'broken', last_error: 'not_in_channel' }))).toEqual(
      expect.objectContaining({ statusText: 'Broken', tone: 'danger', statusHint: 'not_in_channel' }),
    );
  });
});

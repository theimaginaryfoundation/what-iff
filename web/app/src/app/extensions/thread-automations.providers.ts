import { Provider } from '@angular/core';

import { CombinedThreadAutomations, ThreadAutomationSource } from '../core/services/thread-automation-source';
import { BuzzThreadAutomationSource } from '../features/buzz/buzz-sources';
import { DiscordThreadAutomationSource } from '../features/discord/discord-sources';
import { SlackThreadAutomationSource } from '../features/slack/slack-sources';

/**
 * DI providers for thread automations shown in the Jobs tab (swap-point file).
 *
 * This build lists Discord, Buzz and Slack relay threads next to agent-job threads, combined into
 * the one ThreadAutomationSource the Thread Manager reads. Another build may replace this file to
 * bind a source that adds its own; it should keep these three, by adding its source to the
 * combined list. `app.config.ts` spreads these into the application config.
 */
export const threadAutomationProviders: Provider[] = [
  DiscordThreadAutomationSource,
  BuzzThreadAutomationSource,
  SlackThreadAutomationSource,
  {
    provide: ThreadAutomationSource,
    useFactory: (discord: DiscordThreadAutomationSource, buzz: BuzzThreadAutomationSource, slack: SlackThreadAutomationSource) =>
      new CombinedThreadAutomations([discord, buzz, slack]),
    deps: [DiscordThreadAutomationSource, BuzzThreadAutomationSource, SlackThreadAutomationSource],
  },
];

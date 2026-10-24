import { Provider } from '@angular/core';

import { CombinedThreadAutomations, ThreadAutomationSource } from '../core/services/thread-automation-source';
import { BuzzThreadAutomationSource } from '../features/buzz/buzz-sources';
import { DiscordThreadAutomationSource } from '../features/discord/discord-sources';

/**
 * DI providers for thread automations shown in the Jobs tab (swap-point file).
 *
 * This build lists Discord and Buzz relay threads next to agent-job threads, combined into the
 * one ThreadAutomationSource the Thread Manager reads. Another build may replace this file to
 * bind a source that adds its own; it should keep these two, by adding its source to the
 * combined list. `app.config.ts` spreads these into the application config.
 */
export const threadAutomationProviders: Provider[] = [
  DiscordThreadAutomationSource,
  BuzzThreadAutomationSource,
  {
    provide: ThreadAutomationSource,
    useFactory: (discord: DiscordThreadAutomationSource, buzz: BuzzThreadAutomationSource) =>
      new CombinedThreadAutomations([discord, buzz]),
    deps: [DiscordThreadAutomationSource, BuzzThreadAutomationSource],
  },
];

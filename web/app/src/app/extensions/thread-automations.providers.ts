import { Provider } from '@angular/core';

import { ThreadAutomationSource } from '../core/services/thread-automation-source';
import { DiscordThreadAutomationSource } from '../features/discord/discord-sources';

/**
 * DI providers for thread automations shown in the Jobs tab (swap-point file).
 *
 * This build lists Discord relay threads next to agent-job threads. Another
 * build may replace this file to bind a source that adds its own; it should keep
 * the Discord relay threads. `app.config.ts` spreads these into the application
 * config.
 */
export const threadAutomationProviders: Provider[] = [{ provide: ThreadAutomationSource, useClass: DiscordThreadAutomationSource }];

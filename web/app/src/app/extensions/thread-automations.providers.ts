import { Provider } from '@angular/core';

import { NoThreadAutomations, ThreadAutomationSource } from '../core/services/thread-automation-source';

/**
 * DI providers for thread automations shown in the Jobs tab (swap-point file).
 *
 * This build lists none, so the Jobs tab shows agent-job threads only. Another
 * build replaces this file to bind a source that adds its own. `app.config.ts`
 * spreads these into the application config.
 */
export const threadAutomationProviders: Provider[] = [
  { provide: ThreadAutomationSource, useClass: NoThreadAutomations },
];

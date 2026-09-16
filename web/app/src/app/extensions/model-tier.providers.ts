import { Provider } from '@angular/core';

import { HiddenModelTierDisplay, ModelTierDisplay } from '../core/services/model-tier-display';

/**
 * DI providers for model tier display (swap-point file).
 *
 * This build hides subscription tiers: they describe what a hosted plan
 * includes, which is meaningless when the user supplies their own provider key.
 * Another build replaces this file to bind an implementation that shows them.
 * `app.config.ts` spreads these into the application config.
 */
export const modelTierProviders: Provider[] = [{ provide: ModelTierDisplay, useClass: HiddenModelTierDisplay }];

import { Injectable } from '@angular/core';

/**
 * Extension point deciding whether model subscription tiers are shown.
 *
 * Tiers describe a hosted plan's entitlement — which models a subscription tier
 * includes — so they mean nothing where someone brings their own provider key
 * and pays the provider directly. The default hides them; a build with a
 * subscription behind it supplies an implementation that shows them. The
 * concrete implementation is supplied via DI (see
 * extensions/model-tier.providers.ts).
 */
@Injectable({ providedIn: 'root', useFactory: () => new HiddenModelTierDisplay() })
export abstract class ModelTierDisplay {
  /** True when tier grouping and tier badges should be offered. */
  abstract enabled(): boolean;
}

/**
 * Default: tiers are not shown. Model providers still group the picker, which
 * is the distinction that means something without a subscription.
 *
 * Bound at the root above as well as in extensions/model-tier.providers.ts. The
 * swap-point file is what another build replaces; the root default exists so
 * anything rendering the model picker outside the application config — every
 * component test that reaches it, directly or through chat — does not have to
 * know this seam exists.
 */
@Injectable()
export class HiddenModelTierDisplay extends ModelTierDisplay {
  enabled(): boolean {
    return false;
  }
}

/**
 * Shows tiers. Ships here rather than in the build that uses it, because the
 * implementation is a constant — there is no logic to keep private, and asking
 * the other build to write its own leaves the swap-point file as the only place
 * the two answers can disagree.
 *
 * Nothing in this build binds it. It exists so binding it is one line, and so
 * both answers are exercised by the tests in this repo rather than only one.
 */
@Injectable()
export class ShownModelTierDisplay extends ModelTierDisplay {
  enabled(): boolean {
    return true;
  }
}

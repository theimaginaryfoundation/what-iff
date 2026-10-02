import { Injectable, Type } from '@angular/core';
import { Observable, of } from 'rxjs';

/** One extra tab on the Integrations page, contributed by another build. */
export interface IntegrationTab {
  /** Stable id, used as the active-tab value. Must not be 'connectors' or 'webhooks'. */
  id: string;
  label: string;
  /** Tooltip on the tab button. */
  tooltip?: string;
  /** Standalone component rendered as the tab's content. */
  component: Type<unknown>;
}

/**
 * Extension point for extra Integrations tabs. The default source contributes
 * none; another build binds its own (see extensions/integration-tabs.providers.ts),
 * and can emit a different list as the user's entitlements load.
 */
@Injectable({ providedIn: 'root', useFactory: () => new NoIntegrationTabs() })
export abstract class IntegrationTabSource {
  abstract tabs(): Observable<IntegrationTab[]>;
}

/** Default source: no extra tabs. */
@Injectable()
export class NoIntegrationTabs extends IntegrationTabSource {
  tabs(): Observable<IntegrationTab[]> {
    return of([]);
  }
}

import { Injectable, Type } from '@angular/core';
import { Observable, catchError, combineLatest, map, of } from 'rxjs';

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

/**
 * Several sources as one, so a build can contribute tabs from more than one feature (see
 * extensions/integration-tabs.providers.ts). Tabs come in the order of the sources; a source that
 * fails contributes none rather than hiding the others.
 */
export class CombinedIntegrationTabs extends IntegrationTabSource {
  constructor(private readonly sources: readonly IntegrationTabSource[]) {
    super();
  }

  tabs(): Observable<IntegrationTab[]> {
    if (this.sources.length === 0) return of([]);
    return combineLatest(this.sources.map(s => s.tabs().pipe(catchError(() => of([] as IntegrationTab[]))))).pipe(
      map(lists => lists.flat()),
    );
  }
}

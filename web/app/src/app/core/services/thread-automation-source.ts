import { Injectable } from '@angular/core';
import { Observable, catchError, combineLatest, map, of } from 'rxjs';

/** Same tones as the agent-job status pill, so both read alike in the Jobs tab. */
export type ThreadAutomationTone = 'success' | 'warning' | 'danger' | 'neutral';

/**
 * Something other than a scheduled job that drives a thread (a relay to another
 * chat service, say), listed in the Thread Manager's Jobs tab next to agent jobs.
 */
export interface ThreadAutomation {
  /** The thread it drives. */
  chatId: string;
  /** Short status word for the pill, e.g. "Live" or "Broken". */
  statusText: string;
  tone: ThreadAutomationTone;
  /** Tooltip on the status pill. */
  statusHint?: string;
  /** Shown as a link, e.g. "Relay · #general". */
  name: string;
  /** Router commands for the link (where the automation is managed). */
  link: readonly string[];
  /** Optional second line, e.g. "Last post 5m ago". */
  when?: string;
}

/**
 * Extension point for threads driven by something other than an agent job. The
 * default source lists none; another build binds its own (see
 * extensions/thread-automations.providers.ts).
 */
@Injectable({ providedIn: 'root', useFactory: () => new NoThreadAutomations() })
export abstract class ThreadAutomationSource {
  abstract list(): Observable<ThreadAutomation[]>;
}

/** Default source: no automations. */
@Injectable()
export class NoThreadAutomations extends ThreadAutomationSource {
  list(): Observable<ThreadAutomation[]> {
    return of([]);
  }
}

/**
 * Several sources as one, so a build can list automations from more than one feature (see
 * extensions/thread-automations.providers.ts). A source that fails contributes none rather than
 * hiding the others.
 */
export class CombinedThreadAutomations extends ThreadAutomationSource {
  constructor(private readonly sources: readonly ThreadAutomationSource[]) {
    super();
  }

  list(): Observable<ThreadAutomation[]> {
    if (this.sources.length === 0) return of([]);
    return combineLatest(this.sources.map(s => s.list().pipe(catchError(() => of([] as ThreadAutomation[]))))).pipe(
      map(lists => lists.flat()),
    );
  }
}

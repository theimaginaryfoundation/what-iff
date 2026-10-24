import { Injectable, inject } from '@angular/core';
import { Observable, map, of, tap } from 'rxjs';

import { PersonalityService } from './personality.service';

/**
 * Remembers that the user has at least one personality, so the setup guard can let a
 * navigation through without asking the server again.
 *
 * The guard runs on every navigation inside the app shell. Before this cache it sent
 * `GET /personality?limit=1` each time, and the router holds the old page until a guard
 * settles — on a slow phone connection (or with the browser's per-host connection pool
 * busy with streams) a nav tap looked like it did nothing (#348).
 *
 * Only a positive answer is cached: a user with no personality yet is re-checked on every
 * navigation, so the redirect stops as soon as they create one. Any delete or
 * invalidation drops the answer, since it may have removed the last personality.
 */
@Injectable({ providedIn: 'root' })
export class PersonalityPresenceService {
  private readonly personalityService = inject(PersonalityService);
  private confirmed = false;

  constructor() {
    this.personalityService.changes$.subscribe(change => {
      if (change.kind === 'created') {
        this.confirmed = true;
      } else if (change.kind === 'deleted' || change.kind === 'invalidated') {
        this.confirmed = false;
      }
    });
  }

  /** Whether the user has any personality. Answers from cache once one is known to exist. */
  hasPersonality(): Observable<boolean> {
    if (this.confirmed) {
      return of(true);
    }
    return this.personalityService.listPersonalities(1, 1).pipe(
      map(response => (response.total_count ?? response.results?.length ?? 0) > 0),
      tap(has => {
        this.confirmed = has;
      }),
    );
  }

  /** Forgets the cached answer. Called on logout so the next user is checked afresh. */
  clearCache(): void {
    this.confirmed = false;
  }
}

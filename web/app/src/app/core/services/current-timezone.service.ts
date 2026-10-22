import { DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';

import { environment } from '../../../environments/environment';
import { UserResponse } from '../models/user.model';
import { detectBrowserTimezone } from '../utils/timezone.helpers';

/**
 * Tells the API which timezone the browser is in, so jobs set to follow the user run at the same
 * local time on a trip. Best effort: it reports when a user signs in and whenever the tab becomes
 * visible again, and only when the zone differs from the one the API last heard.
 */
@Injectable({ providedIn: 'root' })
export class CurrentTimezoneService {
  private readonly http = inject(HttpClient);
  private readonly document = inject(DOCUMENT);
  private readonly url = `${environment.apiUrl}/user/current-timezone`;

  private active = false;
  private listening = false;
  /** The zone the API has (or is being sent); null when unknown. */
  private known: string | null = null;

  /** Starts reporting for a signed-in user, whose profile carries the zone the API has now. */
  start(user: UserResponse): void {
    this.active = true;
    this.known = (user.current_timezone ?? '').trim() || null;
    this.listen();
    this.report();
  }

  /** Stops reporting (sign-out). */
  stop(): void {
    this.active = false;
    this.known = null;
  }

  private listen(): void {
    if (this.listening) return;
    this.listening = true;
    this.document.addEventListener('visibilitychange', () => {
      if (this.document.visibilityState === 'visible') this.report();
    });
  }

  private report(): void {
    if (!this.active) return;
    const zone = detectBrowserTimezone();
    if (!zone || zone === this.known) return;
    this.known = zone;
    this.http.put<void>(this.url, { timezone: zone }).subscribe({
      // Forget it so the next visibility change tries again.
      error: () => {
        if (this.known === zone) this.known = null;
      },
    });
  }
}

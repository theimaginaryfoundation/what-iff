import { DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Injectable, computed, inject, signal } from '@angular/core';

import { environment } from '../../../environments/environment';
import { CurrentTimezoneReport, UserResponse } from '../models/user.model';
import { detectBrowserTimezone } from '../utils/timezone.helpers';

/**
 * Tells the API which timezone the browser is in, so jobs set to follow the user run at the same
 * local time on a trip (ADR 0x027). Best effort: it reports when a user signs in and whenever the
 * tab becomes visible again, and only when the zone differs from the one the API last heard.
 *
 * When the user's devices keep reporting different zones, the API pauses following and says so;
 * `followingPausedAt` then holds when, until the user resumes.
 */
@Injectable({ providedIn: 'root' })
export class CurrentTimezoneService {
  private readonly http = inject(HttpClient);
  private readonly document = inject(DOCUMENT);
  private readonly url = `${environment.apiUrl}/user/current-timezone`;

  private active = false;
  /** The zone the API has (or is being sent); null when unknown. */
  private known: string | null = null;
  private readonly onVisibilityChange = () => {
    if (this.document.visibilityState === 'visible') this.report();
  };

  private readonly pausedAt = signal<string | null>(null);
  /** When following the user's timezone was paused (ISO time), or null while it is on. */
  readonly followingPausedAt = this.pausedAt.asReadonly();
  readonly followingPaused = computed(() => this.pausedAt() !== null);

  private readonly resumingState = signal(false);
  private readonly resumeErrorState = signal<string | null>(null);
  /** True while a resume request is in flight. */
  readonly resuming = this.resumingState.asReadonly();
  /** Why the last resume failed, for whichever view started it. */
  readonly resumeError = this.resumeErrorState.asReadonly();

  /** Starts reporting for a signed-in user, whose profile carries what the API has now. */
  start(user: UserResponse): void {
    this.known = (user.current_timezone ?? '').trim() || null;
    this.pausedAt.set(user.timezone_following_paused_at ?? null);
    if (!this.active) {
      this.active = true;
      this.document.addEventListener('visibilitychange', this.onVisibilityChange);
    }
    this.report();
  }

  /** Stops reporting (sign-out). */
  stop(): void {
    if (this.active) {
      this.document.removeEventListener('visibilitychange', this.onVisibilityChange);
    }
    this.active = false;
    this.known = null;
    this.pausedAt.set(null);
  }

  /** Ends a pause, then reports this browser's zone straight away. */
  resume(): void {
    if (this.resumingState()) return;
    this.resumingState.set(true);
    this.resumeErrorState.set(null);
    this.http.post<void>(`${this.url}/resume`, {}).subscribe({
      next: () => {
        this.resumingState.set(false);
        this.pausedAt.set(null);
        this.known = null;
        this.report();
      },
      error: () => {
        this.resumingState.set(false);
        this.resumeErrorState.set("Couldn't resume following. Try again.");
      },
    });
  }

  private report(): void {
    if (!this.active || this.pausedAt()) return;
    const zone = detectBrowserTimezone();
    if (!zone || zone === this.known) return;
    this.known = zone;
    this.http.put<CurrentTimezoneReport>(this.url, { timezone: zone }).subscribe({
      next: report => {
        if (report?.timezone_following_paused_at) this.pausedAt.set(report.timezone_following_paused_at);
      },
      // Best effort, no backoff on purpose: forget the zone so the next visibility change sends
      // it again rather than assuming the API has it.
      error: () => {
        if (this.known === zone) this.known = null;
      },
    });
  }
}

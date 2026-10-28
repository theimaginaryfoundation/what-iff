import { DOCUMENT } from '@angular/common';
import { Injectable, Injector, effect, inject, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { AuthService } from '../../core/services/auth.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { RemoteSyncService } from './remote-sync.service';

/**
 * Mullet mode's "sync on wake" (ADR 0x033): instead of polling the remote on a clock, the app
 * tells its own server when it is being used, and the server syncs if the copy is stale.
 *
 * A wake is sent when a signed-in session starts, and when the tab comes back after being hidden
 * for a while. When the wake starts a run, this follows it and refreshes the thread list if
 * anything new came over, so the threads someone left on their phone are just there.
 *
 * A persona created or imported here sends a wake too: copied threads written with a persona of
 * that name on the remote take it straight away.
 *
 * Nothing is sent on a server that does not run remote sync (its route answers 404 once and the
 * answer is cached for the session), and every failure is swallowed: a wake is a hint.
 */
@Injectable({ providedIn: 'root' })
export class RemoteSyncWakeService {
  /** Back after at least this long hidden counts as waking up. */
  static readonly IDLE_MS = 5 * 60_000;
  /** How long to follow a run the wake started (polls × interval). */
  static readonly MAX_POLLS = 60;

  private readonly api = inject(RemoteSyncService);
  private readonly auth = inject(AuthService);
  private readonly threadList = inject(ThreadListService);
  private readonly personalities = inject(PersonalityService);
  private readonly document = inject(DOCUMENT);
  private readonly injector = inject(Injector);

  /** How often to re-read the status while a run is going (tests shorten it). */
  pollMs = 3000;

  private started = false;
  private inFlight = false;
  private hiddenAt: number | null = null;

  /** Starts listening. Idempotent; called once from the app initializer. */
  start(): void {
    if (this.started) return;
    this.started = true;
    effect(
      () => {
        if (this.auth.isLoggedIn()) untracked(() => void this.wake());
      },
      { injector: this.injector },
    );
    this.document.addEventListener('visibilitychange', this.onVisibilityChange);
    // Lives as long as the app (root service started once), so it is never unsubscribed.
    this.personalities.changes$.subscribe(change => {
      if (change.kind === 'created' || change.kind === 'invalidated') void this.wake();
    });
  }

  private readonly onVisibilityChange = (): void => {
    if (this.document.visibilityState === 'hidden') {
      this.hiddenAt = Date.now();
      return;
    }
    const away = this.hiddenAt === null ? 0 : Date.now() - this.hiddenAt;
    this.hiddenAt = null;
    if (away >= RemoteSyncWakeService.IDLE_MS && this.auth.isLoggedIn()) void this.wake();
  };

  /** Sends one wake and follows the run it starts. Never throws. */
  async wake(): Promise<void> {
    if (this.inFlight) return;
    this.inFlight = true;
    try {
      if (!(await firstValueFrom(this.api.available()))) return;
      const { started, rematched } = await firstValueFrom(this.api.wake());
      if (!started) {
        if (rematched > 0) await this.threadList.refresh();
        return;
      }
      for (let i = 0; i < RemoteSyncWakeService.MAX_POLLS; i++) {
        await new Promise(resolve => setTimeout(resolve, this.pollMs));
        const s = await firstValueFrom(this.api.get());
        if (s.running) continue;
        if (rematched > 0 || (s.last_run?.messages_added ?? 0) > 0) await this.threadList.refresh();
        return;
      }
    } catch {
      // A wake is a hint; the Remote sync tab shows any error that matters.
    } finally {
      this.inFlight = false;
    }
  }
}

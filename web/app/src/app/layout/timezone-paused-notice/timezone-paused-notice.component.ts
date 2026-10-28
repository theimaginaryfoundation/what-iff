import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { CurrentTimezoneService } from '../../core/services/current-timezone.service';
import { ButtonComponent } from '../../shared/ui/button/button.component';

const DISMISSED_KEY = 'timezoneFollowingPausedNoticeDismissed';

/**
 * Tells the user that following their timezone was paused because their devices kept reporting
 * different zones (ADR 0x027), with a way to resume. Dismissing hides it for that pause only.
 */
@Component({
  selector: 'app-timezone-paused-notice',
  standalone: true,
  imports: [ButtonComponent],
  template: `
    @if (visible()) {
      <aside class="timezone-paused-notice" role="status" aria-live="polite" data-testid="timezone-paused-notice">
        <p class="timezone-paused-notice__title">Jobs stopped following your timezone</p>
        <p class="timezone-paused-notice__body">
          Your devices kept reporting different timezones, so jobs set to follow you are back on their own timezones. Resume once only the
          device you're travelling with is in use.
        </p>
        @if (error()) {
          <p class="timezone-paused-notice__error">{{ error() }}</p>
        }
        <div class="timezone-paused-notice__actions">
          <ui-button size="sm" variant="secondary" (activate)="dismiss()">Dismiss</ui-button>
          <ui-button size="sm" variant="primary" [loading]="resuming()" (activate)="resume()" data-testid="timezone-resume">
            Resume following
          </ui-button>
        </div>
      </aside>
    }
  `,
  styleUrl: './timezone-paused-notice.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TimezonePausedNoticeComponent {
  private readonly timezone = inject(CurrentTimezoneService);
  private readonly dismissedFor = signal<string | null>(readDismissed());
  readonly resuming = this.timezone.resuming;
  readonly error = this.timezone.resumeError;

  readonly visible = computed(() => {
    const pausedAt = this.timezone.followingPausedAt();
    return !!pausedAt && pausedAt !== this.dismissedFor();
  });

  dismiss(): void {
    const pausedAt = this.timezone.followingPausedAt();
    this.dismissedFor.set(pausedAt);
    try {
      if (pausedAt) localStorage.setItem(DISMISSED_KEY, pausedAt);
    } catch {
      // Storage unavailable: it stays dismissed for this visit.
    }
  }

  resume(): void {
    this.timezone.resume();
  }
}

function readDismissed(): string | null {
  try {
    return localStorage.getItem(DISMISSED_KEY);
  } catch {
    return null;
  }
}

import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
} from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';

import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { ActivePersonalityMediaJob } from '../../../core/models/personality-media-job.model';

const TERMINAL = new Set(['complete', 'failed']);

export interface MediaJobBannerCopy {
  title: string;
  hint: string;
}

export function mediaJobBannerCopy(
  job: ActivePersonalityMediaJob | null | undefined,
  contextPersonalityId?: string | null,
): MediaJobBannerCopy | null {
  if (!job || TERMINAL.has(job.status)) {
    return null;
  }

  const hint =
    'This usually takes about a minute. You can leave this page—the job will keep running in the background.';

  if (job.job_type === 'personality_portrait') {
    return {
      title: 'Generating personality portrait…',
      hint,
    };
  }

  const name = job.personality_name?.trim() || 'this personality';
  if (
    contextPersonalityId &&
    job.personality_id &&
    job.personality_id !== contextPersonalityId
  ) {
    return {
      title: `Generating expressions for ${name}`,
      hint: `${hint} Only one image job can run at a time.`,
    };
  }

  return {
    title: `Generating default expressions for ${name}…`,
    hint,
  };
}

@Component({
  selector: 'app-personality-media-job-banner',
  standalone: true,
  template: `
    @if (copy(); as banner) {
      <aside
        class="rounded-xl border border-(--color-border-default) bg-[color-mix(in_srgb,var(--color-accent)_10%,var(--color-surface-card))] px-4 py-3"
        role="status"
        aria-live="polite"
      >
        <p class="text-sm font-semibold text-(--color-text-primary)">{{ banner.title }}</p>
        <p class="mt-1 text-sm text-(--color-text-secondary)">{{ banner.hint }}</p>
      </aside>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityMediaJobBannerComponent {
  /** When set, copy still shows global jobs but wording reflects cross-personality blocking. */
  readonly personalityId = input<string | null>(null);

  private readonly mediaJobs = inject(PersonalityMediaJobService);
  private readonly activeJob = toSignal(this.mediaJobs.activeJob$, { initialValue: null });

  readonly copy = computed(() =>
    mediaJobBannerCopy(this.activeJob(), this.personalityId()),
  );

  constructor() {
    this.mediaJobs
      .refreshActiveJob()
      .pipe(takeUntilDestroyed())
      .subscribe();
  }
}

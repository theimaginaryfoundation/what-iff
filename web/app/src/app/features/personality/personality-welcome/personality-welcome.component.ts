import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { guideUrl } from '../../../core/constants/guides.constants';
import { SparkleIconComponent } from '../../../shared/ui/icons/icons';

/** Copied threads waiting for a persona (see PersonalityWelcomeComponent.remoteSyncWaiting). */
export interface RemoteSyncWaiting {
  threads: number;
  personas: string[];
}

/**
 * First-run welcome shown on the personalities page when the user has none yet (where the
 * personality-setup guard sends brand-new accounts). Explains what WhatIff is before asking the
 * user to make their first personality, and offers the three ways to start.
 */
@Component({
  selector: 'app-personality-welcome',
  standalone: true,
  imports: [SparkleIconComponent],
  templateUrl: './personality-welcome.component.html',
  styleUrl: './personality-welcome.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityWelcomeComponent {
  readonly generate = output<void>();
  readonly createManually = output<void>();
  readonly importData = output<void>();
  /** Offers remote sync (keeping a copy of whatiff.chat threads here) when the server runs it. */
  readonly remoteSyncAvailable = input(false);
  readonly openRemoteSync = output<void>();
  /**
   * Threads remote sync already copied here, and the remote persona names they were written with:
   * creating (or importing) a persona with one of those names gives it those threads.
   */
  readonly remoteSyncWaiting = input<RemoteSyncWaiting | null>(null);

  readonly gettingStartedHref = guideUrl('gettingStarted');

  readonly highlights = [
    {
      title: 'Personality-first',
      body: 'Choose who you talk to: a voice, expertise and look of your own, not a generic assistant.',
    },
    {
      title: 'It remembers you',
      body: 'Memories and a scratchpad it updates as you chat, so it learns how you think and work over time.',
    },
    {
      title: 'Yours to see and change',
      body: 'Inspect and edit everything it knows, switch models whenever you like, and export your data.',
    },
  ] as const;
}

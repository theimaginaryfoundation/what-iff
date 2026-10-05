import { EMPTY, Observable, catchError, distinctUntilChanged, filter, ignoreElements, map, merge, pairwise, switchMap, timer } from 'rxjs';

import { ActivePersonalityMediaJob } from '../../../core/models/personality-media-job.model';

/** What the gallery needs from the media job service (a type so tests can fake it). */
export interface MediaJobSource {
  readonly activeJob$: Observable<ActivePersonalityMediaJob | null>;
  refreshActiveJob(): Observable<ActivePersonalityMediaJob | null>;
}

const TERMINAL_STATUSES = new Set(['complete', 'failed']);

/** How often to ask whether an image job is still running while one is. */
export const MEDIA_JOB_POLL_MS = 3000;

/**
 * Emits once each time an image-generation job (default expressions, expression candidates, a
 * portrait) finishes, so the gallery can load the images it made. It also polls the job while one is
 * running, since nothing else does once you have left the page that started it (the job keeps going in
 * the background). Subscribing is what starts the polling; unsubscribe to stop it.
 */
export function mediaJobFinished$(source: MediaJobSource, pollMs = MEDIA_JOB_POLL_MS): Observable<void> {
  const running$ = source.activeJob$.pipe(
    map(job => job !== null && !TERMINAL_STATUSES.has(job.status)),
    distinctUntilChanged(),
  );
  const finished$ = running$.pipe(
    pairwise(),
    filter(([wasRunning, isRunning]) => wasRunning && !isRunning),
    map(() => undefined),
  );
  const polling$ = running$.pipe(
    switchMap(running =>
      running ? timer(pollMs, pollMs).pipe(switchMap(() => source.refreshActiveJob().pipe(catchError(() => EMPTY)))) : EMPTY,
    ),
    ignoreElements(),
  );
  return merge(finished$, polling$);
}

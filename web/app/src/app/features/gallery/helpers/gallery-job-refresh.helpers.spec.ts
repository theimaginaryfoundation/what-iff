import type { Mock } from 'vitest';
import { BehaviorSubject, Observable, of, throwError } from 'rxjs';

import { ActivePersonalityMediaJob } from '../../../core/models/personality-media-job.model';
import { mediaJobFinished$, MediaJobSource } from './gallery-job-refresh.helpers';

function job(status: string): ActivePersonalityMediaJob {
  return { job_id: 'j1', job_type: 'expression_grid', reference: 'r', status };
}

describe('mediaJobFinished$', () => {
  let active: BehaviorSubject<ActivePersonalityMediaJob | null>;
  let refresh: Mock<() => Observable<ActivePersonalityMediaJob | null>>;
  let source: MediaJobSource;
  let finished: number;
  let subscription: { unsubscribe(): void };

  beforeEach(() => {
    vi.useFakeTimers();
    active = new BehaviorSubject<ActivePersonalityMediaJob | null>(null);
    refresh = vi.fn<() => Observable<ActivePersonalityMediaJob | null>>(() => of(active.value));
    source = { activeJob$: active, refreshActiveJob: refresh };
    finished = 0;
    subscription = mediaJobFinished$(source, 1000).subscribe(() => finished++);
  });

  afterEach(() => {
    subscription.unsubscribe();
    vi.useRealTimers();
  });

  it('does nothing while there is no job', () => {
    vi.advanceTimersByTime(10_000);
    expect(finished).toBe(0);
    expect(refresh).not.toHaveBeenCalled();
  });

  it('asks about the job while it runs, and says so once when it finishes', () => {
    active.next(job('processing'));
    vi.advanceTimersByTime(3000);
    expect(refresh).toHaveBeenCalledTimes(3);
    expect(finished).toBe(0);

    active.next(job('complete'));

    expect(finished).toBe(1);
  });

  it('notices a job that is gone (no active job) as finished', () => {
    active.next(job('processing'));
    active.next(null);
    expect(finished).toBe(1);
  });

  it('notices a failed job too, since it may have left some images', () => {
    active.next(job('processing'));
    active.next(job('failed'));
    expect(finished).toBe(1);
  });

  it('stops asking once the job is over', () => {
    active.next(job('processing'));
    vi.advanceTimersByTime(1000);
    active.next(job('complete'));
    refresh.mockClear();

    vi.advanceTimersByTime(10_000);

    expect(refresh).not.toHaveBeenCalled();
    expect(finished).toBe(1);
  });

  it('does not repeat itself while the job status keeps changing but is still running', () => {
    active.next(job('queued'));
    active.next(job('processing'));
    active.next(job('processing'));
    expect(finished).toBe(0);
  });

  it('fires again for the next job', () => {
    active.next(job('processing'));
    active.next(null);
    active.next(job('processing'));
    active.next(job('complete'));
    expect(finished).toBe(2);
  });

  it('keeps going when a status check fails', () => {
    refresh.mockReturnValue(throwError(() => new Error('offline')));
    active.next(job('processing'));
    vi.advanceTimersByTime(3000);
    expect(refresh).toHaveBeenCalledTimes(3);

    active.next(job('complete'));
    expect(finished).toBe(1);
  });

  it('stops polling when unsubscribed', () => {
    active.next(job('processing'));
    subscription.unsubscribe();
    refresh.mockClear();

    vi.advanceTimersByTime(10_000);

    expect(refresh).not.toHaveBeenCalled();
  });

  it('knows about a job that was already running when it started watching', () => {
    subscription.unsubscribe();
    active = new BehaviorSubject<ActivePersonalityMediaJob | null>(job('processing'));
    source = { activeJob$: active, refreshActiveJob: refresh };
    finished = 0;
    subscription = mediaJobFinished$(source, 1000).subscribe(() => finished++);

    vi.advanceTimersByTime(1000);
    expect(refresh).toHaveBeenCalled();

    active.next(job('complete'));
    expect(finished).toBe(1);
  });
});

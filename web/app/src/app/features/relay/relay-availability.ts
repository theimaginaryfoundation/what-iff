import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, of, shareReplay } from 'rxjs';

/**
 * Whether a relay's API is there for this user, asked once: a GET of `probeUrl` (one of the
 * relay's list routes). A relay that is switched off (for the server, or, for an experimental one,
 * for this user) is not routed, so a 404 means off. For an experimental relay (`failClosed`), so
 * does a request that never got an answer (status 0: the network, or a response the browser would
 * not let us read), because it should stay hidden when we cannot tell. Any other error means on,
 * so it shows where the user can see it. The result is replayed to every later subscriber.
 */
export function relayAvailability(http: HttpClient, probeUrl: string, opts: { failClosed?: boolean } = {}): Observable<boolean> {
  const off = (status: number) => status === 404 || (!!opts.failClosed && status === 0);
  return http.get<unknown>(probeUrl).pipe(
    map(() => true),
    catchError((e: unknown) => of(!(e instanceof HttpErrorResponse && off(e.status)))),
    shareReplay({ bufferSize: 1, refCount: false }),
  );
}

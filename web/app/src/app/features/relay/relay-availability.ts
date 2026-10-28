import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, of, shareReplay } from 'rxjs';

/**
 * Whether a relay's API is there for this user, asked once: a GET of `probeUrl` (one of the
 * relay's list routes). A relay that is switched off (for the server, or, for an experimental one,
 * for this user) is not routed, so a 404 means off. Any other outcome means on, so an error then
 * shows where the user can see it. The result is replayed to every later subscriber.
 */
export function relayAvailability(http: HttpClient, probeUrl: string): Observable<boolean> {
  return http.get<unknown>(probeUrl).pipe(
    map(() => true),
    catchError((e: unknown) => of(!(e instanceof HttpErrorResponse && e.status === 404))),
    shareReplay({ bufferSize: 1, refCount: false }),
  );
}

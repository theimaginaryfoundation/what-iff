import { Injectable } from '@angular/core';
import { Observable, of } from 'rxjs';

/**
 * Extension point deciding whether access-gated features (e.g. remote MCP
 * connectors) are available to the current user. The default gate always grants
 * access; another build can supply an implementation that restricts it. The
 * concrete implementation is supplied via DI (see extensions/access-gate.providers.ts).
 */
export abstract class AccessGate {
  /** Emits true when access-gated features should be available. */
  abstract hasAccess(): Observable<boolean>;
}

/**
 * Default gate: access is always granted.
 */
@Injectable()
export class NoopAccessGate extends AccessGate {
  hasAccess(): Observable<boolean> {
    return of(true);
  }
}

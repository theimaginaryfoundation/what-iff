import { inject } from '@angular/core';
import { ActivatedRouteSnapshot, CanDeactivateFn, RouterStateSnapshot } from '@angular/router';

import { AuthService } from '../../core/services/auth.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileWorkspaceService } from './file-workspace.service';

/**
 * Asks before a navigation throws away unsaved edits in the workspace. Runs for query-parameter
 * changes too (the route sets `runGuardsAndResolvers`), because `?file=` is what is open: opening
 * another file and the browser's Back both go through here. A navigation that leaves the open
 * file where it is, such as another query parameter, does not ask.
 */
export const unsavedWorkspaceGuard: CanDeactivateFn<unknown> = (
  _component: unknown,
  current: ActivatedRouteSnapshot,
  state: RouterStateSnapshot,
  next: RouterStateSnapshot,
) => {
  if (!inject(FileWorkspaceService).unsaved()) return true;
  // Signed out (the session expired, or sign-out was confirmed): the edits cannot be saved, and
  // keeping the page would leave the user editing with nowhere to save.
  if (!inject(AuthService).isLoggedIn()) return true;
  const samePlace = state.url.split('?')[0] === next.url.split('?')[0];
  const nextFile = next.root.queryParamMap.get('file');
  if (samePlace && nextFile === current.queryParamMap.get('file')) return true;
  return inject(ConfirmationService).confirmDiscardChanges();
};

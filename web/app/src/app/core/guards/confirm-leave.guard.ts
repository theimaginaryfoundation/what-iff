import { CanDeactivateFn } from '@angular/router';

/** A routed component that may want to ask before the user navigates away from it. */
export interface LeaveConfirmable {
  /** True (or a promise of true) to let the navigation go ahead. */
  confirmLeave(): boolean | Promise<boolean>;
}

/**
 * Asks the routed component whether the user may leave (in-app navigation only; closing the tab
 * is the component's own `beforeunload` concern). Used by the job page (ADR 0x044).
 */
export const confirmLeaveGuard: CanDeactivateFn<LeaveConfirmable> = component =>
  component?.confirmLeave ? component.confirmLeave() : true;

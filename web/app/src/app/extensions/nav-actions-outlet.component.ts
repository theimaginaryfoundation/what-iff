import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * Mount point for extra sidebar-header actions, between the help and profile
 * buttons (e.g. a release-notes bell). Empty in the open-source build; a private
 * build replaces this file (via the overlay) to render its own action.
 *
 * The stub hides its host so the empty slot adds no gap to the header's flex
 * row. A replacement must set its own :host display. The header orders the
 * outlet between profile and help when the sidebar is collapsed.
 */
@Component({
  selector: 'app-nav-actions-outlet',
  standalone: true,
  template: '',
  styles: [':host { display: none; }'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class NavActionsOutletComponent {
  /** True when the sidebar is collapsed (icons stacked, tooltips to the right). */
  readonly collapsed = input(false);
}

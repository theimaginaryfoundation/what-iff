import { ChangeDetectionStrategy, Component } from '@angular/core';

/**
 * Mount point for private help UI (e.g. the feedback modal). Empty in the
 * open-source build; a private build replaces this file (via the overlay) to
 * render its modal component. app-layout renders <app-help-extension-outlet />
 * unconditionally, so the private overlay needs no app-layout edits.
 */
@Component({
  selector: 'app-help-extension-outlet',
  standalone: true,
  template: '',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class HelpExtensionOutletComponent {}

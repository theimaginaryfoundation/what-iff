import { Injectable } from '@angular/core';
import { environment } from '@environments/environment';

/**
 * The help-button action — the frontend swap-point for the help/feedback
 * feature. The open-source build opens the public docs (URL configurable via
 * `environment.docsUrl`); a private build replaces this file (via the
 * overlay) to open the in-app help/feedback modal instead. Nothing else
 * references the docs URL or the modal directly, so swapping this one file
 * changes the button's behavior wholesale.
 */
@Injectable({ providedIn: 'root' })
export class HelpActionService {
  trigger(): void {
    window.open(environment.docsUrl, '_blank', 'noopener');
  }
}

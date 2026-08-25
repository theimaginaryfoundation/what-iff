import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { ProfileSettingsModalService } from './profile-settings-modal.service';
import { ProfileSettingsModalComponent } from './profile-settings-modal.component';

@Component({
  selector: 'app-profile-settings-modal-host',
  standalone: true,
  imports: [ProfileSettingsModalComponent],
  template: `
    @defer (when modal.openState()) {
      <app-profile-settings-modal />
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProfileSettingsModalHostComponent {
  readonly modal = inject(ProfileSettingsModalService);
}

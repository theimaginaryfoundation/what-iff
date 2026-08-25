import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { AuthService } from '../../core/services/auth.service';
import { ProfileSettingsModalService } from '../../features/profile-settings/profile-settings-modal.service';
import { HelpActionService } from '../../extensions/help-action.service';
import { CircleHelpIconComponent, UserIconComponent } from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';

/**
 * Sidebar header: help trigger and profile trigger.
 */
@Component({
  selector: 'app-sidebar-header',
  standalone: true,
  imports: [TooltipDirective, CircleHelpIconComponent, UserIconComponent],
  templateUrl: './app-sidebar-header.component.html',
  styleUrl: './app-sidebar-header.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppSidebarHeaderComponent {
  readonly collapsed = input(false);

  private readonly auth = inject(AuthService);
  private readonly profileSettingsModal = inject(ProfileSettingsModalService);
  private readonly helpAction = inject(HelpActionService);

  readonly displayName = computed(() => {
    const user = this.auth.currentUser();
    if (!user) return 'Guest';
    const composed = [user.first_name, user.last_name].filter(Boolean).join(' ').trim();
    return composed || user.username || user.email || 'Guest';
  });

  openHelp(): void {
    this.helpAction.trigger();
  }

  openProfile(): void {
    this.profileSettingsModal.open('profile');
  }
}

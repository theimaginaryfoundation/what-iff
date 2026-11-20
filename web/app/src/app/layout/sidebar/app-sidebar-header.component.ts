import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';

import { NavService } from '../../core/services/nav.service';
import { HelpActionService } from '../../extensions/help-action.service';
import { NavActionsOutletComponent } from '../../extensions/nav-actions-outlet.component';
import { CircleHelpIconComponent, PanelLeftIconComponent } from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { IconButtonComponent } from '../../shared/ui/icon-button/icon-button.component';
import { ConstellationIconComponent } from '../../shared/ui/icons/constellation-icon.component';
import { ConstellationService } from '../../features/constellation/constellation.service';

/**
 * Sidebar header: help trigger, extension actions (see NavActionsOutletComponent)
 * and the desktop collapse/expand toggle.
 */
@Component({
  selector: 'app-sidebar-header',
  standalone: true,
  imports: [TooltipDirective, CircleHelpIconComponent, PanelLeftIconComponent, NavActionsOutletComponent, IconButtonComponent, ConstellationIconComponent],
  templateUrl: './app-sidebar-header.component.html',
  styleUrl: './app-sidebar-header.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppSidebarHeaderComponent {
  readonly collapsed = input(false);

  private readonly nav = inject(NavService);
  private readonly helpAction = inject(HelpActionService);
  readonly constellation = inject(ConstellationService);

  openHelp(): void {
    this.helpAction.trigger();
  }

  toggleCollapsed(): void {
    this.nav.toggleCollapsed();
  }
}

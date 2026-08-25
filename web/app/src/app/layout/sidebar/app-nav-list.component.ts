import { NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { NavItem } from './nav.helpers';

/**
 * Renders a vertical nav list. Two visual modes:
 * - expanded: icon + label rows, full width.
 * - collapsed: icon-only rows, label exposed via tooltip.
 *
 * Active state is driven by `routerLinkActive`; the helper sets
 * `aria-current="page"` when the link is active. Activation is also relayed
 * via the `select` output so the parent sidebar can close the mobile drawer.
 */
@Component({
  selector: 'app-nav-list',
  standalone: true,
  imports: [RouterLink, RouterLinkActive, NgComponentOutlet, TooltipDirective],
  templateUrl: './app-nav-list.component.html',
  styleUrl: './app-nav-list.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppNavListComponent {
  readonly items = input.required<ReadonlyArray<NavItem>>();
  readonly collapsed = input(false);
  readonly select = output<NavItem>();

  onSelect(item: NavItem): void {
    this.select.emit(item);
  }
}

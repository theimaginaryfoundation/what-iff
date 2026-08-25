import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';
import { TabsComponent } from './tabs.component';

@Component({
  selector: 'ui-tab-panel',
  standalone: true,
  templateUrl: './tab-panel.component.html',
  styleUrl: './tab-panel.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TabPanelComponent {
  readonly id = input.required<string>();
  readonly labelledBy = input.required<string>();
  private readonly tabs = inject(TabsComponent, { host: true });

  isSelected(): boolean { return this.tabs.isSelected(this.labelledBy()); }
}

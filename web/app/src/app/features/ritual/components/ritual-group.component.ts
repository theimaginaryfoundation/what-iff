import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { RitualAffinityGroupVm } from '../helpers/ritual-vm.helpers';
import { RitualRowComponent } from './ritual-row.component';

@Component({
  selector: 'app-ritual-group',
  standalone: true,
  imports: [RitualRowComponent],
  templateUrl: './ritual-group.component.html',
  styleUrl: './ritual-group.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualGroupComponent {
  readonly group = input.required<RitualAffinityGroupVm>();
  readonly selectedIds = input<string[]>([]);

  readonly open = output<string>();
  readonly delete = output<string>();
  readonly toggleSelected = output<string>();

  isSelected(id: string): boolean {
    return this.selectedIds().includes(id);
  }
}

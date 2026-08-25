
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { RitualHotkeyFilter, RitualViewFilters } from '../helpers/ritual-filter.helpers';

export interface RitualSelectOption {
  id: string;
  label: string;
}

@Component({
  selector: 'app-ritual-filter-bar',
  standalone: true,
  imports: [FormsModule],
  templateUrl: './ritual-filter-bar.component.html',
  styleUrl: './ritual-filter-bar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualFilterBarComponent {
  readonly filters = input.required<RitualViewFilters>();
  readonly personalities = input<RitualSelectOption[]>([]);
  readonly loading = input(false);

  readonly filterChanged = output<Partial<RitualViewFilters>>();
  readonly clear = output<void>();

  readonly hotkeyOptions: ReadonlyArray<{ value: RitualHotkeyFilter; label: string }> = [
    { value: 'all', label: 'All skills' },
    { value: 'with', label: 'With hotkeys' },
    { value: 'without', label: 'Without hotkeys' },
  ];
}

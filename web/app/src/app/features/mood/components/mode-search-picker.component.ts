import { Component, input, output, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { SearchIconComponent } from '../../../shared/ui/icons/icons';

export interface ModeSearchPickerOption {
  id: string;
  label: string;
}

@Component({
  selector: 'app-mode-search-picker',
  standalone: true,
  imports: [FormsModule, SearchIconComponent],
  templateUrl: './mode-search-picker.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mode-search-picker.component.scss'],
})
export class ModeSearchPickerComponent {
  placeholder = input.required<string>();
  query = input.required<string>();
  dropdownOpen = input.required<boolean>();
  options = input.required<ModeSearchPickerOption[]>();
  emptyText = input.required<string>();

  queryChange = output<string>();
  openDropdown = output<void>();
  closeDropdown = output<void>();
  selectOption = output<string>();
}

import { Component, input, output, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { SearchIconComponent } from '../../../shared/ui/icons/icons';
import { ModePersonalityVm } from '../helpers/mode-vm.helpers';

@Component({
  selector: 'app-mode-association-picker',
  standalone: true,
  imports: [CommonModule, FormsModule, AuthImagePipe, SearchIconComponent],
  templateUrl: './mode-association-picker.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mode-association-picker.component.scss'],
})
export class ModeAssociationPickerComponent {
  query = input.required<string>();
  dropdownOpen = input.required<boolean>();
  options = input.required<ModePersonalityVm[]>();

  queryChange = output<string>();
  openDropdown = output<void>();
  closeDropdown = output<void>();
  selectPersonality = output<string>();
  close = output<void>();
}

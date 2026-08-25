import { Component, input, output, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { EditIconComponent, TrashIconComponent } from '../../../shared/ui/icons/icons';
import { ModeAssociationPickerComponent } from './mode-association-picker.component';
import { ModeCardVm, ModePersonalityVm } from '../helpers/mode-vm.helpers';

@Component({
  selector: 'app-mode-card',
  standalone: true,
  imports: [CommonModule, AuthImagePipe, EditIconComponent, TrashIconComponent, ModeAssociationPickerComponent],
  templateUrl: './mode-card.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mode-card.component.scss'],
})
export class ModeCardComponent {
  card = input.required<ModeCardVm>();
  isAssociationPickerOpen = input.required<boolean>();
  associationPickerQuery = input.required<string>();
  associationDropdownOpen = input.required<boolean>();
  associationOptions = input.required<ModePersonalityVm[]>();

  edit = output<string>();
  delete = output<{ moodId: string; event: Event }>();
  removeAssociation = output<{ moodId: string; personalityId: string; event: Event }>();
  openAssociationPicker = output<{ moodId: string; event: Event }>();
  associationQueryChange = output<string>();
  associationDropdownOpenRequest = output<void>();
  associationDropdownCloseRequest = output<void>();
  addAssociation = output<{ moodId: string; personalityId: string }>();
  closeAssociationPicker = output<void>();
}

import { Component, input, output, ChangeDetectionStrategy } from '@angular/core';

import { ModeCardComponent } from './mode-card.component';
import { ModeCardVm, ModePersonalityVm } from '../helpers/mode-vm.helpers';

@Component({
  selector: 'app-mode-grid',
  standalone: true,
  imports: [ModeCardComponent],
  templateUrl: './mode-grid.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mode-grid.component.scss'],
})
export class ModeGridComponent {
  cards = input.required<ModeCardVm[]>();
  isLoading = input.required<boolean>();
  emptyText = input.required<string>();

  associationPickerMoodId = input.required<string | null>();
  associationPickerQuery = input.required<string>();
  associationDropdownOpen = input.required<boolean>();
  associationOptionsByMoodId = input.required<Record<string, ModePersonalityVm[]>>();

  editMode = output<string>();
  deleteMode = output<{ moodId: string; event: Event }>();
  removeAssociation = output<{ moodId: string; personalityId: string; event: Event }>();
  openAssociationPicker = output<{ moodId: string; event: Event }>();
  associationQueryChange = output<string>();
  openAssociationDropdown = output<void>();
  closeAssociationDropdown = output<void>();
  addAssociation = output<{ moodId: string; personalityId: string }>();
  closeAssociationPicker = output<void>();
}

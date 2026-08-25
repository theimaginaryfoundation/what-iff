import { Component, HostListener, input, output, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { XIconComponent } from '../../../shared/ui/icons/icons';
import { ModeSearchPickerComponent, ModeSearchPickerOption } from './mode-search-picker.component';

export interface ModeEditModalState {
  mode: 'create' | 'edit' | null;
  open: boolean;
  loading: boolean;
  error: string | null;
  saving: boolean;
  name: string;
  description: string;
  /** Text injected into the system prompt while this mode is active. */
  promptSnippet: string;
  recommendedModel: string;
  attachedSkills: { id: string; label: string }[];
  attachedMCPServers: { id: string; label: string }[];
}

@Component({
  selector: 'app-mode-edit-modal',
  standalone: true,
  imports: [FormsModule, XIconComponent, ModeSearchPickerComponent],
  templateUrl: './mode-edit-modal.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mode-edit-modal.component.scss'],
})
export class ModeEditModalComponent {
  state = input.required<ModeEditModalState>();
  modelOptions = input.required<ModeSearchPickerOption[]>();
  skillPickerOptions = input.required<ModeSearchPickerOption[]>();
  skillQuery = input.required<string>();
  skillDropdownOpen = input.required<boolean>();
  attachedSkillsCountLabel = input.required<string>();
  attachedMcpCountLabel = input.required<string>();

  /** Explicit close (header ✕) — dismisses immediately. */
  close = output<void>();
  /** "Soft" dismiss (backdrop click / Escape) — host may guard against unsaved edits. */
  dismissRequested = output<void>();
  save = output<void>();
  deleteMode = output<void>();
  nameChange = output<string>();
  descriptionChange = output<string>();
  promptSnippetChange = output<string>();
  recommendedModelChange = output<string>();
  skillQueryChange = output<string>();
  openSkillDropdown = output<void>();
  closeSkillDropdown = output<void>();
  addAttachedSkill = output<string>();
  removeAttachedSkill = output<string>();

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.state().open) {
      this.dismissRequested.emit();
    }
  }
}

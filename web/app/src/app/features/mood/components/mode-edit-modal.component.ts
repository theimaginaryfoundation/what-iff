import { Component, HostListener, input, output, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { XIconComponent } from '../../../shared/ui/icons/icons';
import { ModeSearchPickerComponent, ModeSearchPickerOption } from './mode-search-picker.component';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import {
  AGENT_LOOP_DESCRIPTIONS,
  AGENT_LOOP_OPTIONS,
  AgentLoop,
  REASONING_EFFORT_DESCRIPTION,
  REASONING_EFFORT_OPTIONS,
  ReasoningEffort,
} from '../../../core/models/turn-settings.model';

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
  /** Turn settings while this mode is active (ADR 0x026); `''` = don't change the thread's. */
  reasoningEffort: ReasoningEffort | '';
  agentLoop: AgentLoop | '';
  attachedSkills: { id: string; label: string }[];
  attachedMCPServers: { id: string; label: string }[];
}

@Component({
  selector: 'app-mode-edit-modal',
  standalone: true,
  imports: [FormsModule, XIconComponent, ModeSearchPickerComponent, TooltipDirective],
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
  reasoningEffortChange = output<string>();
  agentLoopChange = output<string>();
  skillQueryChange = output<string>();
  openSkillDropdown = output<void>();
  closeSkillDropdown = output<void>();
  addAttachedSkill = output<string>();
  removeAttachedSkill = output<string>();

  readonly reasoningEffortOptions = REASONING_EFFORT_OPTIONS;
  readonly reasoningEffortDescription = REASONING_EFFORT_DESCRIPTION;
  readonly agentLoopOptions = AGENT_LOOP_OPTIONS;
  readonly agentLoopDescriptions = AGENT_LOOP_DESCRIPTIONS;

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.state().open) {
      this.dismissRequested.emit();
    }
  }
}

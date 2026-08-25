import { DatePipe, UpperCasePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { MemoryCardVm, isUserScopedMemoryLevel } from '../helpers/memory-vm.helpers';
import { MemoryPersonalityOption } from './memory-form.component';
import { EditIconComponent, TrashIconComponent, UserIconComponent } from '../../../shared/ui/icons/icons';

@Component({
  selector: 'app-memory-card',
  standalone: true,
  imports: [DatePipe, EditIconComponent, FormsModule, TrashIconComponent, UpperCasePipe, UserIconComponent],
  templateUrl: './memory-card.component.html',
  styleUrl: './memory-card.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryCardComponent {
  readonly memory = input.required<MemoryCardVm>();
  readonly personalities = input<MemoryPersonalityOption[]>([]);
  readonly pinUpdating = input(false);

  readonly save = output<{ id: string; content: string }>();
  readonly pinChange = output<{ id: string; pinnedPersonalityId: string | null }>();
  readonly delete = output<string>();

  readonly editing = signal(false);
  readonly draft = signal('');
  readonly pinDraft = signal<string | null>(null);
  readonly canSave = computed(() => this.draft().trim().length > 0);
  readonly showPinControl = computed(() => isUserScopedMemoryLevel(this.memory().level));

  ngOnChanges(): void {
    if (!this.editing()) {
      this.draft.set(this.memory().content);
    }
  }

  startEdit(event?: Event): void {
    event?.stopPropagation();
    this.draft.set(this.memory().content);
    this.pinDraft.set(this.memory().pinnedPersonalityId);
    this.editing.set(true);
  }

  cancelEdit(event?: Event): void {
    event?.stopPropagation();
    this.draft.set(this.memory().content);
    this.pinDraft.set(this.memory().pinnedPersonalityId);
    this.editing.set(false);
  }

  onPinChange(personalityId: string | null, event?: Event): void {
    event?.stopPropagation();
    this.pinDraft.set(personalityId);
    this.pinChange.emit({ id: this.memory().id, pinnedPersonalityId: personalityId });
  }

  submitEdit(event?: Event): void {
    event?.stopPropagation();
    const content = this.draft().trim();
    if (!content) {
      return;
    }
    this.save.emit({ id: this.memory().id, content });
    this.editing.set(false);
  }
}

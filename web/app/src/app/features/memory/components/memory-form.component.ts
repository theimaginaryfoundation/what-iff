
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { Memory } from '../../../core/models/memory.model';
import { isUserScopedMemoryLevel } from '../helpers/memory-vm.helpers';

export interface MemoryPersonalityOption {
  id: string;
  label: string;
}

@Component({
  selector: 'app-memory-form',
  standalone: true,
  imports: [FormsModule],
  templateUrl: './memory-form.component.html',
  styleUrl: './memory-form.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryFormComponent {
  readonly memory = input.required<Memory>();
  readonly personalities = input<MemoryPersonalityOption[]>([]);
  readonly saving = input(false);
  readonly deleting = input(false);
  readonly pinUpdating = input(false);

  readonly cancel = output<void>();
  readonly remove = output<void>();
  readonly save = output<{ content: string; level: Memory['level'] }>();
  readonly pinChange = output<string | null>();

  readonly content = signal('');
  readonly level = signal<Memory['level']>('thread');
  readonly pinnedPersonalityId = signal<string | null>(null);

  readonly canSave = computed(() => this.content().trim().length > 0);
  readonly showPinControl = computed(() => isUserScopedMemoryLevel(this.level()));

  ngOnChanges(): void {
    const memory = this.memory();
    this.content.set(memory.content);
    this.level.set(memory.level);
    this.pinnedPersonalityId.set(memory.pinned_personality_id ?? null);
  }

  onLevelChange(next: Memory['level']): void {
    this.level.set(next);
    if (!isUserScopedMemoryLevel(next)) {
      this.pinnedPersonalityId.set(null);
    }
  }

  onPinnedPersonalityChange(next: string | null): void {
    this.pinnedPersonalityId.set(next);
    this.pinChange.emit(next);
  }

  onSubmit(): void {
    if (!this.canSave()) return;
    this.save.emit({
      content: this.content().trim(),
      level: this.level(),
    });
  }
}

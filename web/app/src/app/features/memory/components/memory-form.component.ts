
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { Memory, MemorySensitivity } from '../../../core/models/memory.model';
import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';
import { GLOBAL_SCOPE_LABEL, isUserScopedMemoryLevel, levelDescription } from '../helpers/memory-vm.helpers';
import {
  DEFAULT_MEMORY_SENSITIVITY,
  MEMORY_SENSITIVITY_OPTIONS,
  normalizeSensitivity,
  sensitivityDescription,
} from '../helpers/memory-sensitivity.helpers';

export interface MemoryPersonalityOption {
  id: string;
  label: string;
  accent_color?: string | null;
  cover_image_url?: string | null;
  thumbnail_circle?: PersonalityThumbnailCircle | null;
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
  readonly save = output<{ content: string; level: Memory['level']; sensitivity: MemorySensitivity }>();
  readonly pinChange = output<string | null>();

  readonly globalScopeLabel = GLOBAL_SCOPE_LABEL;
  readonly sensitivityOptions = MEMORY_SENSITIVITY_OPTIONS;

  readonly content = signal('');
  readonly level = signal<Memory['level']>('thread');
  readonly sensitivity = signal<MemorySensitivity>(DEFAULT_MEMORY_SENSITIVITY);
  readonly pinnedPersonalityId = signal<string | null>(null);

  readonly canSave = computed(() => this.content().trim().length > 0);
  readonly levelHint = computed(() => levelDescription(this.level()));
  readonly sensitivityHint = computed(() => sensitivityDescription(this.sensitivity()));
  readonly showPinControl = computed(() => isUserScopedMemoryLevel(this.level()));

  ngOnChanges(): void {
    const memory = this.memory();
    this.content.set(memory.content);
    this.level.set(memory.level);
    this.sensitivity.set(normalizeSensitivity(memory.sensitivity) ?? DEFAULT_MEMORY_SENSITIVITY);
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
      sensitivity: this.sensitivity(),
    });
  }
}

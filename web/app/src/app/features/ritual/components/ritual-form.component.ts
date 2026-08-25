
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { Ritual } from '../../../core/models/ritual.model';
import { HotkeyInputComponent } from '../../../core/components/hotkey-input/hotkey-input.component';
import { RitualSelectOption } from './ritual-filter-bar.component';

export interface RitualFormSave {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
}

@Component({
  selector: 'app-ritual-form',
  standalone: true,
  imports: [FormsModule, HotkeyInputComponent],
  templateUrl: './ritual-form.component.html',
  styleUrl: './ritual-form.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualFormComponent {
  readonly ritual = input<Ritual | null>(null);
  readonly personalities = input<RitualSelectOption[]>([]);
  readonly saving = input(false);
  readonly deleting = input(false);
  readonly isSystem = input(false);
  readonly creating = input(false);
  readonly error = input<string | null>(null);

  readonly cancel = output<void>();
  readonly save = output<RitualFormSave>();

  readonly name = signal('');
  readonly description = signal('');
  readonly content = signal('');
  readonly hotkeys = signal('');
  readonly personalityId = signal('');

  readonly canSave = computed(
    () => this.name().trim().length > 0 && this.description().trim().length > 0 && this.content().trim().length > 0,
  );

  /** True when the form differs from the ritual it was hydrated from (empty for create). */
  readonly hasUnsavedEdits = computed(() => {
    const original = this.ritual();
    return (
      this.name() !== (original?.name ?? '') ||
      this.description() !== (original?.description ?? '') ||
      this.content() !== (original?.content ?? '') ||
      this.hotkeys() !== (original?.hotkeys ?? '') ||
      this.personalityId() !== (original?.personality_id ?? '')
    );
  });

  ngOnChanges(): void {
    const ritual = this.ritual();
    if (!ritual) {
      this.name.set('');
      this.description.set('');
      this.content.set('');
      this.hotkeys.set('');
      this.personalityId.set('');
      return;
    }
    this.name.set(ritual.name);
    this.description.set(ritual.description);
    this.content.set(ritual.content);
    this.hotkeys.set(ritual.hotkeys ?? '');
    this.personalityId.set(ritual.personality_id ?? '');
  }

  submit(): void {
    if (!this.canSave()) return;
    this.save.emit({
      name: this.name().trim(),
      description: this.description().trim(),
      content: this.content().trim(),
      hotkeys: this.hotkeys().trim() || undefined,
      personality_id: this.personalityId().trim() || null,
    });
  }
}

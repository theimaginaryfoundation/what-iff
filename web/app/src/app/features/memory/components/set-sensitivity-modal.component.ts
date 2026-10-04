import { ChangeDetectionStrategy, Component, effect, input, output, signal } from '@angular/core';

import { MemorySensitivity } from '../../../core/models/memory.model';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { DEFAULT_MEMORY_SENSITIVITY, MEMORY_SENSITIVITY_OPTIONS, sensitivityLabel } from '../helpers/memory-sensitivity.helpers';

/** Confirmation dialog for setting one sensitivity level on several selected memories. */
@Component({
  selector: 'app-set-sensitivity-modal',
  standalone: true,
  imports: [ModalComponent],
  templateUrl: './set-sensitivity-modal.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SetSensitivityModalComponent {
  readonly open = input(false);
  readonly count = input(1);
  readonly saving = input(false);

  readonly confirm = output<MemorySensitivity>();
  readonly cancel = output<void>();

  readonly options = MEMORY_SENSITIVITY_OPTIONS;
  readonly level = signal<MemorySensitivity>(DEFAULT_MEMORY_SENSITIVITY);
  readonly labelFor = sensitivityLabel;

  constructor() {
    // Start from the default each time the dialog opens, not from the last choice.
    effect(() => {
      if (this.open()) this.level.set(DEFAULT_MEMORY_SENSITIVITY);
    });
  }
}

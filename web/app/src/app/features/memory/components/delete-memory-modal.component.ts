import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { ModalComponent } from '../../../shared/ui/modal/modal.component';

@Component({
  selector: 'app-delete-memory-modal',
  standalone: true,
  imports: [ModalComponent],
  templateUrl: './delete-memory-modal.component.html',
  styleUrl: './delete-memory-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DeleteMemoryModalComponent {
  readonly open = input(false);
  readonly count = input(1);
  readonly deleting = input(false);

  readonly confirm = output<void>();
  readonly cancel = output<void>();
}

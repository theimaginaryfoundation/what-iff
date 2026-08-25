import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { MemoryCardVm } from '../helpers/memory-vm.helpers';
import { MemoryCardComponent } from './memory-card.component';
import { MemoryPersonalityOption } from './memory-form.component';

@Component({
  selector: 'app-memory-card-grid',
  standalone: true,
  imports: [MemoryCardComponent],
  templateUrl: './memory-card-grid.component.html',
  styleUrl: './memory-card-grid.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryCardGridComponent {
  readonly memories = input<MemoryCardVm[]>([]);
  readonly personalities = input<MemoryPersonalityOption[]>([]);
  readonly pinUpdatingId = input<string | null>(null);
  readonly loading = input(false);
  readonly error = input<string | null>(null);
  readonly totalPages = input(1);
  readonly page = input(1);

  readonly save = output<{ id: string; content: string }>();
  readonly pinChange = output<{ id: string; pinnedPersonalityId: string | null }>();
  readonly delete = output<string>();
  readonly pageChange = output<number>();
}

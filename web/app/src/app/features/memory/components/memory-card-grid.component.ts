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
  readonly selectedIds = input<string[]>([]);
  readonly focusedId = input<string | null>(null);
  readonly isArchivedView = input(false);
  readonly readOnly = input(false);
  readonly pinUpdatingId = input<string | null>(null);
  readonly loading = input(false);
  readonly error = input<string | null>(null);
  readonly totalPages = input(1);
  readonly page = input(1);

  readonly toggleSelect = output<string>();
  readonly focus = output<string>();
  readonly save = output<{ id: string; content: string }>();
  readonly pinChange = output<{ id: string; pinnedPersonalityId: string | null }>();
  readonly starChange = output<{ id: string; starred: boolean }>();
  readonly move = output<string>();
  readonly archive = output<string>();
  readonly delete = output<string>();
  readonly pageChange = output<number>();

  isSelected(id: string): boolean {
    return this.selectedIds().includes(id);
  }
}


import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';

import {
  MemoryLevelFilter,
  MemoryScopeFilter,
  MemoryViewFilters,
} from '../helpers/memory-filter.helpers';

export interface MemorySelectOption {
  id: string;
  label: string;
}

@Component({
  selector: 'app-memory-filter-bar',
  standalone: true,
  imports: [FormsModule],
  templateUrl: './memory-filter-bar.component.html',
  styleUrl: './memory-filter-bar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryFilterBarComponent {
  readonly scopeOptions = SCOPE_OPTIONS;
  readonly levelOptions = LEVEL_OPTIONS;
  readonly filters = input.required<MemoryViewFilters>();
  readonly personalities = input<MemorySelectOption[]>([]);
  readonly chats = input<MemorySelectOption[]>([]);
  readonly levelEnabled = input(false);
  readonly importEnabled = input(false);
  readonly loading = input(false);

  readonly filtersChanged = output<Partial<MemoryViewFilters>>();
  readonly clear = output<void>();
}

export const SCOPE_OPTIONS: ReadonlyArray<{ value: MemoryScopeFilter; label: string }> = [
  { value: 'all', label: 'All scopes' },
  { value: 'user', label: 'User memories' },
  { value: 'chat', label: 'Chat memories' },
];

export const LEVEL_OPTIONS: ReadonlyArray<{ value: MemoryLevelFilter; label: string }> = [
  { value: 'all', label: 'All levels' },
  { value: 'global', label: 'Global' },
  { value: 'personality', label: 'Personality' },
  { value: 'thread', label: 'Thread' },
  { value: 'summary', label: 'Summary' },
];

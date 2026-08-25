import { Injectable, computed, inject, signal } from '@angular/core';
import { finalize, forkJoin, map, of } from 'rxjs';

import { Memory } from '../models/memory.model';
import { MemoryService } from './memory.service';
import {
  DEFAULT_MEMORY_VIEW_FILTERS,
  MemoryViewFilters,
  toApiFilters,
} from '../../features/memory/helpers/memory-filter.helpers';

@Injectable({ providedIn: 'root' })
export class MemoryViewService {
  private readonly memoryService = inject(MemoryService);

  readonly pageSize = 24;
  readonly memories = signal<Memory[]>([]);
  readonly totalCount = signal(0);
  readonly currentPage = signal(1);
  readonly loading = signal(false);
  readonly deleting = signal(false);
  readonly error = signal<string | null>(null);
  readonly filters = signal<MemoryViewFilters>({ ...DEFAULT_MEMORY_VIEW_FILTERS });
  readonly selectedIds = signal<string[]>([]);
  readonly associationFilterMode = signal<'all' | 'global' | 'personality'>('all');
  readonly selectedPersonalityIds = signal<string[]>([]);

  readonly hasMemories = computed(() => this.memories().length > 0);
  readonly totalPages = computed(() => Math.max(1, Math.ceil(this.totalCount() / this.pageSize)));
  readonly allSelected = computed(() => this.memories().length > 0 && this.selectedIds().length === this.memories().length);

  load(page: number = 1): void {
    this.loading.set(true);
    this.error.set(null);
    this.currentPage.set(page);
    this.memoryService
      .getMemories(page, this.pageSize, this.toRequestFilters())
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe({
        next: response => {
          this.memories.set(response.results ?? []);
          this.totalCount.set(response.total_count ?? 0);
          this.selectedIds.set([]);
        },
        error: error => {
          this.memories.set([]);
          this.totalCount.set(0);
          this.error.set(error instanceof Error ? error.message : 'Failed to load memories');
        },
      });
  }

  setFilters(partial: Partial<MemoryViewFilters>): void {
    this.filters.update(current => ({ ...current, ...partial }));
    this.load(1);
  }

  applyFilters(filters: MemoryViewFilters): void {
    this.filters.set({ ...filters });
    this.load(1);
  }

  clearFilters(): void {
    this.filters.set({ ...DEFAULT_MEMORY_VIEW_FILTERS });
    this.load(1);
  }

  toggleSelection(memoryId: string): void {
    this.selectedIds.update(current =>
      current.includes(memoryId) ? current.filter(id => id !== memoryId) : [...current, memoryId],
    );
  }

  setAllSelected(selected: boolean): void {
    this.selectedIds.set(selected ? this.memories().map(memory => memory.id) : []);
  }

  deleteSelected() {
    const ids = this.selectedIds();
    if (ids.length === 0) {
      return of(void 0);
    }
    this.deleting.set(true);
    return forkJoin(ids.map(id => this.memoryService.deleteMemory(id))).pipe(
      map(() => void 0),
      finalize(() => this.deleting.set(false)),
    );
  }

  deleteOne(memoryId: string) {
    this.deleting.set(true);
    return this.memoryService.deleteMemory(memoryId).pipe(finalize(() => this.deleting.set(false)));
  }

  setSelectedPersonalityIds(ids: readonly string[]): void {
    this.selectedPersonalityIds.set([...ids]);
    this.associationFilterMode.set(ids.length > 0 ? 'personality' : 'all');
    this.load(1);
  }

  selectAllAssociations(): void {
    this.selectedPersonalityIds.set([]);
    this.associationFilterMode.set('all');
    this.load(1);
  }

  selectGlobalAssociations(): void {
    this.selectedPersonalityIds.set([]);
    this.associationFilterMode.set('global');
    this.load(1);
  }

  private toRequestFilters() {
    const base = toApiFilters(this.filters());
    const mode = this.associationFilterMode();
    if (mode === 'global') {
      return {
        ...base,
        pinned_personality_id: undefined,
        pinned_personality_ids: undefined,
        global_only: true,
      };
    }
    if (mode === 'personality' && this.selectedPersonalityIds().length > 0) {
      return {
        ...base,
        pinned_personality_id: undefined,
        pinned_personality_ids: [...this.selectedPersonalityIds()],
        global_only: undefined,
      };
    }
    return {
      ...base,
      pinned_personality_ids: undefined,
      global_only: undefined,
    };
  }
}

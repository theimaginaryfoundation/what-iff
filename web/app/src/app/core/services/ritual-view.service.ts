import { Injectable, computed, inject, signal } from '@angular/core';
import { finalize, forkJoin, map, Observable, of } from 'rxjs';

import { Ritual } from '../models/ritual.model';
import { RitualService } from './ritual.service';
import {
  DEFAULT_RITUAL_VIEW_FILTERS,
  RitualViewFilters,
  toApiFilters,
} from '../../features/ritual/helpers/ritual-filter.helpers';

@Injectable({ providedIn: 'root' })
export class RitualViewService {
  private readonly ritualService = inject(RitualService);

  readonly pageSize = 24;
  readonly rituals = signal<Ritual[]>([]);
  readonly systemRituals = signal<Ritual[]>([]);
  readonly totalCount = signal(0);
  readonly currentPage = signal(1);
  readonly loading = signal(false);
  readonly deleting = signal(false);
  readonly error = signal<string | null>(null);
  readonly filters = signal<RitualViewFilters>({ ...DEFAULT_RITUAL_VIEW_FILTERS });
  readonly selectedIds = signal<string[]>([]);
  readonly selectedRitualId = signal<string | null>(null);

  readonly totalPages = computed(() => Math.max(1, Math.ceil(this.totalCount() / this.pageSize)));
  readonly allSelected = computed(() => this.rituals().length > 0 && this.selectedIds().length === this.rituals().length);

  load(page: number = 1): void {
    this.loading.set(true);
    this.error.set(null);
    this.currentPage.set(page);
    this.ritualService
      .listRituals(page, this.pageSize, toApiFilters(this.filters()))
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe({
        next: response => {
          this.rituals.set(response.results ?? []);
          this.totalCount.set(response.total_count ?? 0);
          this.selectedIds.set([]);
        },
        error: error => {
          this.rituals.set([]);
          this.totalCount.set(0);
          this.error.set(error instanceof Error ? error.message : 'Failed to load skills');
        },
      });
  }

  loadSystemRituals(): void {
    this.ritualService.listSystemRituals().subscribe({
      next: rituals => this.systemRituals.set(rituals ?? []),
      error: () => this.systemRituals.set([]),
    });
  }

  refresh(): void {
    this.load(this.currentPage());
    this.loadSystemRituals();
  }

  setFilters(partial: Partial<RitualViewFilters>): void {
    this.filters.update(current => ({ ...current, ...partial }));
    this.load(1);
  }

  applyFilters(filters: RitualViewFilters): void {
    this.filters.set({ ...filters });
    this.load(1);
  }

  clearFilters(): void {
    this.filters.set({ ...DEFAULT_RITUAL_VIEW_FILTERS });
    this.load(1);
  }

  toggleSelection(ritualId: string): void {
    this.selectedIds.update(current =>
      current.includes(ritualId) ? current.filter(id => id !== ritualId) : [...current, ritualId],
    );
  }

  setAllSelected(selected: boolean): void {
    this.selectedIds.set(selected ? this.rituals().map(ritual => ritual.id) : []);
  }

  setSelectedRitualId(ritualId: string | null): void {
    this.selectedRitualId.set(ritualId);
  }

  deleteSelected(): Observable<void> {
    const ids = this.selectedIds();
    if (ids.length === 0) return of(void 0);

    this.deleting.set(true);
    return forkJoin(ids.map(id => this.ritualService.deleteRitual(id))).pipe(
      map(() => void 0),
      finalize(() => this.deleting.set(false)),
    );
  }

  deleteOne(ritualId: string): Observable<void> {
    this.deleting.set(true);
    return this.ritualService.deleteRitual(ritualId).pipe(finalize(() => this.deleting.set(false)));
  }
}


import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';

import { MemoryService } from '../../core/services/memory.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemorySort } from '../../core/models/memory.model';
import { toMemoryCardVm } from './helpers/memory-vm.helpers';
import {
  parseQueryParams,
  serializeFilters,
  MemoryViewFilters,
} from './helpers/memory-filter.helpers';
import { MemorySelectOption } from './components/memory-filter-bar.component';
import { MemoryCardGridComponent } from './components/memory-card-grid.component';
import { DeleteMemoryModalComponent } from './components/delete-memory-modal.component';
import { ChevDownIconComponent, UploadIconComponent } from '../../shared/ui/icons/icons';

@Component({
  selector: 'app-memories-page',
  standalone: true,
  imports: [
    MemoryCardGridComponent,
    DeleteMemoryModalComponent,
    ChevDownIconComponent,
    UploadIconComponent,
    RouterLink
],
  templateUrl: './memories-page.component.html',
  styleUrl: './memories-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoriesPageComponent implements OnInit {
  private readonly view = inject(MemoryViewService);
  private readonly memoryService = inject(MemoryService);
  private readonly personalityService = inject(PersonalityService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly personalities = signal<MemorySelectOption[]>([]);
  readonly deleteModalOpen = signal(false);
  readonly deleteTargetIds = signal<string[]>([]);

  readonly filters = this.view.filters;
  readonly loading = this.view.loading;
  readonly error = this.view.error;
  readonly totalPages = this.view.totalPages;
  readonly currentPage = this.view.currentPage;
  readonly totalCount = this.view.totalCount;
  readonly personalityNames = computed<Record<string, string>>(() =>
    this.personalities().reduce<Record<string, string>>((acc, personality) => {
      acc[personality.id] = personality.label;
      return acc;
    }, {}),
  );
  readonly memoryLevelFilter = computed(() => this.filters().level);
  readonly memoryAssociationMode = computed(() => this.view.associationFilterMode());
  readonly isLevelSelectionActive = computed(() => {
    const level = this.filters().level;
    return level === 'personality' || level === 'thread' || level === 'summary';
  });
  readonly memories = computed(() => this.view.memories().map(memory => toMemoryCardVm(memory, 160, this.personalityNames())));
  readonly deleting = this.view.deleting;
  readonly pinUpdatingId = signal<string | null>(null);

  ngOnInit(): void {
    this.personalityService.listPersonalities(1, 200).subscribe({
      next: result => {
        this.personalities.set((result.results ?? []).map(item => ({ id: item.id, label: item.name })));
      },
      error: () => {
        this.personalities.set([]);
      },
    });

    this.route.queryParams.subscribe(params => {
      this.view.applyFilters(parseQueryParams(params));
    });
  }

  onFilterChanged(partial: Partial<MemoryViewFilters>): void {
    const next = { ...this.filters(), ...partial };
    this.view.setFilters(partial);
    void this.router.navigate([], { queryParams: serializeFilters(next), replaceUrl: true });
  }

  selectAssociationFilter(mode: 'all' | 'global'): void {
    // Association mode owns these two buttons. Reset level filters first.
    this.onFilterChanged({ level: 'all' });
    if (mode === 'global') {
      this.view.selectGlobalAssociations();
      return;
    }
    this.view.selectAllAssociations();
  }

  selectLevelFilter(level: 'personality' | 'thread' | 'summary'): void {
    this.onFilterChanged({ level });
    // Level filters are mutually exclusive with personality association filters.
    this.view.selectAllAssociations();
    this.view.setSelectedPersonalityIds([]);
  }

  setSort(sort: MemorySort): void {
    this.onFilterChanged({ sort });
  }

  onClearFilters(): void {
    this.view.clearFilters();
    void this.router.navigate([], { queryParams: {}, replaceUrl: true });
  }

  onDeleteSingle(memoryId: string): void {
    this.deleteTargetIds.set([memoryId]);
    this.deleteModalOpen.set(true);
  }

  confirmDelete(): void {
    const ids = this.deleteTargetIds();
    const request$ = ids.length === 1 ? this.view.deleteOne(ids[0]) : this.view.deleteSelected();
    request$.subscribe({
      next: () => {
        this.deleteModalOpen.set(false);
        this.deleteTargetIds.set([]);
        this.view.load(this.currentPage());
      },
      error: () => {
        this.deleteModalOpen.set(false);
      },
    });
  }

  closeDeleteModal(): void {
    this.deleteModalOpen.set(false);
    this.deleteTargetIds.set([]);
  }

  goToPage(page: number): void {
    this.view.load(page);
  }

  onInlineSaveMemory(event: { id: string; content: string }): void {
    this.memoryService.patchMemory(event.id, { content: event.content }).subscribe({
      next: () => this.view.load(this.currentPage()),
      error: () => this.view.load(this.currentPage()),
    });
  }

  onMemoryPinChange(event: { id: string; pinnedPersonalityId: string | null }): void {
    this.pinUpdatingId.set(event.id);
    this.memoryService.updateMemoryPin(event.id, event.pinnedPersonalityId).subscribe({
      next: () => {
        this.pinUpdatingId.set(null);
        this.view.load(this.currentPage());
      },
      error: () => {
        this.pinUpdatingId.set(null);
        this.view.load(this.currentPage());
      },
    });
  }
}

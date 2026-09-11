import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  OnInit,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { fromEvent } from 'rxjs';

import { MemoryService } from '../../core/services/memory.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemoryMergeEvent, MemorySort } from '../../core/models/memory.model';
import { toMemoryCardVm } from './helpers/memory-vm.helpers';
import {
  parseQueryParams,
  serializeFilters,
  normalizeDateRange,
  MemoryViewFilters,
  MemoryStatusFilter,
} from './helpers/memory-filter.helpers';
import { MemoryPersonalityOption } from './components/memory-form.component';
import { MemoryCardGridComponent } from './components/memory-card-grid.component';
import { MemoryFocusPanelComponent } from './components/memory-focus-panel.component';
import { DeleteMemoryModalComponent } from './components/delete-memory-modal.component';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import {
  CalendarIconComponent,
  ChevDownIconComponent,
  DownloadIconComponent,
  SearchIconComponent,
} from '../../shared/ui/icons/icons';

/** Matches memories-list-tab SCSS: rail beside list at >960px, modal below. */
const DESKTOP_FOCUS_QUERY = '(min-width: 961px)';

@Component({
  selector: 'app-memories-list-tab',
  standalone: true,
  imports: [
    FormsModule,
    MemoryCardGridComponent,
    MemoryFocusPanelComponent,
    DeleteMemoryModalComponent,
    ModalComponent,
    CalendarIconComponent,
    ChevDownIconComponent,
    DownloadIconComponent,
    SearchIconComponent,
  ],
  templateUrl: './memories-list-tab.component.html',
  styleUrl: './memories-list-tab.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoriesListTabComponent implements OnInit {
  private readonly view = inject(MemoryViewService);
  private readonly memoryService = inject(MemoryService);
  private readonly personalityService = inject(PersonalityService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);
  private readonly destroyRef = inject(DestroyRef);

  readonly personalities = signal<MemoryPersonalityOption[]>([]);
  readonly deleteModalOpen = signal(false);
  readonly deleteTargetIds = signal<string[]>([]);
  readonly moveMenuOpen = signal(false);
  readonly moveTargetIds = signal<string[]>([]);
  readonly searchDraft = signal('');
  readonly pinUpdatingId = signal<string | null>(null);
  readonly exporting = signal(false);
  readonly focusedId = signal<string | null>(null);
  readonly mergeEvents = signal<MemoryMergeEvent[]>([]);
  readonly mergeEventsLoading = signal(false);
  readonly dateRangeError = signal<string | null>(null);
  readonly isDesktopFocusLayout = signal(this.readDesktopFocusLayout());
  private mergeEventsLoaded = false;

  readonly filters = this.view.filters;
  readonly loading = this.view.loading;
  readonly error = this.view.error;
  readonly totalPages = this.view.totalPages;
  readonly currentPage = this.view.currentPage;
  readonly totalCount = this.view.totalCount;
  readonly selectedIds = this.view.selectedIds;
  readonly selectedCount = this.view.selectedCount;
  readonly allSelected = this.view.allSelected;
  readonly deleting = this.view.deleting;
  readonly mutating = this.view.mutating;

  readonly personalityNames = computed<Record<string, string>>(() =>
    this.personalities().reduce<Record<string, string>>((acc, personality) => {
      acc[personality.id] = personality.label;
      return acc;
    }, {}),
  );
  readonly memories = computed(() =>
    this.view.memories().map(memory => toMemoryCardVm(memory, 220, this.personalityNames())),
  );
  readonly focusedMemory = computed(() => {
    const id = this.focusedId();
    if (!id) return null;
    return this.memories().find(memory => memory.id === id) ?? null;
  });
  readonly statusFilter = computed(() => this.filters().status);
  readonly isArchivedView = computed(() => this.statusFilter() === 'inactive');
  readonly isSummariesView = computed(() => this.statusFilter() === 'summaries');
  readonly countLabel = computed(() => {
    const n = this.totalCount();
    if (this.isSummariesView()) {
      return `${n} summar${n === 1 ? 'y' : 'ies'}`;
    }
    return `${n} memor${n === 1 ? 'y' : 'ies'}`;
  });
  readonly showDesktopFocusRail = computed(
    () => !!this.focusedMemory() && this.isDesktopFocusLayout(),
  );
  readonly showMobileFocusModal = computed(
    () => !!this.focusedMemory() && !this.isDesktopFocusLayout(),
  );

  ngOnInit(): void {
    this.bindDesktopFocusLayout();

    this.personalityService.listPersonalities(1, 200).subscribe({
      next: result => {
        this.personalities.set(
          (result.results ?? []).map(item => ({
            id: item.id,
            label: item.name,
            accent_color: item.accent_color ?? null,
            cover_image_url: item.cover_image_url ?? null,
            thumbnail_circle: item.thumbnail_circle ?? null,
          })),
        );
      },
      error: () => {
        this.personalities.set([]);
      },
    });

    this.route.queryParams.subscribe(params => {
      const parsed = parseQueryParams(params);
      this.searchDraft.set(parsed.query);
      this.dateRangeError.set(null);
      this.view.applyFilters(parsed);
    });
  }

  private bindDesktopFocusLayout(): void {
    const view = this.document.defaultView;
    if (!view?.matchMedia) {
      return;
    }
    const media = view.matchMedia(DESKTOP_FOCUS_QUERY);
    fromEvent(media, 'change')
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => this.isDesktopFocusLayout.set(media.matches));
  }

  private readDesktopFocusLayout(): boolean {
    const view = this.document.defaultView;
    if (!view?.matchMedia) {
      return true;
    }
    return view.matchMedia(DESKTOP_FOCUS_QUERY).matches;
  }

  onFilterChanged(partial: Partial<MemoryViewFilters>): void {
    const next = { ...this.filters(), ...partial };
    this.view.setFilters(partial);
    void this.router.navigate([], {
      queryParams: {
        ...serializeFilters(next),
        tab: this.route.snapshot.queryParamMap.get('tab'),
      },
      replaceUrl: true,
    });
  }

  setStatusFilter(status: MemoryStatusFilter): void {
    if (status === 'summaries') {
      this.clearSelection();
      this.clearFocus();
      this.onFilterChanged({ status, level: 'all' });
      this.view.selectAllAssociations();
      return;
    }
    this.onFilterChanged({ status, level: this.filters().level === 'summary' ? 'all' : this.filters().level });
  }

  setSort(sort: MemorySort): void {
    this.onFilterChanged({ sort });
  }

  onSearchSubmit(): void {
    this.onFilterChanged({ query: this.searchDraft().trim() });
  }

  onPersonalityFilterChange(personalityId: string): void {
    this.onFilterChanged({ personalityId });
    if (personalityId) {
      this.view.setSelectedPersonalityIds([personalityId]);
    } else {
      this.view.selectAllAssociations();
    }
  }

  onMinDateChange(value: string): void {
    this.applyDateRange(value, this.filters().maxDate);
  }

  onMaxDateChange(value: string): void {
    this.applyDateRange(this.filters().minDate, value);
  }

  openDatePicker(input: HTMLInputElement): void {
    input.focus();
    const picker = (input as HTMLInputElement & { showPicker?: () => void }).showPicker;
    if (typeof picker === 'function') {
      try {
        picker.call(input);
        return;
      } catch {
        // Fall through to focus — some browsers only allow showPicker from direct user gestures.
      }
    }
  }

  private applyDateRange(minDate: string, maxDate: string): void {
    const normalized = normalizeDateRange(minDate, maxDate);
    this.dateRangeError.set(normalized.error);
    this.onFilterChanged({ minDate: normalized.minDate, maxDate: normalized.maxDate });
  }

  onClearFilters(): void {
    this.searchDraft.set('');
    this.dateRangeError.set(null);
    this.view.clearFilters();
    void this.router.navigate([], {
      queryParams: {
        tab: this.route.snapshot.queryParamMap.get('tab'),
      },
      replaceUrl: true,
    });
  }

  refresh(): void {
    this.view.load(this.currentPage());
  }

  focusMemory(id: string): void {
    this.focusedId.set(id);
    this.ensureMergeEventsLoaded();
  }

  clearFocus(): void {
    this.focusedId.set(null);
  }

  private ensureMergeEventsLoaded(): void {
    if (this.mergeEventsLoaded || this.mergeEventsLoading()) return;
    this.mergeEventsLoading.set(true);
    this.memoryService.listMergeEvents(1, 100).subscribe({
      next: response => {
        this.mergeEvents.set(response.results ?? []);
        this.mergeEventsLoaded = true;
        this.mergeEventsLoading.set(false);
      },
      error: () => {
        this.mergeEvents.set([]);
        this.mergeEventsLoaded = true;
        this.mergeEventsLoading.set(false);
      },
    });
  }

  toggleSelection(id: string): void {
    this.view.toggleSelection(id);
  }

  setAllSelected(selected: boolean): void {
    this.view.setAllSelected(selected);
  }

  clearSelection(): void {
    this.view.clearSelection();
  }

  onDeleteSingle(memoryId: string): void {
    this.deleteTargetIds.set([memoryId]);
    this.deleteModalOpen.set(true);
  }

  onDeleteSelected(): void {
    const ids = this.selectedIds();
    if (ids.length === 0) return;
    this.deleteTargetIds.set([...ids]);
    this.deleteModalOpen.set(true);
  }

  confirmDelete(): void {
    const ids = this.deleteTargetIds();
    const request$ = ids.length === 1 ? this.view.deleteOne(ids[0]) : this.view.deleteSelected();
    request$.subscribe({
      next: () => {
        this.deleteModalOpen.set(false);
        this.deleteTargetIds.set([]);
        if (this.focusedId() && ids.includes(this.focusedId()!)) {
          this.clearFocus();
        }
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

  onFocusEdit(memoryId: string): void {
    void this.router.navigate(['/memories', memoryId]);
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

  onToggleStar(event: { id: string; starred: boolean }): void {
    this.view.patchOne(event.id, { starred: event.starred }).subscribe({
      next: () => this.view.load(this.currentPage()),
      error: () => this.view.load(this.currentPage()),
    });
  }

  onArchiveOne(memoryId: string): void {
    const nextStatus = this.isArchivedView() ? 'active' : 'inactive';
    this.view.patchOne(memoryId, { status: nextStatus }).subscribe({
      next: () => this.view.load(this.currentPage()),
      error: () => this.view.load(this.currentPage()),
    });
  }

  onArchiveSelected(): void {
    const nextStatus = this.isArchivedView() ? 'active' : 'inactive';
    this.view.patchSelected({ status: nextStatus }).subscribe({
      next: () => this.view.load(this.currentPage()),
      error: () => this.view.load(this.currentPage()),
    });
  }

  openMoveMenu(ids: string[]): void {
    this.moveTargetIds.set(ids);
    this.moveMenuOpen.set(true);
  }

  closeMoveMenu(): void {
    this.moveMenuOpen.set(false);
    this.moveTargetIds.set([]);
  }

  moveToPersonality(personalityId: string | null): void {
    const ids = this.moveTargetIds();
    if (ids.length === 0) return;
    const patch =
      personalityId === null
        ? { level: 'global' as const, pinned_personality_id: null }
        : { level: 'personality' as const, pinned_personality_id: personalityId };

    const request$ =
      ids.length === 1
        ? this.view.patchOne(ids[0], patch)
        : (() => {
            this.view.setSelectedIds(ids);
            return this.view.patchSelected(patch);
          })();

    request$.subscribe({
      next: () => {
        this.closeMoveMenu();
        this.view.load(this.currentPage());
      },
      error: () => this.closeMoveMenu(),
    });
  }

  onMoveOne(memoryId: string): void {
    this.openMoveMenu([memoryId]);
  }

  onMoveSelected(): void {
    this.openMoveMenu([...this.selectedIds()]);
  }

  exportMemories(): void {
    this.exporting.set(true);
    this.memoryService.exportMemories().subscribe({
      next: blob => {
        const url = URL.createObjectURL(blob);
        try {
          const anchor = document.createElement('a');
          anchor.href = url;
          anchor.download = 'memories-export.zip';
          anchor.click();
        } finally {
          URL.revokeObjectURL(url);
          this.exporting.set(false);
        }
      },
      error: () => this.exporting.set(false),
    });
  }
}

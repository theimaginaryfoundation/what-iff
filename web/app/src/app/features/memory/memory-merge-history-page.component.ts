import { CommonModule, DatePipe, UpperCasePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';

import { MemoryMergeEvent, MemoryMergeSourceMember } from '../../core/models/memory.model';
import { MemoryService } from '../../core/services/memory.service';

@Component({
  selector: 'app-memory-merge-history-page',
  standalone: true,
  imports: [CommonModule, DatePipe, RouterLink, UpperCasePipe],
  templateUrl: './memory-merge-history-page.component.html',
  styleUrl: './memory-merge-history-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryMergeHistoryPageComponent implements OnInit {
  private readonly memoryService = inject(MemoryService);
  private readonly router = inject(Router);

  readonly events = signal<MemoryMergeEvent[]>([]);
  readonly loading = signal(true);
  readonly error = signal<string | null>(null);
  readonly undoingId = signal<string | null>(null);
  readonly expandedIds = signal<Set<string>>(new Set());
  readonly page = signal(1);
  readonly totalCount = signal(0);
  readonly totalPages = signal(1);

  ngOnInit(): void {
    this.load(1);
  }

  load(page: number): void {
    this.loading.set(true);
    this.error.set(null);
    this.memoryService.listMergeEvents(page, 20).subscribe({
      next: response => {
        this.events.set(response.results ?? []);
        this.page.set(response.page ?? page);
        this.totalCount.set(response.total_count ?? 0);
        this.totalPages.set(Math.max(1, Math.ceil((response.total_count ?? 0) / 20)));
        this.loading.set(false);
      },
      error: err => {
        this.error.set(err instanceof Error ? err.message : 'Failed to load merge history');
        this.loading.set(false);
      },
    });
  }

  mergeTypeLabel(event: MemoryMergeEvent): string {
    switch (event.merge_type) {
      case 'link':
        return 'Linked related memories';
      case 'fold_live':
        return 'Memories Merged';
      default:
        return 'Created from batch';
    }
  }

  /** Source memories that went into a fold (stored duplicates_folded is absorb-count = n-1). */
  mergedSourceCount(event: MemoryMergeEvent): number {
    const fromMembers = event.source_members?.length ?? 0;
    if (fromMembers > 0) {
      return fromMembers;
    }
    return Math.max(event.duplicates_folded + 1, 0);
  }

  isLink(event: MemoryMergeEvent): boolean {
    return event.merge_type === 'link';
  }

  isCreate(event: MemoryMergeEvent): boolean {
    return event.merge_type === 'create';
  }

  isReverted(event: MemoryMergeEvent): boolean {
    return !!event.reverted_at;
  }

  isExpanded(event: MemoryMergeEvent): boolean {
    return this.expandedIds().has(event.id);
  }

  toggleExpanded(event: MemoryMergeEvent): void {
    const next = new Set(this.expandedIds());
    if (next.has(event.id)) {
      next.delete(event.id);
    } else {
      next.add(event.id);
    }
    this.expandedIds.set(next);
  }

  sourceMemberLabel(member: MemoryMergeSourceMember): string {
    return member.is_new ? 'New extraction' : 'Stored memory';
  }

  undo(event: MemoryMergeEvent): void {
    if (this.isReverted(event) || this.undoingId()) {
      return;
    }
    this.undoingId.set(event.id);
    this.memoryService.undoMergeEvent(event.id).subscribe({
      next: () => {
        this.undoingId.set(null);
        this.load(this.page());
      },
      error: err => {
        this.error.set(err instanceof Error ? err.message : 'Failed to undo merge');
        this.undoingId.set(null);
      },
    });
  }

  openMemory(event: MemoryMergeEvent): void {
    void this.router.navigate(['/memories', event.survivor_memory_id]);
  }

  openSourceMemory(member: MemoryMergeSourceMember): void {
    if (!member.memory_id) {
      return;
    }
    void this.router.navigate(['/memories', member.memory_id]);
  }

  goToPage(page: number): void {
    if (page < 1 || page > this.totalPages()) {
      return;
    }
    this.load(page);
  }
}

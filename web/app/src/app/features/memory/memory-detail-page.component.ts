import { CommonModule, DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';

import { Memory } from '../../core/models/memory.model';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemoryFormComponent, MemoryPersonalityOption } from './components/memory-form.component';
import { DeleteMemoryModalComponent } from './components/delete-memory-modal.component';

@Component({
  selector: 'app-memory-detail-page',
  standalone: true,
  imports: [CommonModule, DatePipe, MemoryFormComponent, DeleteMemoryModalComponent, RouterLink],
  templateUrl: './memory-detail-page.component.html',
  styleUrl: './memory-detail-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryDetailPageComponent implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly memoryService = inject(MemoryService);
  private readonly personalityService = inject(PersonalityService);

  readonly memory = signal<Memory | null>(null);
  readonly personalities = signal<MemoryPersonalityOption[]>([]);
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly deleting = signal(false);
  readonly pinUpdating = signal(false);
  readonly error = signal<string | null>(null);
  readonly deleteModalOpen = signal(false);

  readonly memoryId = computed(() => this.route.snapshot.paramMap.get('id') ?? '');

  ngOnInit(): void {
    this.personalityService.listPersonalities(1, 200).subscribe({
      next: result => {
        this.personalities.set((result.results ?? []).map(item => ({ id: item.id, label: item.name })));
      },
      error: () => this.personalities.set([]),
    });
    this.load();
  }

  load(): void {
    const id = this.memoryId();
    if (!id) {
      this.error.set('Invalid memory ID.');
      this.loading.set(false);
      return;
    }
    this.loading.set(true);
    this.error.set(null);
    this.memoryService.getMemoryById(id).subscribe({
      next: memory => {
        this.memory.set(memory);
        this.loading.set(false);
      },
      error: error => {
        this.error.set(error instanceof Error ? error.message : 'Failed to load memory');
        this.loading.set(false);
      },
    });
  }

  goBack(): void {
    void this.router.navigate(['/memories'], { queryParamsHandling: 'merge' });
  }

  onPinChange(pinnedPersonalityId: string | null): void {
    if (!this.memory()) return;
    this.pinUpdating.set(true);
    this.memoryService.updateMemoryPin(this.memory()!.id, pinnedPersonalityId).subscribe({
      next: updated => {
        this.memory.set(updated);
        this.pinUpdating.set(false);
      },
      error: error => {
        this.error.set(error instanceof Error ? error.message : 'Failed to update memory pin');
        this.pinUpdating.set(false);
      },
    });
  }

  save(changes: { content: string; level: Memory['level'] }): void {
    if (!this.memory()) return;
    this.saving.set(true);
    this.memoryService.patchMemory(this.memory()!.id, changes).subscribe({
      next: updated => {
        this.memory.set(updated);
        this.saving.set(false);
      },
      error: error => {
        this.error.set(error instanceof Error ? error.message : 'Failed to save memory');
        this.saving.set(false);
      },
    });
  }

  requestDelete(): void {
    this.deleteModalOpen.set(true);
  }

  cancelDelete(): void {
    this.deleteModalOpen.set(false);
  }

  confirmDelete(): void {
    if (!this.memory()) return;
    this.deleting.set(true);
    this.memoryService.deleteMemory(this.memory()!.id).subscribe({
      next: () => {
        this.deleting.set(false);
        void this.router.navigate(['/memories'], { queryParams: { deleted: 'true' } });
      },
      error: error => {
        this.error.set(error instanceof Error ? error.message : 'Failed to delete memory');
        this.deleting.set(false);
        this.deleteModalOpen.set(false);
      },
    });
  }
}

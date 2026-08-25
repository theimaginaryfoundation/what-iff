import { Component, OnInit, signal, inject, computed, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Memory } from '../../core/models/memory.model';
import { Personality } from '../../core/models/personality.model';

@Component({
  selector: 'app-memory-detail',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './memory-detail.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./memory-detail.component.scss']
})
export class MemoryDetailComponent implements OnInit {
  // Signals for reactive state management
  memory = signal<Memory | null>(null);
  loading = signal<boolean>(true);
  error = signal<string | null>(null);
  personalities = signal<Personality[]>([]);
  isLoadingPersonalities = signal<boolean>(false);
  isUpdatingPin = signal<boolean>(false);
  selectedPinnedPersonalityId = signal<string | null>(null);

  // Computed signals
  isUserScoped = computed(() => {
    const level = this.memory()?.level;
    return level === 'global' || level === 'personality';
  });

  private route = inject(ActivatedRoute);
  private router = inject(Router);
  private memoryService = inject(MemoryService);
  private personalityService = inject(PersonalityService);
  private confirmationService = inject(ConfirmationService);

  ngOnInit(): void {
    const memoryId = this.route.snapshot.paramMap.get('id');
    if (memoryId) {
      this.loadMemory(memoryId);
      this.loadPersonalities();
    } else {
      this.error.set('Invalid memory ID');
      this.loading.set(false);
    }
  }

  /**
   * Load memory details by ID
   */
  private loadMemory(id: string): void {
    this.loading.set(true);
    this.error.set(null);

    this.memoryService.getMemoryById(id).subscribe({
      next: (memory: Memory) => {
        this.memory.set(memory);
        this.selectedPinnedPersonalityId.set(memory.pinned_personality_id || null);
        this.loading.set(false);
      },
      error: (error: Error) => {
        this.error.set(error.message);
        this.loading.set(false);
        console.error('Error loading memory:', error);
      }
    });
  }

  /**
   * Load personalities for the pin dropdown
   */
  private loadPersonalities(): void {
    this.isLoadingPersonalities.set(true);

    this.personalityService.listPersonalities(1, 100).subscribe({
      next: (response) => {
        this.personalities.set(response.results);
        this.isLoadingPersonalities.set(false);
      },
      error: (error: Error) => {
        console.error('Error loading personalities:', error);
        this.isLoadingPersonalities.set(false);
      }
    });
  }

  /**
   * Update the pinned personality for the memory
   */
  updatePin(): void {
    const mem = this.memory();
    if (!mem) return;

    this.isUpdatingPin.set(true);
    this.error.set(null);

    this.memoryService.updateMemoryPin(mem.id, this.selectedPinnedPersonalityId()).subscribe({
      next: (updatedMemory: Memory) => {
        this.memory.set(updatedMemory);
        this.isUpdatingPin.set(false);
      },
      error: (error: Error) => {
        this.error.set('Failed to update memory pin: ' + error.message);
        this.isUpdatingPin.set(false);
        console.error('Error updating memory pin:', error);
      }
    });
  }

  /**
   * Get the name of the pinned personality
   */
  getPinnedPersonalityName(): string {
    const mem = this.memory();
    if (!mem?.pinned_personality_id) return 'None (accessible by all)';

    const personality = this.personalities().find(p => p.id === mem.pinned_personality_id);
    return personality?.name || 'Unknown personality';
  }

  /**
   * Navigate back to memories list
   */
  goBack(): void {
    this.router.navigate(['/memory']);
  }

  /**
   * Show delete confirmation modal and handle deletion
   */
  async confirmDelete(): Promise<void> {
    const currentMemory = this.memory();
    if (!currentMemory) return;

    const memoryPreview = currentMemory.content.length > 100
      ? currentMemory.content.substring(0, 100) + '...'
      : currentMemory.content;

    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Memory',
      message: `Are you sure you want to delete this memory? This action cannot be undone and the memory will be permanently removed.\n\n"${memoryPreview}"`,
      type: 'danger',
      confirmText: 'Delete Memory',
      cancelText: 'Cancel',
      keepOpen: true // Keep modal open for async operation
    });

    if (!confirmed) {
      return;
    }

    // Set loading state
    this.confirmationService.setLoading(true, 'Deleting...');

    this.memoryService.deleteMemory(currentMemory.id).subscribe({
      next: () => {
        this.confirmationService.close();
        // Navigate back to memories list with success message
        this.router.navigate(['/memory'], {
          queryParams: { deleted: 'true', memoryId: currentMemory.id }
        });
      },
      error: async (error: Error) => {
        this.error.set(error.message);
        this.confirmationService.setLoading(false);
        this.confirmationService.close();
        console.error('Error deleting memory:', error);
        // Show error alert and await it to ensure it completes
        try {
          await this.confirmationService.alert({
            message: `Failed to delete memory: ${error.message}`,
            type: 'danger'
          });
        } catch (alertError) {
          // Handle any errors in alert flow to prevent inconsistent state
          console.error('Error showing alert:', alertError);
        }
      }
    });
  }

  /**
   * Format date for display
   */
  formatDate(dateString: string): string {
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', {
      weekday: 'long',
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  /**
   * Get scope badge CSS classes
   */
  getLevelBadgeClass(level: string): string {
    switch (level) {
      case 'global':
        return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
      case 'personality':
        return 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-300';
      case 'summary':
        return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-300';
      default:
        return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
    }
  }

  /**
   * Copy memory content to clipboard
   */
  async copyToClipboard(): Promise<void> {
    const currentMemory = this.memory();
    if (!currentMemory) return;

    try {
      await navigator.clipboard.writeText(currentMemory.content);
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
    }
  }
}

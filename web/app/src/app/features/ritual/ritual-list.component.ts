import { Component, inject, signal, OnInit, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { HotkeyInputComponent } from '../../core/components/hotkey-input/hotkey-input.component';
import { AddHotkeyModalComponent } from './add-hotkey-modal/add-hotkey-modal.component';
import { RitualService } from '../../core/services/ritual.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Ritual, RitualFilters, PaginatedRitualResponse, CreateRitualRequest } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';
import { parseHotkeyKeys, formatKeyForDisplay } from '../../core/utils/hotkey.utils';

@Component({
  selector: 'app-ritual-list',
  standalone: true,
  imports: [FormsModule, HotkeyInputComponent, AddHotkeyModalComponent],
  templateUrl: './ritual-list.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./ritual-list.component.scss']
})
export class RitualListComponent implements OnInit {
  private ritualService = inject(RitualService);
  private personalityService = inject(PersonalityService);
  private router = inject(Router);
  private confirmationService = inject(ConfirmationService);

  rituals = signal<Ritual[]>([]);
  systemRituals = signal<Ritual[]>([]);
  personalities = signal<Personality[]>([]);
  isLoading = signal(false);
  totalCount = signal(0);
  currentPage = signal(1);
  pageSize = signal(10);
  successMessage = signal<string | null>(null);

  // Filters
  nameFilter = signal('');
  searchFilter = signal('');
  personalityFilter = signal('');
  minDateFilter = signal('');
  maxDateFilter = signal('');

  // Create ritual form
  isCreateFormOpen = signal(false);
  createForm = signal({
    name: '',
    description: '',
    content: '',
    hotkeys: '',
    personality_id: ''
  });
  isCreating = signal(false);
  createErrorMessage = signal<string | null>(null);
  // Binding modal state
  isBindingModalOpen = signal(false);
  bindingTarget = signal<Ritual | null>(null);
  bindingValue = signal<string>('');
  isBindingSaving = signal(false);
  bindingError = signal<string | null>(null);
  // Reference to imported component to satisfy static analyzer
  // (ensures the standalone component import is treated as used)
  private readonly __addHotkeyModal = AddHotkeyModalComponent;

  ngOnInit(): void {
    this.loadSystemRituals();
    this.loadRituals();
    this.loadPersonalities();
  }

  openBindingModal(ritual: Ritual): void {
    this.bindingTarget.set(ritual);
    this.bindingValue.set(ritual.hotkeys || '');
    this.bindingError.set(null);
    this.isBindingModalOpen.set(true);
    document.body.style.overflow = 'hidden';
  }

  closeBindingModal(): void {
    this.bindingTarget.set(null);
    this.bindingValue.set('');
    this.bindingError.set(null);
    this.isBindingModalOpen.set(false);
    document.body.style.overflow = '';
  }

  saveBinding(): void {
    const ritual = this.bindingTarget();
    if (!ritual) return;
    const hotkeys = this.bindingValue().trim();

    this.isBindingSaving.set(true);
    this.bindingError.set(null);

    this.ritualService.assignSystemRitualHotkey(ritual.id, hotkeys).subscribe({
      next: (res) => {
        this.isBindingSaving.set(false);
        this.showSuccessMessage('Hotkey saved');
        this.closeBindingModal();
        // Refresh system rituals to show updated hotkeys
        this.loadSystemRituals();
      },
      error: (err) => {
        this.isBindingSaving.set(false);
        if (err?.status === 409) {
          this.bindingError.set('Hotkey already in use. Choose another or clear the conflicting binding.');
        } else {
          this.bindingError.set(err?.message || 'Failed to save hotkey. Please try again.');
        }
      }
    });
  }

  private loadSystemRituals(): void {
    this.ritualService.listSystemRituals().subscribe({
      next: (rituals) => {
        this.systemRituals.set(rituals || []);
      },
      error: (error) => {
        console.error('Failed to load system rituals:', error);
        this.systemRituals.set([]);
      }
    });
  }

  loadRituals(): void {
    this.isLoading.set(true);

    const filters: RitualFilters = {};
    if (this.nameFilter()) filters.name = this.nameFilter();
    if (this.searchFilter()) filters.search = this.searchFilter();
    if (this.personalityFilter() && this.personalityFilter() !== 'global') {
      filters.personality_id = this.personalityFilter();
    }
    if (this.minDateFilter()) filters.min_date = this.minDateFilter();
    if (this.maxDateFilter()) filters.max_date = this.maxDateFilter();

    this.ritualService.listRituals(this.currentPage(), this.pageSize(), filters).subscribe({
      next: (response: PaginatedRitualResponse) => {
        this.rituals.set(response.results);
        this.totalCount.set(response.total_count);
        this.isLoading.set(false);
      },
      error: (error) => {
        console.error('Failed to load rituals:', error);
        this.isLoading.set(false);
      }
    });
  }

  loadPersonalities(): void {
    // Load all personalities for the filters and create form
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: (response) => {
        this.personalities.set(response.results);
      },
      error: (error) => {
        console.error('Failed to load personalities:', error);
      }
    });
  }

  onPageChange(page: number): void {
    this.currentPage.set(page);
    this.loadRituals();
  }

  onFiltersApplied(): void {
    this.currentPage.set(1);
    this.loadRituals();
  }

  clearFilters(): void {
    this.nameFilter.set('');
    this.searchFilter.set('');
    this.personalityFilter.set('');
    this.minDateFilter.set('');
    this.maxDateFilter.set('');
    this.onFiltersApplied();
  }

  viewRitual(ritual: Ritual): void {
    this.router.navigate(['/skills', ritual.id]);
  }

  async deleteRitual(ritual: Ritual): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Skill',
      message: `Are you sure you want to delete "${ritual.name}"?`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel'
    });
    if (confirmed) {
      this.ritualService.deleteRitual(ritual.id).subscribe({
        next: () => {
          this.loadRituals();
          this.showSuccessMessage('Skill deleted successfully!');
        },
        error: async (error) => {
          console.error('Failed to delete ritual:', error);
          // Show error alert and await it to ensure it completes
          try {
            await this.confirmationService.alert({
              message: 'Failed to delete skill. Please try again.',
              type: 'danger'
            });
          } catch (alertError) {
            // Handle any errors in alert flow to prevent inconsistent state
            console.error('Error showing alert:', alertError);
          }
        }
      });
    }
  }

  async copyToClipboard(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      this.showSuccessMessage('Copied to clipboard!');
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
      // Show error alert and await it to ensure it completes
      try {
        await this.confirmationService.alert({
          message: 'Failed to copy to clipboard',
          type: 'danger'
        });
      } catch (alertError) {
        // Handle any errors in alert flow to prevent inconsistent state
        console.error('Error showing alert:', alertError);
      }
    }
  }

  openCreateForm(): void {
    this.isCreateFormOpen.set(true);
    this.createForm.set({
      name: '',
      description: '',
      content: '',
      hotkeys: '',
      personality_id: ''
    });
    this.createErrorMessage.set(null);
    // Prevent body scroll
    document.body.style.overflow = 'hidden';
  }

  closeCreateForm(): void {
    this.isCreateFormOpen.set(false);
    this.createForm.set({
      name: '',
      description: '',
      content: '',
      hotkeys: '',
      personality_id: ''
    });
    this.createErrorMessage.set(null);
    // Restore body scroll
    document.body.style.overflow = '';
  }

  createRitual(): void {
    const form = this.createForm();

    if (!form.name.trim() || !form.description.trim() || !form.content.trim()) {
      this.createErrorMessage.set('Name, description, and content are required.');
      return;
    }

    this.isCreating.set(true);
    this.createErrorMessage.set(null);

    const request: CreateRitualRequest = {
      name: form.name.trim(),
      description: form.description.trim(),
      content: form.content.trim(),
      hotkeys: form.hotkeys.trim() || undefined,
      personality_id: form.personality_id || null
    };

    this.ritualService.createRitual(request).subscribe({
      next: (ritual) => {
        this.isCreating.set(false);
        this.closeCreateForm();
        this.loadRituals();
        // Navigate to the new ritual
        this.router.navigate(['/skills', ritual.id]);
      },
      error: (error) => {
        this.isCreating.set(false);
        this.createErrorMessage.set(error.message || 'Failed to create skill. Please try again.');
        console.error('Failed to create ritual:', error);
      }
    });
  }

  getPersonalityName(personalityId: string | null): string | null {
    if (!personalityId) return null;
    const personality = this.personalities().find(p => p.id === personalityId);
    return personality?.name || null;
  }

  showSuccessMessage(message: string): void {
    this.successMessage.set(message);
    setTimeout(() => {
      this.successMessage.set(null);
    }, 3000);
  }

  trackByRitualId(index: number, ritual: Ritual): string {
    return ritual.id;
  }

  getPaginationTo(): number {
    return Math.min(this.currentPage() * this.pageSize(), this.totalCount());
  }

  getPaginationFrom(): number {
    return this.totalCount() > 0 ? (this.currentPage() - 1) * this.pageSize() + 1 : 0;
  }

  getTotalPages(): number {
    return Math.ceil(this.totalCount() / this.pageSize());
  }

  formatDate(dateString: string): string {
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  parseHotkeys(hotkey: string): string[] {
    return parseHotkeyKeys(hotkey);
  }

  isModifierKey(key: string): boolean {
    const lowerKey = key.toLowerCase();
    return ['ctrl', 'alt', 'meta', 'shift', 'cmd', 'win'].includes(lowerKey);
  }

  formatKeyDisplay(key: string): string {
    return formatKeyForDisplay(key);
  }
}

import { Component, OnInit, signal, computed, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { MemoryService } from '../../core/services/memory.service';
import { Memory, MemoryFilters, PaginatedMemoryResponse } from '../../core/models/memory.model';

@Component({
  selector: 'app-memory-list',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './memory-list.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./memory-list.component.scss']
})
export class MemoryListComponent implements OnInit {
  // Signals for reactive state management
  memories = signal<Memory[]>([]);
  loading = signal<boolean>(true);
  error = signal<string | null>(null);
  totalCount = signal<number>(0);
  currentPage = signal<number>(1);
  pageSize = signal<number>(10);

  // Filter state
  searchQuery = signal<string>('');
  selectedLevel = signal<string>('');
  selectedChatId = signal<string>('');
  selectedPersonalityId = signal<string>('');
  startDate = signal<string>('');
  endDate = signal<string>('');

  // Computed values
  totalPages = computed(() => Math.ceil(this.totalCount() / this.pageSize()));
  hasMemories = computed(() => this.memories().length > 0);
  isFirstPage = computed(() => this.currentPage() === 1);
  isLastPage = computed(() => this.currentPage() === this.totalPages());

  // Pagination display range
  pageNumbers = computed(() => {
    const current = this.currentPage();
    const total = this.totalPages();
    const range = [];
    
    const start = Math.max(1, current - 2);
    const end = Math.min(total, current + 2);
    
    for (let i = start; i <= end; i++) {
      range.push(i);
    }
    
    return range;
  });

  constructor(
    private memoryService: MemoryService,
    private router: Router,
    private route: ActivatedRoute,
  ) {}

  ngOnInit(): void {
    this.route.queryParamMap.subscribe(params => {
      const chatId = params.get('chat_id')?.trim() ?? '';
      const level = params.get('level')?.trim() ?? '';
      const personalityId = params.get('personality_id')?.trim() ?? '';
      const query = params.get('query')?.trim() ?? '';
      if (chatId) {
        this.selectedChatId.set(chatId);
      }
      if (level) {
        this.selectedLevel.set(level);
      }
      if (personalityId) {
        this.selectedPersonalityId.set(personalityId);
      }
      if (query) {
        this.searchQuery.set(query);
      }
      this.currentPage.set(1);
      this.loadMemories();
    });
  }

  /**
   * Load memories with current filters and pagination
   */
  loadMemories(): void {
    this.loading.set(true);
    this.error.set(null);

    const filters: MemoryFilters = {};
    
    if (this.searchQuery()) {
      filters.query = this.searchQuery();
    }
    if (this.selectedLevel()) {
      filters.level = this.selectedLevel() as 'global' | 'personality' | 'thread' | 'summary';
    }
    if (this.selectedChatId()) {
      filters.chat_id = this.selectedChatId();
    }
    if (this.selectedPersonalityId() && (this.selectedLevel() === '' || this.selectedLevel() === 'personality')) {
      filters.pinned_personality_id = this.selectedPersonalityId();
    }
    if (this.startDate()) {
      filters.min_date = new Date(this.startDate()).toISOString();
    }
    if (this.endDate()) {
      filters.max_date = new Date(this.endDate()).toISOString();
    }

    this.memoryService.getMemories(this.currentPage(), this.pageSize(), filters)
      .subscribe({
        next: (response: PaginatedMemoryResponse) => {
          this.memories.set(response.results);
          this.totalCount.set(response.total_count);
          this.loading.set(false);
        },
        error: (error: Error) => {
          this.error.set(error.message);
          this.loading.set(false);
          console.error('Error loading memories:', error);
        }
      });
  }

  /**
   * Handle search form submission
   */
  onSearch(): void {
    this.currentPage.set(1); // Reset to first page when searching
    this.loadMemories();
  }

  /**
   * Clear all filters
   */
  clearFilters(): void {
    this.searchQuery.set('');
    this.selectedLevel.set('');
    this.selectedChatId.set('');
    this.selectedPersonalityId.set('');
    this.startDate.set('');
    this.endDate.set('');
    this.currentPage.set(1);
    this.loadMemories();
  }

  /**
   * Navigate to specific page
   */
  goToPage(page: number): void {
    if (page >= 1 && page <= this.totalPages()) {
      this.currentPage.set(page);
      this.loadMemories();
    }
  }

  /**
   * Go to previous page
   */
  previousPage(): void {
    if (!this.isFirstPage()) {
      this.goToPage(this.currentPage() - 1);
    }
  }

  /**
   * Go to next page
   */
  nextPage(): void {
    if (!this.isLastPage()) {
      this.goToPage(this.currentPage() + 1);
    }
  }

  /**
   * Navigate to memory details
   */
  viewMemory(memory: Memory): void {
    this.router.navigate(['/memory', memory.id]);
  }

  /**
   * Format date for display
   */
  formatDate(dateString: string): string {
    const date = new Date(dateString);
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
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
   * Truncate content for preview
   */
  truncateContent(content: string, maxLength: number = 150): string {
    if (content.length <= maxLength) {
      return content;
    }
    return content.substring(0, maxLength) + '...';
  }

  /**
   * TrackBy function for ngFor optimization
   */
  trackByMemoryId(index: number, memory: Memory): string {
    return memory.id;
  }

  /**
   * Calculate the "to" number for pagination display
   */
  getPaginationTo(): number {
    return Math.min(this.currentPage() * this.pageSize(), this.totalCount());
  }

  /**
   * Calculate the "from" number for pagination display
   */
  getPaginationFrom(): number {
    return (this.currentPage() - 1) * this.pageSize() + 1;
  }
}

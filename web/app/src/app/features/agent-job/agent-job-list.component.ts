import { Component, inject, signal, OnDestroy, OnInit, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { AgentJob, AgentJobFilters, AgentJobStatus, AgentJobScheduleType } from '../../core/models/agent-job.model';

@Component({
  selector: 'app-agent-job-list',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './agent-job-list.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./agent-job-list.component.scss']
})
export class AgentJobListComponent implements OnInit, OnDestroy {
  private agentJobService = inject(AgentJobService);
  private chatService = inject(ChatService);
  private router = inject(Router);
  private confirmationService = inject(ConfirmationService);

  jobs = signal<AgentJob[]>([]);
  isLoading = signal(false);
  runningNowJobIDs = signal<Record<string, boolean>>({});
  successMessage = signal<string | null>(null);
  errorMessage = signal<string | null>(null);
  totalCount = signal(0);
  currentPage = signal(1);
  pageSize = signal(10);

  searchFilter = signal('');
  statusFilter = signal<AgentJobStatus | ''>('');
  scheduleTypeFilter = signal<AgentJobScheduleType | ''>('');
  private successMessageTimer: ReturnType<typeof setTimeout> | null = null;

  ngOnInit(): void {
    this.loadJobs();
  }

  ngOnDestroy(): void {
    if (this.successMessageTimer) {
      clearTimeout(this.successMessageTimer);
      this.successMessageTimer = null;
    }
  }

  loadJobs(): void {
    this.isLoading.set(true);

    const filters: AgentJobFilters = {};
    if (this.searchFilter()) filters.search = this.searchFilter();
    if (this.statusFilter()) filters.status = this.statusFilter() as AgentJobStatus;
    if (this.scheduleTypeFilter()) filters.schedule_type = this.scheduleTypeFilter() as AgentJobScheduleType;

    this.agentJobService.listAgentJobs(this.currentPage(), this.pageSize(), filters).subscribe({
      next: (resp) => {
        this.jobs.set(resp.results || []);
        this.totalCount.set(resp.total_count || 0);
        this.isLoading.set(false);
      },
      error: (error) => {
        console.error('Failed to load agent jobs:', error);
        this.isLoading.set(false);
      }
    });
  }

  onFiltersApplied(): void {
    this.currentPage.set(1);
    this.loadJobs();
  }

  clearFilters(): void {
    this.searchFilter.set('');
    this.statusFilter.set('');
    this.scheduleTypeFilter.set('');
    this.onFiltersApplied();
  }

  onPageChange(page: number): void {
    this.currentPage.set(page);
    this.loadJobs();
  }

  viewJob(job: AgentJob): void {
    this.router.navigate(['/agent-jobs', job.id]);
  }

  async togglePause(job: AgentJob): Promise<void> {
    if (job.status !== 'active' && job.status !== 'paused') return;
    const nextStatus: 'active' | 'paused' = job.status === 'active' ? 'paused' : 'active';

    const confirmed = await this.confirmationService.confirm({
      title: nextStatus === 'paused' ? 'Pause Job' : 'Resume Job',
      message: nextStatus === 'paused'
        ? `Pause "${job.title || 'Untitled'}"?`
        : `Resume "${job.title || 'Untitled'}"?`,
      type: 'warning',
      confirmText: nextStatus === 'paused' ? 'Pause' : 'Resume',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.agentJobService.updateAgentJobStatus(job.id, { status: nextStatus }).subscribe({
      next: () => this.loadJobs(),
      error: (error) => {
        console.error('Failed to update job status:', error);
      }
    });
  }

  canRunNow(job: AgentJob): boolean {
    return job.status === 'active' || job.status === 'paused';
  }

  isRunningNow(jobId: string): boolean {
    return !!this.runningNowJobIDs()[jobId];
  }

  runNow(job: AgentJob): void {
    if (!this.canRunNow(job)) return;
    if (this.isRunningNow(job.id)) return;

    this.successMessage.set(null);
    this.errorMessage.set(null);
    this.runningNowJobIDs.update((state) => ({ ...state, [job.id]: true }));

    this.agentJobService.runNow(job.id).subscribe({
      next: () => {
        this.showSuccessMessage(`Triggered "${job.title || 'Untitled'}".`);
        this.clearRunningNow(job.id);
        this.loadJobs();
      },
      error: (error) => {
        console.error('Failed to run job now:', error);
        this.errorMessage.set(this.extractErrorMessage(error, 'Failed to run job now'));
        this.clearRunningNow(job.id);
      }
    });
  }

  private clearRunningNow(jobId: string): void {
    this.runningNowJobIDs.update((state) => {
      if (!state[jobId]) return state;
      const next = { ...state };
      delete next[jobId];
      return next;
    });
  }

  private showSuccessMessage(message: string): void {
    this.successMessage.set(message);
    if (this.successMessageTimer) {
      clearTimeout(this.successMessageTimer);
    }
    this.successMessageTimer = setTimeout(() => {
      this.successMessage.set(null);
      this.successMessageTimer = null;
    }, 4000);
  }

  private extractErrorMessage(error: unknown, fallback: string): string {
    if (!error) return fallback;
    if (typeof error === 'string') return error;
    if (error instanceof Error) return error.message || fallback;

    const errorObj = error as { message?: unknown; error?: unknown; status?: unknown };

    if (typeof errorObj.error === 'string' && errorObj.error.trim() !== '') {
      return errorObj.error;
    }

    if (errorObj.error && typeof errorObj.error === 'object') {
      const nested = errorObj.error as { message?: unknown; error?: unknown };
      if (typeof nested.message === 'string' && nested.message.trim() !== '') {
        return nested.message;
      }
      if (typeof nested.error === 'string' && nested.error.trim() !== '') {
        return nested.error;
      }
    }

    if (typeof errorObj.message === 'string' && errorObj.message.trim() !== '') {
      return errorObj.message;
    }

    if (errorObj.status === 0) {
      return 'Network error: could not reach server';
    }

    return fallback;
  }

  async deleteJob(job: AgentJob): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Job',
      message: `Are you sure you want to delete "${job.title || 'Untitled'}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.agentJobService.deleteAgentJob(job.id).subscribe({
      next: () => this.loadJobs(),
      error: (error) => {
        console.error('Failed to delete job:', error);
      }
    });
  }

  openChat(job: AgentJob): void {
    if (!job.chat_id) return;
    this.chatService.setLastChatId(job.chat_id);
    this.chatService.getChat(job.chat_id).subscribe({
      next: () => this.router.navigate(['/chat']),
      error: (error) => {
        console.error('Failed to open chat:', error);
        this.router.navigate(['/chat']);
      }
    });
  }

  getTotalPages(): number {
    return Math.ceil(this.totalCount() / this.pageSize());
  }

  formatDate(dateString?: string | null): string {
    if (!dateString) return '—';
    const date = new Date(dateString);
    return date.toLocaleString();
  }

  trackByJobId(index: number, job: AgentJob): string {
    return job.id;
  }
}


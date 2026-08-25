
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { JobViewService, JobViewFilters } from '../../core/services/job-view.service';
import { toJobCardVm } from './helpers/job-vm.helpers';
import { JobFilterBarComponent } from './components/job-filter-bar.component';
import { JobCardComponent } from './components/job-card.component';

@Component({
  selector: 'app-jobs-page',
  standalone: true,
  imports: [JobFilterBarComponent, JobCardComponent],
  templateUrl: './jobs-page.component.html',
  styleUrl: './jobs-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobsPageComponent implements OnInit, OnDestroy {
  private readonly view = inject(JobViewService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly router = inject(Router);

  readonly jobs = this.view.jobs;
  readonly loading = this.view.loading;
  readonly error = this.view.error;
  readonly filters = this.view.filters;
  readonly currentPage = this.view.currentPage;
  readonly totalPages = this.view.totalPages;
  readonly totalCount = this.view.totalCount;
  readonly runningNowIds = this.view.runningNowIds;

  readonly cardVms = computed(() => this.jobs().map(job => toJobCardVm(job)));

  onCreateJob(): void {
    void this.router.navigate(['/agent-jobs/new']);
  }

  ngOnInit(): void {
    this.view.load(1);
    this.view.startPollingActiveJobs();
  }

  ngOnDestroy(): void {
    this.view.stopPolling();
  }

  onFiltersChanged(partial: Partial<JobViewFilters>): void {
    this.view.setFilters(partial);
  }

  onClearFilters(): void {
    this.view.clearFilters();
  }

  onOpen(jobId: string): void {
    void this.router.navigate(['/agent-jobs', jobId]);
  }

  async onTogglePauseResume(event: { id: string; nextStatus: 'active' | 'paused' }): Promise<void> {
    const action = event.nextStatus === 'paused' ? 'Pause' : 'Resume';
    const confirmed = await this.confirmation.confirm({
      title: `${action} job`,
      message: `${action} this job now?`,
      type: 'warning',
      confirmText: action,
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.view.updateStatus(event.id, event.nextStatus).subscribe({
      next: updated => {
        this.jobs.update(current => current.map(job => (job.id === updated.id ? updated : job)));
      },
    });
  }

  onRunNow(jobId: string): void {
    this.view.runNow(jobId).subscribe({
      next: () => {
        this.view.startPollingActiveJobs();
      },
    });
  }

  async onDelete(jobId: string): Promise<void> {
    const confirmed = await this.confirmation.confirm({
      title: 'Delete job',
      message: 'Delete this job? This action cannot be undone.',
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.view.deleteOne(jobId).subscribe({
      next: () => this.view.load(this.currentPage()),
    });
  }

  onPageChange(page: number): void {
    if (page < 1 || page > this.totalPages()) return;
    this.view.load(page);
  }
}

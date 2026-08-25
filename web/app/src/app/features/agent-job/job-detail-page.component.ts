
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { forkJoin, of } from 'rxjs';
import { catchError, map } from 'rxjs/operators';

import {
  AgentJob,
  AgentJobScheduleParseRequest,
  AgentJobSchedulePreview,
  AgentJobStatus,
  CreateAgentJobRequest,
  UpdateAgentJobRequest,
} from '../../core/models/agent-job.model';
import { Chat } from '../../core/models/chat.model';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import { Ritual } from '../../core/models/ritual.model';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { isTerminalStatus } from './helpers/job-status.helpers';
import { JobFormComponent } from './components/job-form.component';
import { JobRunHistoryComponent } from './components/job-run-history.component';

@Component({
  selector: 'app-job-detail-page',
  standalone: true,
  imports: [FormsModule, JobFormComponent, JobRunHistoryComponent],
  templateUrl: './job-detail-page.component.html',
  styleUrl: './job-detail-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobDetailPageComponent implements OnInit, OnDestroy {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly agentJobService = inject(AgentJobService);
  private readonly chatService = inject(ChatService);
  private readonly modelService = inject(ModelService);
  private readonly personalityService = inject(PersonalityService);
  private readonly ritualService = inject(RitualService);
  private readonly confirmation = inject(ConfirmationService);

  private schedulePreviewTimer: ReturnType<typeof setTimeout> | null = null;
  private pollTimer: ReturnType<typeof setInterval> | null = null;

  readonly job = signal<AgentJob | null>(null);
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly runningNow = signal(false);
  readonly previewLoading = signal(false);
  readonly preview = signal<AgentJobSchedulePreview | null>(null);
  readonly error = signal<string | null>(null);

  readonly chats = signal<Chat[]>([]);
  readonly personalities = signal<Personality[]>([]);
  readonly models = signal<Model[]>([]);

  readonly attachedRituals = signal<Ritual[]>([]);
  readonly ritualOptions = signal<Ritual[]>([]);
  readonly ritualSearch = signal('');
  readonly ritualLoading = signal(false);
  readonly isCreateMode = signal(false);

  ngOnInit(): void {
    const id = this.route.snapshot.paramMap.get('id');
    if (!id) {
      void this.router.navigate(['/agent-jobs']);
      return;
    }
    if (id === 'new') {
      this.isCreateMode.set(true);
      this.loadCreatePage();
      return;
    }
    this.loadPage(id);
  }

  ngOnDestroy(): void {
    this.clearSchedulePreviewTimer();
    this.stopPolling();
  }

  private loadCreatePage(): void {
    this.loading.set(true);
    this.error.set(null);
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    this.job.set({
      id: 'new',
      user_id: '',
      prompt: '',
      schedule_input: '',
      schedule_type: 'at',
      timezone,
      status: 'active',
      run_count: 0,
      created_at: '',
      updated_at: '',
    });
    forkJoin({
      chats: this.chatService.listAllChats(200).pipe(
        catchError(() => of({ chats: [], truncated: false })),
        map(result => ({ results: result.chats })),
      ),
      personalities: this.personalityService.listPersonalities(1, 200).pipe(catchError(() => of({ results: [] } as any))),
      models: this.modelService.getModels().pipe(catchError(() => of([]))),
    }).subscribe({
      next: result => {
        this.chats.set(result.chats.results ?? []);
        this.personalities.set(result.personalities.results ?? []);
        this.models.set(result.models ?? []);
        this.loading.set(false);
      },
      error: error => {
        this.loading.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to load job form');
      },
    });
  }

  private loadPage(id: string): void {
    this.loading.set(true);
    this.error.set(null);
    forkJoin({
      job: this.agentJobService.getAgentJob(id),
      chats: this.chatService.listAllChats(200).pipe(
        catchError(() => of({ chats: [], truncated: false })),
        map(result => ({ results: result.chats })),
      ),
      personalities: this.personalityService.listPersonalities(1, 200).pipe(catchError(() => of({ results: [] } as any))),
      models: this.modelService.getModels().pipe(catchError(() => of([]))),
    }).subscribe({
      next: result => {
        this.job.set(result.job);
        this.chats.set(result.chats.results ?? []);
        this.personalities.set(result.personalities.results ?? []);
        this.models.set(result.models ?? []);
        this.attachedRituals.set(result.job.rituals ?? []);
        this.loading.set(false);
        this.searchRituals('');
        this.startPollingIfNeeded(result.job.status);
      },
      error: error => {
        this.loading.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to load job');
      },
    });
  }

  goBack(): void {
    void this.router.navigate(['/agent-jobs']);
  }

  openChat(): void {
    const current = this.job();
    if (!current?.chat_id) return;
    this.chatService.setLastChatId(current.chat_id);
    void this.router.navigate(['/chat']);
  }

  save(payload: UpdateAgentJobRequest): void {
    if (this.isCreateMode()) {
      this.create(payload);
      return;
    }
    const current = this.job();
    if (!current) return;
    this.saving.set(true);
    this.error.set(null);
    this.agentJobService.updateAgentJob(current.id, payload).subscribe({
      next: updated => {
        this.job.set(updated);
        this.attachedRituals.set(updated.rituals ?? this.attachedRituals());
        this.saving.set(false);
      },
      error: error => {
        this.saving.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to save job');
      },
    });
  }

  private create(payload: UpdateAgentJobRequest): void {
    const prompt = (payload.prompt || '').trim();
    const scheduleInput = (payload.schedule_input || '').trim();
    if (!prompt || !scheduleInput) {
      this.error.set('Prompt and schedule are required');
      return;
    }

    const request: CreateAgentJobRequest = {
      prompt,
      schedule_input: scheduleInput,
      timezone: payload.timezone ?? this.job()?.timezone ?? 'UTC',
    };
    const title = (payload.title ?? '').trim();
    if (title) request.title = title;
    const chatId = (payload.chat_id ?? '').trim();
    if (chatId) request.chat_id = chatId;
    const personalityId = (payload.personality_id ?? '').trim();
    if (personalityId) request.personality_id = personalityId;
    const modelId = (payload.model_id ?? '').trim();
    if (modelId) request.model_id = modelId;

    this.saving.set(true);
    this.error.set(null);
    this.agentJobService.createAgentJob(request).subscribe({
      next: () => {
        this.saving.set(false);
        void this.router.navigate(['/agent-jobs']);
      },
      error: error => {
        this.saving.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to create job');
      },
    });
  }

  onScheduleEdited(request: AgentJobScheduleParseRequest): void {
    this.clearSchedulePreviewTimer();
    this.schedulePreviewTimer = setTimeout(() => {
      this.requestPreview(request);
    }, 300);
  }

  requestPreview(request: AgentJobScheduleParseRequest): void {
    if (!request.schedule_input.trim()) {
      this.preview.set(null);
      return;
    }
    this.previewLoading.set(true);
    this.agentJobService.parseSchedule(request).subscribe({
      next: preview => {
        this.preview.set(preview);
        this.previewLoading.set(false);
      },
      error: () => {
        this.previewLoading.set(false);
      },
    });
  }

  async togglePauseResume(): Promise<void> {
    const current = this.job();
    if (!current) return;
    if (current.status !== 'active' && current.status !== 'paused') return;
    const nextStatus: 'active' | 'paused' = current.status === 'active' ? 'paused' : 'active';
    const confirmed = await this.confirmation.confirm({
      title: nextStatus === 'paused' ? 'Pause job' : 'Resume job',
      message: `Do you want to ${nextStatus === 'paused' ? 'pause' : 'resume'} this job?`,
      type: 'warning',
      confirmText: nextStatus === 'paused' ? 'Pause' : 'Resume',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.agentJobService.updateAgentJobStatus(current.id, { status: nextStatus }).subscribe({
      next: updated => {
        this.job.set(updated);
        this.startPollingIfNeeded(updated.status);
      },
      error: error => {
        this.error.set(error instanceof Error ? error.message : 'Failed to update job status');
      },
    });
  }

  runNow(): void {
    const current = this.job();
    if (!current) return;
    this.runningNow.set(true);
    this.agentJobService.runNow(current.id).subscribe({
      next: () => {
        this.runningNow.set(false);
        this.startPollingIfNeeded(current.status);
      },
      error: error => {
        this.runningNow.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to trigger job');
      },
    });
  }

  async deleteJob(): Promise<void> {
    const current = this.job();
    if (!current) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Delete job',
      message: `Delete "${current.title || 'Untitled job'}"?`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.agentJobService.deleteAgentJob(current.id).subscribe({
      next: () => void this.router.navigate(['/agent-jobs']),
      error: error => this.error.set(error instanceof Error ? error.message : 'Failed to delete job'),
    });
  }

  searchRituals(query: string): void {
    this.ritualSearch.set(query);
    this.ritualLoading.set(true);
    this.ritualService.listRituals(1, 50, { search: query.trim() || undefined }).subscribe({
      next: response => {
        const attached = new Set(this.attachedRituals().map(ritual => ritual.id));
        this.ritualOptions.set((response.results ?? []).filter(ritual => !attached.has(ritual.id)));
        this.ritualLoading.set(false);
      },
      error: () => {
        this.ritualOptions.set([]);
        this.ritualLoading.set(false);
      },
    });
  }

  addRitual(ritual: Ritual): void {
    const current = this.job();
    if (!current) return;
    this.agentJobService.addRitual(current.id, ritual.id).subscribe({
      next: () => {
        this.attachedRituals.update(items => [...items, ritual]);
        this.ritualOptions.update(items => items.filter(item => item.id !== ritual.id));
      },
    });
  }

  removeRitual(ritual: Ritual): void {
    const current = this.job();
    if (!current) return;
    this.agentJobService.removeRitual(current.id, ritual.id).subscribe({
      next: () => {
        this.attachedRituals.update(items => items.filter(item => item.id !== ritual.id));
      },
    });
  }

  private startPollingIfNeeded(status: AgentJobStatus): void {
    this.stopPolling();
    if (isTerminalStatus(status)) return;
    const current = this.job();
    if (!current) return;
    this.pollTimer = setInterval(() => {
      this.agentJobService.getAgentJob(current.id).subscribe({
        next: updated => {
          this.job.set(updated);
          this.attachedRituals.set(updated.rituals ?? this.attachedRituals());
          if (isTerminalStatus(updated.status)) {
            this.stopPolling();
          }
        },
      });
    }, 5000);
  }

  private stopPolling(): void {
    if (!this.pollTimer) return;
    clearInterval(this.pollTimer);
    this.pollTimer = null;
  }

  private clearSchedulePreviewTimer(): void {
    if (!this.schedulePreviewTimer) return;
    clearTimeout(this.schedulePreviewTimer);
    this.schedulePreviewTimer = null;
  }
}

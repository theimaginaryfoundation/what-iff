import { Component, DestroyRef, ElementRef, HostListener, OnDestroy, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { take } from 'rxjs/operators';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { AgentJob, AgentJobSchedulePreview } from '../../core/models/agent-job.model';
import { Chat } from '../../core/models/chat.model';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import { Ritual } from '../../core/models/ritual.model';

@Component({
  selector: 'app-agent-job-detail',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './agent-job-detail.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./agent-job-detail.component.scss']
})
export class AgentJobDetailComponent implements OnInit, OnDestroy {
  private route = inject(ActivatedRoute);
  private router = inject(Router);
  private agentJobService = inject(AgentJobService);
  private chatService = inject(ChatService);
  private modelService = inject(ModelService);
  private personalityService = inject(PersonalityService);
  private ritualService = inject(RitualService);
  private confirmationService = inject(ConfirmationService);
  private destroyRef = inject(DestroyRef);
  private elementRef = inject(ElementRef<HTMLElement>);
  private chatSearchDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly chatSearchDebounceMs = 300;
  private readonly chatSearchMinChars = 3;

  jobId = signal<string>('');
  job = signal<AgentJob | null>(null);
  isLoading = signal(false);
  isLoadingChats = signal(false);
  isSavingTitle = signal(false);
  isSavingPrompt = signal(false);
  isSavingChatAssociation = signal(false);
  isSavingSchedule = signal(false);
  isRunningNow = signal(false);
  isPreviewing = signal(false);
  preview = signal<AgentJobSchedulePreview | null>(null);
  errorMessage = signal<string | null>(null);
  successMessage = signal<string | null>(null);

  editTitle = signal('');
  editPrompt = signal('');
  isChatDropdownOpen = signal(false);
  chatSearch = signal('');
  selectedChatId = signal('');
  selectedChat = signal<Chat | null>(null);
  chatOptions = signal<Chat[]>([]);
  editScheduleInput = signal('');
  editTimezone = signal('');
  editPersonalityId = signal('');
  editModelId = signal('');
  isSavingOverrides = signal(false);
  models = signal<Model[]>([]);
  personalities = signal<Personality[]>([]);

  // Attached skills (rituals)
  attachedRituals = signal<Ritual[]>([]);
  isRitualDropdownOpen = signal(false);
  ritualSearch = signal('');
  ritualOptions = signal<Ritual[]>([]);
  isLoadingRituals = signal(false);
  isAddingRitual = signal(false);
  ritualAddError = signal<string | null>(null);
  private ritualSearchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

  /** Inline confirmation under prompt / chat / overrides save buttons */
  promptSaveFeedback = signal<string | null>(null);
  chatSaveFeedback = signal<string | null>(null);
  overridesSaveFeedback = signal<string | null>(null);

  private promptFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
  private chatFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
  private overridesFeedbackTimer: ReturnType<typeof setTimeout> | null = null;

  ngOnInit(): void {
    const id = this.route.snapshot.paramMap.get('id');
    if (!id) {
      this.router.navigate(['/agent-jobs']);
      return;
    }
    this.jobId.set(id);
    this.loadModelAndPersonalityOptions();
    this.loadJob();
  }

  private loadModelAndPersonalityOptions(): void {
    this.modelService.getModels().pipe(take(1), takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (models) => this.models.set(models),
      error: (e) => console.error('Failed to load models for job overrides:', e)
    });
    this.personalityService.listPersonalities(1, 200).pipe(take(1), takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (res) => this.personalities.set(res.results),
      error: (e) => console.error('Failed to load personalities for job overrides:', e)
    });
  }

  ngOnDestroy(): void {
    if (this.chatSearchDebounceTimer) {
      clearTimeout(this.chatSearchDebounceTimer);
      this.chatSearchDebounceTimer = null;
    }
    if (this.ritualSearchDebounceTimer) {
      clearTimeout(this.ritualSearchDebounceTimer);
      this.ritualSearchDebounceTimer = null;
    }
    this.clearSaveFeedbackTimers();
  }

  private clearSaveFeedbackTimers(): void {
    if (this.promptFeedbackTimer) {
      clearTimeout(this.promptFeedbackTimer);
      this.promptFeedbackTimer = null;
    }
    if (this.chatFeedbackTimer) {
      clearTimeout(this.chatFeedbackTimer);
      this.chatFeedbackTimer = null;
    }
    if (this.overridesFeedbackTimer) {
      clearTimeout(this.overridesFeedbackTimer);
      this.overridesFeedbackTimer = null;
    }
  }

  private flashPromptSaved(): void {
    if (this.promptFeedbackTimer) {
      clearTimeout(this.promptFeedbackTimer);
    }
    this.promptSaveFeedback.set('Saved.');
    this.promptFeedbackTimer = setTimeout(() => {
      this.promptSaveFeedback.set(null);
      this.promptFeedbackTimer = null;
    }, 4000);
  }

  private flashChatSaved(): void {
    if (this.chatFeedbackTimer) {
      clearTimeout(this.chatFeedbackTimer);
    }
    this.chatSaveFeedback.set('Saved.');
    this.chatFeedbackTimer = setTimeout(() => {
      this.chatSaveFeedback.set(null);
      this.chatFeedbackTimer = null;
    }, 4000);
  }

  private flashOverridesSaved(): void {
    if (this.overridesFeedbackTimer) {
      clearTimeout(this.overridesFeedbackTimer);
    }
    this.overridesSaveFeedback.set('Saved.');
    this.overridesFeedbackTimer = setTimeout(() => {
      this.overridesSaveFeedback.set(null);
      this.overridesFeedbackTimer = null;
    }, 4000);
  }

  loadJob(): void {
    this.isLoading.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);

    this.agentJobService.getAgentJob(this.jobId()).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.editTitle.set(job.title || '');
        this.editPrompt.set(job.prompt || '');
        this.editScheduleInput.set(job.schedule_input || '');
        this.editTimezone.set(job.timezone || 'UTC');
        this.editPersonalityId.set(job.personality_id || '');
        this.editModelId.set(job.model_id || '');
        this.initializeChatSelector(job.chat_id);
        this.attachedRituals.set(job.rituals ?? []);
        this.isLoading.set(false);
      },
      error: (error) => {
        console.error('Failed to load agent job:', error);
        this.errorMessage.set(error.message || 'Failed to load job');
        this.isLoading.set(false);
      }
    });
  }

  goBack(): void {
    this.router.navigate(['/agent-jobs']);
  }

  initializeChatSelector(chatId?: string | null): void {
    this.selectedChatId.set(chatId || '');
    this.selectedChat.set(null);
    this.loadChatsForSelector();

    if (!chatId) {
      return;
    }

    this.chatService.getChat(chatId).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (chat) => {
        this.selectedChat.set(chat);
        this.chatOptions.set(this.mergeAndDedupeChats(this.chatOptions(), chat));
      },
      error: (error) => {
        console.error('Failed to load selected chat:', error);
      }
    });
  }

  onChatSearchInput(value: string): void {
    this.chatSearch.set(value);
    this.scheduleChatSearch();
  }

  toggleChatDropdown(): void {
    this.isChatDropdownOpen.set(!this.isChatDropdownOpen());
    if (this.isChatDropdownOpen()) {
      this.loadChatsForSelector();
    }
  }

  closeChatDropdown(): void {
    this.isChatDropdownOpen.set(false);
  }

  selectChatOption(chat: Chat | null): void {
    if (chat) {
      this.selectedChatId.set(chat.id);
      this.selectedChat.set(chat);
      this.chatOptions.set(this.mergeAndDedupeChats(this.chatOptions(), chat));
    } else {
      this.selectedChatId.set('');
      this.selectedChat.set(null);
    }
    this.closeChatDropdown();
  }

  getSelectedChatLabel(): string {
    const selectedId = this.selectedChatId();
    if (!selectedId) return 'Select a chat';
    const selected = this.selectedChat() || this.chatOptions().find((chat) => chat.id === selectedId);
    return selected?.name || 'Select a chat';
  }

  showChatSearchHint(): boolean {
    const trimmed = this.chatSearch().trim();
    return trimmed.length > 0 && trimmed.length < this.chatSearchMinChars;
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    if (!this.isChatDropdownOpen()) return;
    const target = event.target as Node | null;
    if (!target) return;
    if (!this.elementRef.nativeElement.contains(target)) {
      this.closeChatDropdown();
    }
  }

  private scheduleChatSearch(): void {
    if (this.chatSearchDebounceTimer) {
      clearTimeout(this.chatSearchDebounceTimer);
      this.chatSearchDebounceTimer = null;
    }
    this.chatSearchDebounceTimer = setTimeout(() => {
      this.loadChatsForSelector();
    }, this.chatSearchDebounceMs);
  }

  loadChatsForSelector(): void {
    const search = this.chatSearch().trim();
    if (search !== '' && search.length < this.chatSearchMinChars) {
      this.isLoadingChats.set(false);
      this.chatOptions.set(this.mergeAndDedupeChats([], this.selectedChat()));
      return;
    }

    this.isLoadingChats.set(true);
    const filters = search ? { name: search } : undefined;

    this.chatService.listChats(1, 50, filters).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (response) => {
        this.chatOptions.set(this.mergeAndDedupeChats(response.results, this.selectedChat()));
        this.isLoadingChats.set(false);
      },
      error: (error) => {
        console.error('Failed to load chats for selector:', error);
        this.errorMessage.set(error.message || 'Failed to load chats');
        this.isLoadingChats.set(false);
      }
    });
  }

  saveChatAssociation(): void {
    this.isSavingChatAssociation.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.chatSaveFeedback.set(null);

    const chatId = this.selectedChatId().trim();
    const payloadChatID = chatId || '';

    this.agentJobService.updateAgentJob(this.jobId(), { chat_id: payloadChatID }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.selectedChatId.set(job.chat_id || '');
        if (!job.chat_id) {
          this.selectedChat.set(null);
        }
        this.flashChatSaved();
        this.isSavingChatAssociation.set(false);
      },
      error: (error) => {
        console.error('Failed to save chat association:', error);
        this.errorMessage.set(error.message || 'Failed to save chat association');
        this.isSavingChatAssociation.set(false);
      }
    });
  }

  private mergeAndDedupeChats(chats: Chat[], selected: Chat | null): Chat[] {
    const merged = selected ? [selected, ...chats] : chats;
    const seen = new Set<string>();
    return merged.filter((chat) => {
      if (seen.has(chat.id)) return false;
      seen.add(chat.id);
      return true;
    });
  }

  openChat(): void {
    const job = this.job();
    if (!job?.chat_id) return;
    this.chatService.setLastChatId(job.chat_id);
    this.chatService.getChat(job.chat_id).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: () => this.router.navigate(['/chat']),
      error: () => this.router.navigate(['/chat'])
    });
  }

  previewSchedule(): void {
    const scheduleInput = this.editScheduleInput().trim();
    const timezone = this.editTimezone().trim() || 'UTC';
    if (!scheduleInput) {
      this.errorMessage.set('Schedule input is required to preview.');
      return;
    }

    this.isPreviewing.set(true);
    this.preview.set(null);
    this.errorMessage.set(null);

    this.agentJobService.parseSchedule({ schedule_input: scheduleInput, timezone }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (preview) => {
        this.preview.set(preview);
        this.isPreviewing.set(false);
      },
      error: (error) => {
        console.error('Failed to preview schedule:', error);
        this.errorMessage.set(error.message || 'Failed to preview schedule');
        this.isPreviewing.set(false);
      }
    });
  }

  saveTitle(): void {
    this.isSavingTitle.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);

    const title = this.editTitle().trim();
    this.agentJobService.updateAgentJob(this.jobId(), { title: title }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.isSavingTitle.set(false);
        this.successMessage.set('Saved.');
      },
      error: (error) => {
        console.error('Failed to save title:', error);
        this.errorMessage.set(error.message || 'Failed to save title');
        this.isSavingTitle.set(false);
      }
    });
  }

  savePrompt(): void {
    const prompt = this.editPrompt().trim();
    if (!prompt) {
      this.errorMessage.set('Prompt is required.');
      return;
    }

    this.isSavingPrompt.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.promptSaveFeedback.set(null);

    this.agentJobService.updateAgentJob(this.jobId(), { prompt }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.editPrompt.set(job.prompt || '');
        this.isSavingPrompt.set(false);
        this.flashPromptSaved();
      },
      error: (error) => {
        console.error('Failed to save prompt:', error);
        this.errorMessage.set(error.message || 'Failed to save prompt');
        this.isSavingPrompt.set(false);
      }
    });
  }

  saveSchedule(): void {
    const scheduleInput = this.editScheduleInput().trim();
    const timezone = this.editTimezone().trim() || 'UTC';
    if (!scheduleInput) {
      this.errorMessage.set('Schedule input is required.');
      return;
    }

    this.isSavingSchedule.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);

    this.agentJobService.updateAgentJob(this.jobId(), { schedule_input: scheduleInput, timezone }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.isSavingSchedule.set(false);
        this.successMessage.set('Schedule updated.');
      },
      error: (error) => {
        console.error('Failed to save schedule:', error);
        this.errorMessage.set(error.message || 'Failed to save schedule');
        this.isSavingSchedule.set(false);
      }
    });
  }

  saveOverrides(): void {
    this.isSavingOverrides.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.overridesSaveFeedback.set(null);
    this.agentJobService.updateAgentJob(this.jobId(), {
      personality_id: this.editPersonalityId().trim() || '',
      model_id: this.editModelId().trim() || ''
    }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
        this.editPersonalityId.set(job.personality_id || '');
        this.editModelId.set(job.model_id || '');
        this.isSavingOverrides.set(false);
        this.flashOverridesSaved();
      },
      error: (error) => {
        console.error('Failed to save overrides:', error);
        this.errorMessage.set(error.message || 'Failed to save overrides');
        this.isSavingOverrides.set(false);
      }
    });
  }

  async togglePause(): Promise<void> {
    const job = this.job();
    if (!job) return;
    if (job.status !== 'active' && job.status !== 'paused') return;

    const nextStatus: 'active' | 'paused' = job.status === 'active' ? 'paused' : 'active';
    const confirmed = await this.confirmationService.confirm({
      title: nextStatus === 'paused' ? 'Pause Job' : 'Resume Job',
      message: nextStatus === 'paused' ? 'Pause this job?' : 'Resume this job?',
      type: 'warning',
      confirmText: nextStatus === 'paused' ? 'Pause' : 'Resume',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.agentJobService.updateAgentJobStatus(this.jobId(), { status: nextStatus }).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (updated) => this.job.set(updated),
      error: (error) => {
        console.error('Failed to update status:', error);
        this.errorMessage.set(error.message || 'Failed to update status');
      }
    });
  }

  canRunNow(): boolean {
    const status = this.job()?.status;
    return status === 'active' || status === 'paused';
  }

  runNow(): void {
    if (!this.canRunNow()) return;

    this.isRunningNow.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);

    this.agentJobService.runNow(this.jobId()).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: () => {
        this.successMessage.set('Job run triggered.');
        this.isRunningNow.set(false);
        if (this.job()?.chat_id) {
          this.openChat();
          return;
        }
        this.refreshJobAfterRunNow();
      },
      error: (error) => {
        console.error('Failed to run job now:', error);
        this.errorMessage.set(error.message || 'Failed to run job now');
        this.isRunningNow.set(false);
      }
    });
  }

  private refreshJobAfterRunNow(): void {
    this.agentJobService.getAgentJob(this.jobId()).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (job) => {
        this.job.set(job);
      },
      error: (error) => {
        console.error('Failed to refresh agent job after run now:', error);
      }
    });
  }

  async deleteJob(): Promise<void> {
    const job = this.job();
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Job',
      message: `Delete "${job?.title || 'Untitled'}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.agentJobService.deleteAgentJob(this.jobId()).pipe(
      take(1),
      takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: () => this.router.navigate(['/agent-jobs']),
      error: (error) => {
        console.error('Failed to delete job:', error);
        this.errorMessage.set(error.message || 'Failed to delete job');
      }
    });
  }

  // ── Ritual / Skills management ───────────────────────────────────────────

  toggleRitualDropdown(): void {
    const newState = !this.isRitualDropdownOpen();
    this.isRitualDropdownOpen.set(newState);
    if (newState && this.ritualSearch() === '') {
      this.searchRituals('');
    }
  }

  closeRitualDropdown(): void {
    this.isRitualDropdownOpen.set(false);
  }

  onRitualSearchChange(value: string): void {
    this.ritualSearch.set(value);
    if (this.ritualSearchDebounceTimer) clearTimeout(this.ritualSearchDebounceTimer);
    this.ritualSearchDebounceTimer = setTimeout(() => this.searchRituals(value), 300);
  }

  private searchRituals(query: string): void {
    const normalized = query.trim();
    this.isLoadingRituals.set(true);
    this.ritualService.listRituals(1, 50, { search: normalized || undefined }).pipe(
      take(1), takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: (res) => {
        // Drop stale responses when the user typed ahead or a slower request finishes last.
        if (this.ritualSearch().trim() !== normalized) {
          return;
        }
        const attachedIds = new Set(this.attachedRituals().map(r => r.id));
        this.ritualOptions.set(res.results.filter(r => !attachedIds.has(r.id)));
        this.isLoadingRituals.set(false);
      },
      error: () => {
        if (this.ritualSearch().trim() === normalized) {
          this.isLoadingRituals.set(false);
        }
      }
    });
  }

  addRitual(ritual: Ritual): void {
    this.isAddingRitual.set(true);
    this.ritualAddError.set(null);
    this.agentJobService.addRitual(this.jobId(), ritual.id).pipe(
      take(1), takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: () => {
        this.attachedRituals.update(list => [...list, ritual]);
        this.ritualOptions.update(opts => opts.filter(r => r.id !== ritual.id));
        this.isAddingRitual.set(false);
      },
      error: (err) => {
        this.ritualAddError.set(err.message || 'Failed to add skill');
        this.isAddingRitual.set(false);
      }
    });
  }

  removeRitual(ritual: Ritual): void {
    this.agentJobService.removeRitual(this.jobId(), ritual.id).pipe(
      take(1), takeUntilDestroyed(this.destroyRef)
    ).subscribe({
      next: () => {
        this.attachedRituals.update(list => list.filter(r => r.id !== ritual.id));
      },
      error: (err) => {
        console.error('Failed to remove skill', err);
      }
    });
  }

  formatDate(dateString?: string | null): string {
    if (!dateString) return '—';
    return new Date(dateString).toLocaleString();
  }
}

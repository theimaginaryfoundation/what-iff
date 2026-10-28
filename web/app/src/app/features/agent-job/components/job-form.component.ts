
import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { AgentJob, AgentJobScheduleParseRequest, AgentJobSchedulePreview, UpdateAgentJobRequest } from '../../../core/models/agent-job.model';
import { Chat } from '../../../core/models/chat.model';
import { CurrentTimezoneService } from '../../../core/services/current-timezone.service';
import { Model } from '../../../core/models/model.model';
import { Personality } from '../../../core/models/personality.model';
import {
  AGENT_LOOP_DESCRIPTIONS,
  AGENT_LOOP_OPTIONS,
  AgentLoopUpdate,
  REASONING_EFFORT_DESCRIPTION,
  REASONING_EFFORT_OPTIONS,
  ReasoningEffortUpdate,
} from '../../../core/models/turn-settings.model';
import { HelpHintComponent } from '../../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/**
 * The form's values as last hydrated from the job, normalized the way the dirty checks compare
 * them (trimmed strings, timezone defaulting to UTC, `''` for an unset turn setting). Compared
 * against the live signals to tell local draft edits apart from a server refresh.
 */
interface JobFormSnapshot {
  title: string;
  prompt: string;
  scheduleInput: string;
  timezone: string;
  chatId: string;
  personalityId: string;
  modelId: string;
  reasoningEffort: ReasoningEffortUpdate;
  agentLoop: AgentLoopUpdate;
  followUserTimezone: boolean;
}

@Component({
  selector: 'app-job-form',
  standalone: true,
  imports: [FormsModule, HelpHintComponent, TooltipDirective],
  templateUrl: './job-form.component.html',
  styleUrl: './job-form.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobFormComponent {
  readonly job = input.required<AgentJob>();
  readonly createMode = input(false);
  readonly chats = input<Chat[]>([]);
  readonly personalities = input<Personality[]>([]);
  readonly models = input<Model[]>([]);
  readonly saving = input(false);
  readonly preview = input<AgentJobSchedulePreview | null>(null);
  readonly previewLoading = input(false);

  readonly save = output<UpdateAgentJobRequest>();
  readonly previewRequested = output<AgentJobScheduleParseRequest>();
  readonly scheduleEdited = output<AgentJobScheduleParseRequest>();

  readonly title = signal('');
  readonly prompt = signal('');
  readonly scheduleInput = signal('');
  readonly timezone = signal('UTC');
  readonly followUserTimezone = signal(false);
  private readonly currentTimezone = inject(CurrentTimezoneService);
  readonly timezoneFollowingPaused = this.currentTimezone.followingPaused;
  readonly resumeError = this.currentTimezone.resumeError;
  /** Following only changes recurring schedules (a one-off run is a fixed moment). */
  readonly isRecurring = computed(() => (this.preview()?.schedule_type ?? this.job().schedule_type) === 'cron');
  readonly chatId = signal('');
  readonly personalityId = signal('');
  readonly modelId = signal('');
  /** Turn settings for this job's runs (ADR 0x026); `''` = use the thread's (and its mode's). */
  readonly reasoningEffort = signal<ReasoningEffortUpdate>('');
  readonly agentLoop = signal<AgentLoopUpdate>('');
  readonly reasoningEffortOptions = REASONING_EFFORT_OPTIONS;
  readonly reasoningEffortDescription = REASONING_EFFORT_DESCRIPTION;
  readonly agentLoopOptions = AGENT_LOOP_OPTIONS;
  readonly agentLoopDescriptions = AGENT_LOOP_DESCRIPTIONS;
  private hydratedJobId: string | null = null;
  private hydratedJobUpdatedAt: string | null = null;
  private hydratedFormSnapshot: JobFormSnapshot | null = null;
  // Reflect local draft edits without rehydrating on poll refreshes for the same job.
  readonly hasUnsavedEdits = computed(() => {
    const current = this.job();
    if (!current) return false;
    return (
      this.normalize(this.title()) !== this.normalize(current.title) ||
      this.normalize(this.prompt()) !== this.normalize(current.prompt) ||
      this.normalize(this.scheduleInput()) !== this.normalize(current.schedule_input) ||
      this.normalizeTimezone(this.timezone()) !== this.normalizeTimezone(current.timezone) ||
      this.followUserTimezone() !== !!current.follow_user_timezone ||
      this.normalize(this.chatId()) !== this.normalize(current.chat_id) ||
      this.normalize(this.personalityId()) !== this.normalize(current.personality_id) ||
      this.normalize(this.modelId()) !== this.normalize(current.model_id) ||
      this.reasoningEffort() !== (current.reasoning_effort ?? '') ||
      this.agentLoop() !== (current.agent_loop ?? '')
    );
  });
  // The timezone a following job runs in right now, when it is not the job's own.
  readonly travelTimezone = computed(() => {
    const current = this.job();
    const effective = this.normalize(current?.effective_timezone);
    if (!current?.follow_user_timezone || !effective) return null;
    return effective === this.normalizeTimezone(current.timezone) ? null : effective;
  });

  ngOnChanges(): void {
    const current = this.job();
    if (!current) {
      this.hydratedJobId = null;
      this.hydratedJobUpdatedAt = null;
      return;
    }
    if (this.hydratedJobId === null) {
      this.hydrateFromJob(current);
      return;
    }
    if (this.hydratedJobId !== current.id) {
      this.hydrateFromJob(current);
      return;
    }
    if ((this.hydratedJobUpdatedAt || '') === (current.updated_at || '')) {
      return;
    }
    if (this.matchesCurrentJob(current)) {
      this.hydrateFromJob(current);
      return;
    }
    if (this.hasLocalDraftEdits()) {
      return;
    }
    this.hydrateFromJob(current);
  }

  onScheduleChange(value: string): void {
    this.scheduleInput.set(value);
    this.scheduleEdited.emit({ schedule_input: value.trim(), timezone: this.timezone().trim() || 'UTC' });
  }

  onTimezoneChange(value: string): void {
    this.timezone.set(value);
    this.scheduleEdited.emit({
      schedule_input: this.scheduleInput().trim(),
      timezone: value.trim() || 'UTC',
    });
  }

  requestPreview(): void {
    this.previewRequested.emit({
      schedule_input: this.scheduleInput().trim(),
      timezone: this.timezone().trim() || 'UTC',
    });
  }

  submit(): void {
    this.save.emit({
      title: this.title().trim() || '',
      prompt: this.prompt().trim(),
      schedule_input: this.scheduleInput().trim(),
      timezone: this.timezone().trim() || 'UTC',
      follow_user_timezone: this.followUserTimezone(),
      chat_id: this.chatId().trim() || '',
      personality_id: this.personalityId().trim() || '',
      model_id: this.modelId().trim() || '',
      reasoning_effort: this.reasoningEffort(),
      agent_loop: this.agentLoop(),
    });
  }

  resumeFollowing(): void {
    this.currentTimezone.resume();
  }

  private normalize(value: string | null | undefined): string {
    return (value || '').trim();
  }

  private normalizeTimezone(value: string | null | undefined): string {
    // Treat missing timezone as UTC to avoid false dirty-state diffs.
    return this.normalize(value) || 'UTC';
  }

  private hydrateFromJob(current: AgentJob): void {
    this.hydratedJobId = current.id;
    this.hydratedJobUpdatedAt = current.updated_at || '';
    this.title.set(current.title || '');
    this.prompt.set(current.prompt || '');
    this.scheduleInput.set(current.schedule_input || '');
    this.timezone.set(current.timezone || 'UTC');
    this.followUserTimezone.set(!!current.follow_user_timezone);
    this.chatId.set(current.chat_id || '');
    this.personalityId.set(current.personality_id || '');
    this.modelId.set(current.model_id || '');
    this.reasoningEffort.set(current.reasoning_effort ?? '');
    this.agentLoop.set(current.agent_loop ?? '');
    this.hydratedFormSnapshot = {
      title: this.normalize(current.title),
      prompt: this.normalize(current.prompt),
      scheduleInput: this.normalize(current.schedule_input),
      timezone: this.normalizeTimezone(current.timezone),
      followUserTimezone: !!current.follow_user_timezone,
      chatId: this.normalize(current.chat_id),
      personalityId: this.normalize(current.personality_id),
      modelId: this.normalize(current.model_id),
      reasoningEffort: current.reasoning_effort ?? '',
      agentLoop: current.agent_loop ?? '',
    };
  }

  private hasLocalDraftEdits(): boolean {
    const snapshot = this.hydratedFormSnapshot;
    if (!snapshot) return false;
    return (
      this.normalize(this.title()) !== snapshot.title ||
      this.normalize(this.prompt()) !== snapshot.prompt ||
      this.normalize(this.scheduleInput()) !== snapshot.scheduleInput ||
      this.normalizeTimezone(this.timezone()) !== snapshot.timezone ||
      this.followUserTimezone() !== snapshot.followUserTimezone ||
      this.normalize(this.chatId()) !== snapshot.chatId ||
      this.normalize(this.personalityId()) !== snapshot.personalityId ||
      this.normalize(this.modelId()) !== snapshot.modelId ||
      this.reasoningEffort() !== snapshot.reasoningEffort ||
      this.agentLoop() !== snapshot.agentLoop
    );
  }

  private matchesCurrentJob(current: AgentJob): boolean {
    return (
      this.normalize(this.title()) === this.normalize(current.title) &&
      this.normalize(this.prompt()) === this.normalize(current.prompt) &&
      this.normalize(this.scheduleInput()) === this.normalize(current.schedule_input) &&
      this.normalizeTimezone(this.timezone()) === this.normalizeTimezone(current.timezone) &&
      this.followUserTimezone() === !!current.follow_user_timezone &&
      this.normalize(this.chatId()) === this.normalize(current.chat_id) &&
      this.normalize(this.personalityId()) === this.normalize(current.personality_id) &&
      this.normalize(this.modelId()) === this.normalize(current.model_id) &&
      this.reasoningEffort() === (current.reasoning_effort ?? '') &&
      this.agentLoop() === (current.agent_loop ?? '')
    );
  }
}

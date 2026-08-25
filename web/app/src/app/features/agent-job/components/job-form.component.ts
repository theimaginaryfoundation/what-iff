
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { AgentJob, AgentJobScheduleParseRequest, AgentJobSchedulePreview, UpdateAgentJobRequest } from '../../../core/models/agent-job.model';
import { Chat } from '../../../core/models/chat.model';
import { Model } from '../../../core/models/model.model';
import { Personality } from '../../../core/models/personality.model';

@Component({
  selector: 'app-job-form',
  standalone: true,
  imports: [FormsModule],
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
  readonly chatId = signal('');
  readonly personalityId = signal('');
  readonly modelId = signal('');
  private hydratedJobId: string | null = null;
  private hydratedJobUpdatedAt: string | null = null;
  private hydratedFormSnapshot: {
    title: string;
    prompt: string;
    scheduleInput: string;
    timezone: string;
    chatId: string;
    personalityId: string;
    modelId: string;
  } | null = null;
  // Reflect local draft edits without rehydrating on poll refreshes for the same job.
  readonly hasUnsavedEdits = computed(() => {
    const current = this.job();
    if (!current) return false;
    return (
      this.normalize(this.title()) !== this.normalize(current.title) ||
      this.normalize(this.prompt()) !== this.normalize(current.prompt) ||
      this.normalize(this.scheduleInput()) !== this.normalize(current.schedule_input) ||
      this.normalizeTimezone(this.timezone()) !== this.normalizeTimezone(current.timezone) ||
      this.normalize(this.chatId()) !== this.normalize(current.chat_id) ||
      this.normalize(this.personalityId()) !== this.normalize(current.personality_id) ||
      this.normalize(this.modelId()) !== this.normalize(current.model_id)
    );
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
      chat_id: this.chatId().trim() || '',
      personality_id: this.personalityId().trim() || '',
      model_id: this.modelId().trim() || '',
    });
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
    this.chatId.set(current.chat_id || '');
    this.personalityId.set(current.personality_id || '');
    this.modelId.set(current.model_id || '');
    this.hydratedFormSnapshot = {
      title: this.normalize(current.title),
      prompt: this.normalize(current.prompt),
      scheduleInput: this.normalize(current.schedule_input),
      timezone: this.normalizeTimezone(current.timezone),
      chatId: this.normalize(current.chat_id),
      personalityId: this.normalize(current.personality_id),
      modelId: this.normalize(current.model_id),
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
      this.normalize(this.chatId()) !== snapshot.chatId ||
      this.normalize(this.personalityId()) !== snapshot.personalityId ||
      this.normalize(this.modelId()) !== snapshot.modelId
    );
  }

  private matchesCurrentJob(current: AgentJob): boolean {
    return (
      this.normalize(this.title()) === this.normalize(current.title) &&
      this.normalize(this.prompt()) === this.normalize(current.prompt) &&
      this.normalize(this.scheduleInput()) === this.normalize(current.schedule_input) &&
      this.normalizeTimezone(this.timezone()) === this.normalizeTimezone(current.timezone) &&
      this.normalize(this.chatId()) === this.normalize(current.chat_id) &&
      this.normalize(this.personalityId()) === this.normalize(current.personality_id) &&
      this.normalize(this.modelId()) === this.normalize(current.model_id)
    );
  }
}

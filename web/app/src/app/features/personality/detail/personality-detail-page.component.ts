import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  OnInit,
  signal,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ChatService } from '../../../core/services/chat.service';
import {
  Personality,
  PersonalityExpression,
  UpdatePersonalityRequest,
} from '../../../core/models/personality.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { PersonalityViewService } from '../../../core/services/personality-view.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { UserPreferences } from '../../../core/models/user.model';
import { NULL_PERSONALITY_ID } from '../../../core/constants/app.constants';
import { PersonaAccentScopeComponent } from '../picker/persona-accent-scope.component';
import { PersonaCoverComponent } from '../picker/persona-cover.component';
import { toPersonalityDetailVm } from '../helpers/personality-vm.helpers';
import { personalityCoverUrl } from '../helpers/cover-image.helpers';
import { PersonalitySystemPromptEditorComponent, SystemPromptValue } from './personality-system-prompt-editor.component';
import { PersonalityAttachmentsListComponent } from './personality-attachments-list.component';
import { PersonalityExpressionsManagerComponent } from './personality-expressions-manager.component';
import { PersonalityMediaJobBannerComponent } from '../components/personality-media-job-banner.component';
import { BellIconComponent } from '../../../shared/ui/icons/icons';

@Component({
  selector: 'app-personality-detail-page',
  standalone: true,
  imports: [
    PersonaAccentScopeComponent,
    PersonaCoverComponent,
    PersonalitySystemPromptEditorComponent,
    PersonalityAttachmentsListComponent,
    PersonalityExpressionsManagerComponent,
    PersonalityMediaJobBannerComponent,
    BellIconComponent,
  ],
  providers: [PersonalityViewService],
  templateUrl: './personality-detail-page.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityDetailPageComponent implements OnInit {
  private readonly view = inject(PersonalityViewService);
  private readonly chatService = inject(ChatService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly personalityService = inject(PersonalityService);
  private readonly userPreferencesService = inject(UserPreferencesService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly personality = this.view.personality;
  readonly expressions = this.view.expressions;
  readonly isLoading = this.view.loading;
  readonly errorMessage = this.view.error;

  readonly preferences = toSignal<UserPreferences | null>(this.userPreferencesService.preferences$, {
    initialValue: null,
  });

  readonly defaultPersonalityId = computed<string | null>(() => {
    const id = this.preferences()?.default_personality_id;
    if (!id || id === NULL_PERSONALITY_ID) return null;
    return id;
  });

  readonly isDefault = computed(() => {
    const personality = this.personality();
    if (!personality) return false;
    return this.defaultPersonalityId() === personality.id;
  });

  readonly vm = computed(() => {
    const personality = this.personality();
    if (!personality) return null;
    return toPersonalityDetailVm(personality, {
      defaultPersonalityId: this.defaultPersonalityId(),
      coverImageUrl: personalityCoverUrl(
        personality,
        this.expressions(),
        this.imageGallery.getImageUrl.bind(this.imageGallery),
      ),
    });
  });

  readonly autoPinUpdating = signal(false);
  readonly expressionsEnabledUpdating = signal(false);

  readonly systemPromptValue = computed<SystemPromptValue>(() => {
    const personality = this.personality();
    return {
      name: personality?.name ?? '',
      systemPrompt: personality?.system_prompt ?? '',
    };
  });

  ngOnInit(): void {
    this.userPreferencesService.getUserPreferences().subscribe();
    const id = this.route.snapshot.paramMap.get('id');
    if (!id) {
      this.router.navigate(['/personality']);
      return;
    }
    this.view.setActive(id);
  }

  onExpressionsChanged(next: readonly PersonalityExpression[]): void {
    this.view.setExpressions(next);
  }

  goBack(): void {
    this.router.navigate(['/personality']);
  }

  onExpressionsEnabledChanged(enabled: boolean): void {
    const personality = this.personality();
    if (!personality || personality.expressions_enabled === enabled) return;
    this.expressionsEnabledUpdating.set(true);
    // Optimistic update so zoneless CD reflects the toggle immediately.
    this.view.setPersonality({ ...personality, expressions_enabled: enabled });
    const request = this.buildUpdateRequest(personality, {
      expressions_enabled: enabled,
    });
    this.personalityService.updatePersonality(personality.id, request).subscribe({
      next: updated => {
        this.view.setPersonality(updated);
        this.expressionsEnabledUpdating.set(false);
      },
      error: async err => {
        console.error('Failed to update expressions setting', err);
        this.view.setPersonality(personality);
        this.expressionsEnabledUpdating.set(false);
        await this.confirmation.alert({
          message: 'Failed to update expressions setting. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  onAutoPinMemoriesChange(enabled: boolean): void {
    const personality = this.personality();
    if (!personality || personality.auto_pin_memories === enabled) return;
    this.autoPinUpdating.set(true);
    const request = this.buildUpdateRequest(personality, {
      auto_pin_memories: enabled,
    });
    this.personalityService.updatePersonality(personality.id, request).subscribe({
      next: updated => {
        this.view.setPersonality(updated);
        this.autoPinUpdating.set(false);
      },
      error: async err => {
        console.error('Failed to update auto-pin setting', err);
        this.autoPinUpdating.set(false);
        await this.confirmation.alert({
          message: 'Failed to update auto-pin setting. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async onSavePrompt(value: SystemPromptValue): Promise<void> {
    const personality = this.personality();
    if (!personality) return;
    const request = this.buildUpdateRequest(personality, {
      name: value.name,
      system_prompt: value.systemPrompt,
    });
    this.personalityService.updatePersonality(personality.id, request).subscribe({
      next: updated => this.view.setPersonality(updated),
      error: async err => {
        console.error('Failed to save personality', err);
        await this.confirmation.alert({
          message: 'Failed to save personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async onDelete(): Promise<void> {
    const personality = this.personality();
    if (!personality) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Delete Personality',
      message: `Are you sure you want to delete "${personality.name}"? This action cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.personalityService.deletePersonality(personality.id).subscribe({
      next: () => this.router.navigate(['/personality']),
      error: async err => {
        console.error('Failed to delete personality', err);
        await this.confirmation.alert({
          message: 'Failed to delete personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async onMakeDefault(): Promise<void> {
    const personality = this.personality();
    const prefs = this.preferences();
    if (!personality || !prefs) return;
    if (this.isDefault()) return;
    const updated: UserPreferences = { ...prefs, default_personality_id: personality.id };
    this.userPreferencesService.updateUserPreferences(updated).subscribe({
      error: async err => {
        console.error('Failed to make default', err);
        await this.confirmation.alert({
          message: 'Failed to update default personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async onUseInNewChat(): Promise<void> {
    const personality = this.personality();
    if (!personality) return;
    try {
      const chat = await firstValueFrom(this.chatService.createChat({
        name: 'New Chat',
        personality_id: personality.id,
      }));
      this.chatService.setLastChatId(chat.id);
      this.router.navigate(['/chat', chat.id]);
    } catch (err) {
      console.error('Failed to start chat', err);
      await this.confirmation.alert({
        message: 'Failed to start a new chat with this personality.',
        type: 'danger',
      });
    }
  }

  /** Builds a full PUT payload so untouched personality settings are preserved on partial edits. */
  private buildUpdateRequest(
    personality: Personality,
    overrides: Partial<UpdatePersonalityRequest>,
  ): UpdatePersonalityRequest {
    return {
      name: personality.name,
      system_prompt: personality.system_prompt,
      auto_pin_memories: personality.auto_pin_memories,
      cover_image_id: personality.cover_image_id,
      accent_color: personality.accent_color,
      thumbnail_circle: personality.thumbnail_circle,
      scratchpad: personality.scratchpad,
      scratchpad_update_prompt: personality.scratchpad_update_prompt,
      archival_model: personality.archival_model,
      memory_search_prompt: personality.memory_search_prompt,
      memory_write_prompt: personality.memory_write_prompt,
      expressions_enabled: personality.expressions_enabled,
      image_style: personality.image_style,
      ...overrides,
    };
  }
}


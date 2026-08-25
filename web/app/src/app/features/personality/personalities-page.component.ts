import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  OnInit,
  signal,
} from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { filter, map } from 'rxjs/operators';
import { FormsModule } from '@angular/forms';

import {
  CreatePersonalityRequest,
  Personality,
  PersonalityFilters,
  PaginatedPersonalityResponse,
} from '../../core/models/personality.model';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { GeneratePersonalityModalService } from '../../core/services/generate-personality-modal.service';
import { UserPreferences } from '../../core/models/user.model';
import { NULL_PERSONALITY_ID } from '../../core/constants/app.constants';
import {
  PersonalityCardActionEvent,
  PersonalityCardGridComponent,
} from './components/personality-card-grid.component';
import {
  PersonalityFilterValue,
  PersonalitySortMode,
} from './components/personality-filter-bar.component';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { SparkleIconComponent } from '../../shared/ui/icons/icons';
import { PersonalityEditModalComponent } from './detail/personality-edit-modal.component';
import {
  TEXT_LIMIT_HARD_MAX,
  TEXT_LIMIT_WARNING_THRESHOLD,
} from '../../core/constants/text-limits.constants';

const DEFAULT_PAGE_SIZE = 24;

@Component({
  selector: 'app-personalities-page',
  standalone: true,
  imports: [
    FormsModule,
    PersonalityCardGridComponent,
    ModalComponent,
    SparkleIconComponent,
    PersonalityEditModalComponent,
  ],
  templateUrl: './personalities-page.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalitiesPageComponent implements OnInit {
  private readonly personalityService = inject(PersonalityService);
  private readonly userPreferencesService = inject(UserPreferencesService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly generatePersonalityModal = inject(GeneratePersonalityModalService);
  private readonly destroyRef = inject(DestroyRef);

  readonly personalities = signal<Personality[]>([]);
  readonly totalCount = signal(0);
  readonly isLoading = signal(false);
  readonly preferences = signal<UserPreferences | null>(null);
  readonly isUpdatingDefaultId = signal<string | null>(null);
  readonly currentPage = signal(1);
  readonly pageSize = signal(DEFAULT_PAGE_SIZE);

  readonly filters = signal<PersonalityFilterValue>({
    query: '',
    sort: 'recent',
    defaultOnly: false,
  });

  readonly isCreateOpen = signal(false);
  readonly isEditOpen = signal(false);
  readonly editingPersonality = signal<Personality | null>(null);
  readonly createForm = signal({ name: '', system_prompt: '' });
  readonly isCreating = signal(false);
  readonly createErrorMessage = signal<string | null>(null);
  readonly createPromptCharacterCount = computed(() => this.createForm().system_prompt.length);
  readonly createPromptCharacterCountLabel = computed(() =>
    this.createPromptCharacterCount().toLocaleString(),
  );
  readonly createPromptIsNearLimit = computed(
    () =>
      this.createPromptCharacterCount() >= TEXT_LIMIT_WARNING_THRESHOLD &&
      !this.createPromptIsOverLimit(),
  );
  readonly createPromptIsOverLimit = computed(
    () => this.createPromptCharacterCount() > TEXT_LIMIT_HARD_MAX,
  );
  readonly createLimitWarningOpen = signal(false);
  readonly createPromptHardLimitLabel = TEXT_LIMIT_HARD_MAX.toLocaleString();
  readonly createPromptWarningLimitLabel = TEXT_LIMIT_WARNING_THRESHOLD.toLocaleString();
  private readonly createLimitWarningAcknowledged = signal(false);

  readonly defaultPersonalityId = computed<string | null>(() => {
    const id = this.preferences()?.default_personality_id;
    if (!id || id === NULL_PERSONALITY_ID) return null;
    return id;
  });

  readonly visiblePersonalities = computed<Personality[]>(() => {
    const list = [...this.personalities()];
    const filters = this.filters();
    const defaultId = this.defaultPersonalityId();
    let filtered = list;
    if (filters.defaultOnly && defaultId) {
      filtered = filtered.filter(p => p.id === defaultId);
    } else if (filters.defaultOnly) {
      filtered = [];
    }
    return sortPersonalities(filtered, filters.sort);
  });

  readonly hasResults = computed(() => this.visiblePersonalities().length > 0);
  readonly needsSetup = toSignal(
    this.route.queryParamMap.pipe(map(params => params.get('setup') === '1')),
    { initialValue: this.route.snapshot.queryParamMap.get('setup') === '1' },
  );

  ngOnInit(): void {
    this.loadPreferences();
    this.loadPersonalities();

    this.route.queryParamMap
      .pipe(
        map(params => params.get('create') === '1'),
        filter(shouldOpen => shouldOpen),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(() => {
        this.openCreate();
        void this.router.navigate([], {
          relativeTo: this.route,
          queryParams: { create: null },
          queryParamsHandling: 'merge',
          replaceUrl: true,
        });
      });

    const createdSub = this.generatePersonalityModal.personalityCreated$.subscribe(() => {
      this.loadPersonalities();
      this.loadPreferences();
    });
    this.destroyRef.onDestroy(() => createdSub.unsubscribe());
  }

  loadPreferences(): void {
    this.userPreferencesService.getUserPreferences().subscribe({
      next: prefs => this.preferences.set(prefs),
      error: err => console.error('Failed to load preferences', err),
    });
  }

  loadPersonalities(): void {
    this.isLoading.set(true);
    const filters: PersonalityFilters = {};
    const query = this.filters().query.trim();
    if (query) filters.name = query;

    this.personalityService
      .listPersonalities(this.currentPage(), this.pageSize(), filters)
      .subscribe({
        next: (response: PaginatedPersonalityResponse) => {
          this.personalities.set(response.results ?? []);
          this.totalCount.set(response.total_count ?? 0);
          this.isLoading.set(false);
        },
        error: err => {
          console.error('Failed to load personalities', err);
          this.isLoading.set(false);
        },
      });
  }

  onFiltersChanged(value: PersonalityFilterValue): void {
    const previousQuery = this.filters().query;
    this.filters.set(value);
    if (value.query.trim() !== previousQuery.trim()) {
      this.currentPage.set(1);
      this.loadPersonalities();
    }
  }

  onAction(event: PersonalityCardActionEvent): void {
    switch (event.action) {
      case 'open':
        this.openPersonality(event.personality);
        return;
      case 'edit':
        this.openPersonality(event.personality, { edit: true });
        return;
      case 'delete':
        void this.deletePersonality(event.personality);
        return;
      case 'set-default':
        void this.makeDefault(event.personality);
        return;
    }
  }

  openPersonality(personality: Personality, opts: { edit?: boolean } = {}): void {
    if (opts.edit) {
      this.editingPersonality.set(personality);
      this.isEditOpen.set(true);
      return;
    }
    this.router.navigate(['/personality', personality.id]);
  }

  closeEditModal(): void {
    this.isEditOpen.set(false);
    this.editingPersonality.set(null);
  }

  onEditSaved(updated: Personality): void {
    this.personalities.update(list => list.map(item => (item.id === updated.id ? updated : item)));
    this.closeEditModal();
  }

  onEditDeleted(_personalityId: string): void {
    this.closeEditModal();
    this.loadPersonalities();
    this.loadPreferences();
  }

  navigateGenerate(): void {
    this.generatePersonalityModal.show();
  }

  openCreate(): void {
    this.createForm.set({ name: '', system_prompt: '' });
    this.createErrorMessage.set(null);
    this.createLimitWarningOpen.set(false);
    this.createLimitWarningAcknowledged.set(false);
    this.isCreateOpen.set(true);
  }

  closeCreate(): void {
    this.isCreateOpen.set(false);
    this.createForm.set({ name: '', system_prompt: '' });
    this.createErrorMessage.set(null);
    this.createLimitWarningOpen.set(false);
    this.createLimitWarningAcknowledged.set(false);
  }

  setCreateName(value: string): void {
    this.createForm.update(form => ({ ...form, name: value }));
  }

  setCreateSystemPrompt(value: string): void {
    this.createForm.update(form => ({ ...form, system_prompt: value }));
    this.createLimitWarningAcknowledged.set(false);
    if (this.createLimitWarningOpen()) {
      this.createLimitWarningOpen.set(false);
    }
  }

  submitCreate(): void {
    const form = this.createForm();
    if (!form.name.trim() || !form.system_prompt.trim()) {
      this.createErrorMessage.set('Both name and system prompt are required.');
      return;
    }
    if (this.createPromptIsOverLimit()) {
      this.createErrorMessage.set(
        `System prompt cannot exceed ${this.createPromptHardLimitLabel} characters.`,
      );
      return;
    }
    if (this.createPromptIsNearLimit() && !this.createLimitWarningAcknowledged()) {
      this.createLimitWarningOpen.set(true);
      return;
    }

    this.isCreating.set(true);
    this.createErrorMessage.set(null);
    const request: CreatePersonalityRequest = {
      name: form.name.trim(),
      system_prompt: form.system_prompt.trim(),
    };

    this.personalityService.createPersonality(request).subscribe({
      next: personality => {
        this.isCreating.set(false);
        this.closeCreate();
        this.loadPersonalities();
        this.loadPreferences();
        this.router.navigate(['/personality', personality.id]);
      },
      error: err => {
        this.isCreating.set(false);
        this.createErrorMessage.set(err?.message ?? 'Failed to create personality. Please try again.');
        console.error('Failed to create personality', err);
      },
    });
  }

  confirmCreateDespiteWarning(): void {
    this.createLimitWarningAcknowledged.set(true);
    this.createLimitWarningOpen.set(false);
    this.submitCreate();
  }

  async deletePersonality(personality: Personality): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Personality',
      message: `Are you sure you want to delete "${personality.name}"?`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.personalityService.deletePersonality(personality.id).subscribe({
      next: () => this.loadPersonalities(),
      error: async err => {
        console.error('Failed to delete personality', err);
        await this.confirmationService.alert({
          message: 'Failed to delete personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async makeDefault(personality: Personality): Promise<void> {
    const prefs = this.preferences();
    if (!prefs) return;
    if (this.defaultPersonalityId() === personality.id) return;

    this.isUpdatingDefaultId.set(personality.id);
    const updated: UserPreferences = { ...prefs, default_personality_id: personality.id };

    this.userPreferencesService.updateUserPreferences(updated).subscribe({
      next: next => {
        this.preferences.set(next);
        this.isUpdatingDefaultId.set(null);
      },
      error: async err => {
        console.error('Failed to update default personality', err);
        this.isUpdatingDefaultId.set(null);
        await this.confirmationService.alert({
          message: 'Failed to set default personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }
}

function sortPersonalities(list: Personality[], sort: PersonalitySortMode): Personality[] {
  switch (sort) {
    case 'alpha':
      return list.sort((a, b) => a.name.localeCompare(b.name));
    case 'most-used':
      return list.sort((a, b) => (b.stats?.chat_count ?? 0) - (a.stats?.chat_count ?? 0));
    case 'recent':
    default:
      return list.sort((a, b) => {
        const aLast = a.stats?.last_used_at ?? null;
        const bLast = b.stats?.last_used_at ?? null;
        if (aLast && bLast) return bLast.localeCompare(aLast);
        if (aLast) return -1;
        if (bLast) return 1;
        return (b.updated_at ?? '').localeCompare(a.updated_at ?? '');
      });
  }
}

import { computed, effect, inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';

import { Mood, UpdateMoodRequest } from '../../../core/models/mood.model';
import { Model } from '../../../core/models/model.model';
import { MCPServer } from '../../../core/models/mcp-server.model';
import { Personality } from '../../../core/models/personality.model';
import { Ritual } from '../../../core/models/ritual.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ModeViewService } from '../../../core/services/mode-view.service';
import { MCPServerService } from '../../../core/services/mcp-server.service';
import { ModelService } from '../../../core/services/model.service';
import { MoodService } from '../../../core/services/mood.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { RitualService } from '../../../core/services/ritual.service';
import { personalityCoverUrl } from '../../personality/helpers/cover-image.helpers';
import { personalityAccent } from '../../personality/helpers/personality-vm.helpers';
import { ModeEditModalState } from '../components/mode-edit-modal.component';
import { ModeSearchPickerOption } from '../components/mode-search-picker.component';
import { MODE_PLURAL, MODE_SINGULAR } from '../mode-ui.labels';
import {
  ModeCardVm,
  ModePersonalityVm,
  filterAssociationOptions,
  filterMoodsBySelectedPersonalities,
  initialsForName,
  moodSkillsChipText,
} from '../helpers/mode-vm.helpers';

@Injectable()
export class MoodListFacade {
  readonly modeSingular = MODE_SINGULAR;
  readonly modePlural = MODE_PLURAL;

  private moodService = inject(MoodService);
  private modelService = inject(ModelService);
  private mcpServerService = inject(MCPServerService);
  private galleryService = inject(ImageGalleryService);
  private personalityService = inject(PersonalityService);
  private ritualService = inject(RitualService);
  private modeView = inject(ModeViewService);
  private confirmationService = inject(ConfirmationService);
  private router = inject(Router);
  private lastCreateTick = 0;

  moods = signal<Mood[]>([]);
  personalities = signal<Personality[]>([]);
  allRituals = signal<Ritual[]>([]);
  allModels = signal<Model[]>([]);
  allMCPServers = signal<MCPServer[]>([]);
  isLoading = signal(false);
  isLoadingRituals = signal(false);
  isLoadingModels = signal(false);
  isLoadingMCPServers = signal(false);

  modalMode = signal<'create' | 'edit' | null>(null);
  isEditModalOpen = signal(false);
  editLoading = signal(false);
  editingMoodId = signal<string | null>(null);
  editError = signal<string | null>(null);
  editSaving = signal(false);

  formName = signal('');
  formDescription = signal('');
  formPromptSnippet = signal('');
  formRecommendedModel = signal('');
  formImageIds = signal<string[]>([]);
  formRitualIds = signal<string[]>([]);
  /** Snapshot of the form as last hydrated; null while no modal is open. */
  private editFormSnapshot = signal<{
    name: string;
    description: string;
    promptSnippet: string;
    recommendedModel: string;
    imageIds: string[];
    ritualIds: string[];
  } | null>(null);

  skillSearchQuery = signal('');
  skillDropdownOpen = signal(false);

  associationPickerMoodId = signal<string | null>(null);
  associationPickerQuery = signal('');
  associationDropdownOpen = signal(false);

  readonly selectedSidebarPersonalityIds = this.modeView.selectedPersonalityIds;
  readonly personalityById = computed(() => {
    const map = new Map<string, Personality>();
    for (const personality of this.personalities()) {
      map.set(personality.id, personality);
    }
    return map;
  });
  readonly visibleMoods = computed(() =>
    filterMoodsBySelectedPersonalities(this.moods(), this.selectedSidebarPersonalityIds()),
  );
  readonly ritualNameById = computed(() => {
    const map = new Map<string, string>();
    for (const ritual of this.allRituals()) {
      map.set(ritual.id, ritual.name);
    }
    return map;
  });
  readonly editingMood = computed(() => {
    const editingID = this.editingMoodId();
    if (!editingID) return null;
    return this.moods().find(mood => mood.id === editingID) ?? null;
  });
  readonly filteredSkillOptions = computed(() => {
    const query = this.skillSearchQuery().trim().toLowerCase();
    const selected = new Set(this.formRitualIds());
    return this.allRituals()
      .filter(ritual => !selected.has(ritual.id))
      .filter(ritual => !query || ritual.name.toLowerCase().includes(query));
  });
  readonly modeCards = computed<ModeCardVm[]>(() =>
    this.visibleMoods().map(mood => ({
      mood,
      title: mood.name,
      description: mood.description || 'No description provided yet.',
      toolsSilencedLabel: mood.recommended_model ? `Model: ${this.modelDisplayName(mood.recommended_model)}` : 'Model: Auto',
      skillsLabel: moodSkillsChipText(mood),
      jobsLabel: `${this.mcpCountForMood(mood)} MCPs`,
      personalities: this.personalityCardsForMood(mood),
    })),
  );
  readonly modelOptions = computed<ModeSearchPickerOption[]>(() =>
    this.allModels().map(model => ({ id: model.id, label: model.display_name })),
  );
  readonly mcpNameById = computed(() => {
    const map = new Map<string, string>();
    for (const server of this.allMCPServers()) {
      map.set(server.id, server.name);
    }
    return map;
  });
  readonly attachedMCPServerIds = computed(() => {
    const ritualByID = new Map(this.allRituals().map(ritual => [ritual.id, ritual] as const));
    const ids = new Set<string>();
    for (const ritualID of this.formRitualIds()) {
      const ritual = ritualByID.get(ritualID);
      for (const serverID of ritual?.mcp_server_ids ?? []) {
        ids.add(serverID);
      }
    }
    return [...ids];
  });
  readonly associationOptionsByMoodId = computed<Record<string, ModePersonalityVm[]>>(() => {
    const moodID = this.associationPickerMoodId();
    if (!moodID) return {};
    const mood = this.moods().find(item => item.id === moodID);
    if (!mood) return {};
    return {
      [moodID]: this.associationOptionsForMood(mood),
    };
  });
  readonly modalState = computed<ModeEditModalState>(() => ({
    mode: this.modalMode(),
    open: this.isEditModalOpen(),
    loading: this.editLoading(),
    error: this.editError(),
    saving: this.editSaving(),
    name: this.formName(),
    description: this.formDescription(),
    promptSnippet: this.formPromptSnippet(),
    recommendedModel: this.formRecommendedModel(),
    attachedSkills: this.formRitualIds().map(id => ({ id, label: this.ritualLabel(id) })),
    attachedMCPServers: this.attachedMCPServerIds().map(id => ({ id, label: this.mcpLabel(id) })),
  }));
  readonly skillPickerOptions = computed<ModeSearchPickerOption[]>(() =>
    this.filteredSkillOptions().map(ritual => ({ id: ritual.id, label: ritual.name })),
  );
  readonly attachedSkillsCountLabel = computed(() =>
    this.formRitualIds().length === 0 ? 'No skills attached' : `${this.formRitualIds().length} attached`,
  );
  readonly attachedMcpCountLabel = computed(() =>
    this.attachedMCPServerIds().length === 0 ? 'None' : `${this.attachedMCPServerIds().length} attached`,
  );
  /** True when the open mode form differs from its hydrated snapshot. */
  readonly hasUnsavedModeEdits = computed(() => {
    const snap = this.editFormSnapshot();
    if (!snap) return false;
    return (
      this.formName() !== snap.name ||
      this.formDescription() !== snap.description ||
      this.formPromptSnippet() !== snap.promptSnippet ||
      this.formRecommendedModel() !== snap.recommendedModel ||
      !stringArraysEqual(this.formImageIds(), snap.imageIds) ||
      !stringArraysEqual(this.formRitualIds(), snap.ritualIds)
    );
  });

  constructor() {
    effect(() => {
      const tick = this.modeView.createRequestTick();
      if (tick > this.lastCreateTick) {
        this.lastCreateTick = tick;
        this.openCreateModeModal();
      }
    });
  }

  initialize(): void {
    this.loadMoods();
    this.loadPersonalities();
    this.loadRitualsIfNeeded();
    this.loadModelsIfNeeded();
    this.loadMCPServersIfNeeded();
  }

  destroy(): void {
    document.body.style.overflow = '';
  }

  handleRouteMoodId(id: string | null): void {
    if (id) {
      this.openEditMoodModal(id, false);
      return;
    }
    if (this.modalMode() === 'edit') {
      this.closeModeModal(false);
    }
  }

  loadMoods(): void {
    this.isLoading.set(true);
    this.moodService.listMoods(1, 20).subscribe({
      next: (res) => {
        const nextMoods = (res.results as Mood[]).map(mood => ({
          ...mood,
          personality_ids: mood.personality_ids ?? [],
        }));
        this.moods.set(nextMoods);
        this.isLoading.set(false);
      },
      error: () => this.isLoading.set(false),
    });
  }

  loadPersonalities(): void {
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: (res) => this.personalities.set(res.results as Personality[]),
      error: () => {},
    });
  }

  openEditMood(id: string, navigate = true): void {
    this.openEditMoodModal(id, navigate);
  }

  openEditMoodModal(id: string, navigate = true): void {
    if (navigate) {
      void this.router.navigate(['/mode', id]);
    }
    this.modalMode.set('edit');
    this.editLoading.set(true);
    this.editError.set(null);
    this.isEditModalOpen.set(true);
    document.body.style.overflow = 'hidden';
    this.editingMoodId.set(id);

    this.moodService.getMood(id).subscribe({
      next: mood => {
        this.moods.update(current =>
          current.map(item => (item.id === mood.id ? { ...item, ...mood } : item)),
        );
        this.formName.set(mood.name);
        this.formDescription.set(mood.description);
        this.formPromptSnippet.set(mood.prompt_snippet);
        this.formRecommendedModel.set(mood.recommended_model ?? '');
        this.formImageIds.set([...(mood.image_ids ?? [])]);
        this.formRitualIds.set([...(mood.ritual_ids ?? [])]);
        this.editFormSnapshot.set({
          name: mood.name,
          description: mood.description,
          promptSnippet: mood.prompt_snippet,
          recommendedModel: mood.recommended_model ?? '',
          imageIds: [...(mood.image_ids ?? [])],
          ritualIds: [...(mood.ritual_ids ?? [])],
        });
        this.skillSearchQuery.set('');
        this.skillDropdownOpen.set(false);
        this.editLoading.set(false);
      },
      error: () => {
        this.editLoading.set(false);
        this.editError.set(`Failed to load ${this.modeSingular.toLowerCase()} details.`);
      },
    });
  }

  closeEditMood(restoreRoute = true): void {
    this.closeModeModal(restoreRoute);
  }

  closeModeModal(restoreRoute = true): void {
    const closingMode = this.modalMode();
    this.isEditModalOpen.set(false);
    this.modalMode.set(null);
    this.editingMoodId.set(null);
    this.editError.set(null);
    this.editSaving.set(false);
    this.editFormSnapshot.set(null);
    this.skillSearchQuery.set('');
    this.skillDropdownOpen.set(false);
    document.body.style.overflow = '';
    if (restoreRoute && closingMode !== 'create') {
      void this.router.navigate(['/mode']);
    }
  }

  /**
   * Backdrop/Escape dismissal: confirm before discarding unsaved edits.
   * The explicit ✕ button uses {@link closeModalFromOverlay} and closes immediately.
   */
  async requestCloseModeModal(): Promise<void> {
    if (this.hasUnsavedModeEdits() && !(await this.confirmationService.confirmDiscardChanges())) {
      return;
    }
    this.closeModalFromOverlay();
  }

  saveEditMood(): void {
    if (this.modalMode() === 'create') {
      this.createModeFromSharedModal();
      return;
    }
    const moodID = this.editingMoodId();
    if (!moodID) return;
    const name = this.formName().trim();
    if (!name) {
      this.editError.set('Name is required.');
      return;
    }

    this.editSaving.set(true);
    this.editError.set(null);
    const updateReq: UpdateMoodRequest = {
      name,
      description: this.formDescription(),
      prompt_snippet: this.formPromptSnippet(),
      recommended_model: this.formRecommendedModel(),
      image_ids: this.formImageIds(),
      ritual_ids: this.formRitualIds(),
    };
    this.moodService.updateMood(moodID, updateReq).subscribe({
      next: updated => {
        this.moods.update(current =>
          current.map(mood => (mood.id === moodID ? { ...mood, ...updated, personality_ids: mood.personality_ids ?? [] } : mood)),
        );
        this.editSaving.set(false);
        this.closeModeModal(true);
      },
      error: () => {
        this.editSaving.set(false);
        this.editError.set(`Failed to save ${this.modeSingular.toLowerCase()}.`);
      },
    });
  }

  setSkillSearchQuery(value: string): void {
    this.skillSearchQuery.set(value);
    this.skillDropdownOpen.set(true);
  }

  openSkillDropdown(): void {
    this.skillDropdownOpen.set(true);
  }

  closeSkillDropdown(): void {
    setTimeout(() => this.skillDropdownOpen.set(false), 100);
  }

  addAttachedSkill(id: string): void {
    this.formRitualIds.update(ids => (ids.includes(id) ? ids : [...ids, id]));
    this.skillSearchQuery.set('');
    this.skillDropdownOpen.set(false);
  }

  removeAttachedSkill(id: string): void {
    this.formRitualIds.update(ids => ids.filter(item => item !== id));
  }

  async deleteEditingMood(): Promise<void> {
    const mood = this.editingMood();
    if (!mood) return;
    const confirmed = await this.confirmationService.confirm({
      title: `Delete ${MODE_SINGULAR}`,
      message: `Delete "${mood.name}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.moodService.deleteMood(mood.id).subscribe({
      next: () => {
        this.closeEditMood(true);
        this.loadMoods();
      },
      error: async () => {
        await this.confirmationService.alert({ message: `Failed to delete ${MODE_SINGULAR.toLowerCase()}.`, type: 'danger' });
      },
    });
  }

  async removePersonalityFromMode(mood: Mood, personalityID: string, event: Event): Promise<void> {
    event.stopPropagation();
    const nextIDs = (mood.personality_ids ?? []).filter(id => id !== personalityID);
    this.moodService.attachToPersonalities(mood.id, { personality_ids: nextIDs }).subscribe({
      next: () => {
        this.moods.update(current =>
          current.map(item => (item.id === mood.id ? { ...item, personality_ids: nextIDs } : item)),
        );
      },
      error: async () => {
        await this.confirmationService.alert({
          message: `Failed to update ${MODE_SINGULAR.toLowerCase()} personalities.`,
          type: 'danger',
        });
      },
    });
  }

  openAssociationPicker(moodID: string, event?: Event): void {
    event?.stopPropagation();
    this.associationPickerMoodId.set(moodID);
    this.associationPickerQuery.set('');
    this.associationDropdownOpen.set(false);
  }

  closeAssociationPicker(): void {
    this.associationPickerMoodId.set(null);
    this.associationPickerQuery.set('');
    this.associationDropdownOpen.set(false);
  }

  setAssociationPickerQuery(value: string): void {
    this.associationPickerQuery.set(value);
    this.associationDropdownOpen.set(true);
  }

  openAssociationDropdown(): void {
    this.associationDropdownOpen.set(true);
  }

  closeAssociationDropdown(): void {
    setTimeout(() => this.associationDropdownOpen.set(false), 100);
  }

  addPersonalityToMode(mood: Mood, personalityID: string, event?: Event): void {
    event?.stopPropagation();
    const nextIDs = Array.from(new Set([...(mood.personality_ids ?? []), personalityID]));
    this.moodService.attachToPersonalities(mood.id, { personality_ids: nextIDs }).subscribe({
      next: () => {
        this.moods.update(current =>
          current.map(item => (item.id === mood.id ? { ...item, personality_ids: nextIDs } : item)),
        );
        this.closeAssociationPicker();
      },
      error: async () => {
        await this.confirmationService.alert({
          message: `Failed to associate personality to ${MODE_SINGULAR.toLowerCase()}.`,
          type: 'danger',
        });
      },
    });
  }

  async deleteMood(mood: Mood, event: Event): Promise<void> {
    event.stopPropagation();
    const confirmed = await this.confirmationService.confirm({
      title: `Delete ${MODE_SINGULAR}`,
      message: `Delete "${mood.name}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.moodService.deleteMood(mood.id).subscribe({
      next: () => this.loadMoods(),
      error: async () => {
        await this.confirmationService.alert({ message: `Failed to delete ${MODE_SINGULAR.toLowerCase()}.`, type: 'danger' });
      },
    });
  }

  openCreateModeModal(): void {
    this.modalMode.set('create');
    this.isEditModalOpen.set(true);
    this.editLoading.set(false);
    this.editError.set(null);
    this.editingMoodId.set(null);
    this.formName.set('');
    this.formDescription.set('');
    this.formPromptSnippet.set('');
    this.formRecommendedModel.set('');
    this.formImageIds.set([]);
    this.formRitualIds.set([]);
    this.editFormSnapshot.set({
      name: '',
      description: '',
      promptSnippet: '',
      recommendedModel: '',
      imageIds: [],
      ritualIds: [],
    });
    this.skillSearchQuery.set('');
    this.skillDropdownOpen.set(false);
    document.body.style.overflow = 'hidden';
  }

  createModeFromSharedModal(): void {
    const name = this.formName().trim();
    if (!name) {
      this.editError.set('Name is required.');
      return;
    }
    this.editSaving.set(true);
    const req = {
      name,
      description: this.formDescription(),
      prompt_snippet: this.formPromptSnippet(),
      recommended_model: this.formRecommendedModel(),
      image_ids: this.formImageIds(),
      ritual_ids: this.formRitualIds(),
    };
    this.moodService.createMood(req).subscribe({
      next: mood => {
        this.editSaving.set(false);
        this.moods.update(current => [{ ...mood, personality_ids: [] }, ...current]);
        this.closeModeModal(false);
      },
      error: () => {
        this.editSaving.set(false);
        this.editError.set(`Failed to create ${MODE_SINGULAR.toLowerCase()}. Please try again.`);
      },
    });
  }

  ritualLabel(id: string): string {
    return this.ritualNameById().get(id) ?? `Skill ${id.slice(0, 8)}`;
  }

  associationOptionsForMood(mood: Mood): ModePersonalityVm[] {
    return filterAssociationOptions(this.personalities(), mood, this.associationPickerQuery()).map(personality =>
      this.toPersonalityVm(personality),
    );
  }

  personalityCardsForMood(mood: Mood): ModePersonalityVm[] {
    const ids = mood.personality_ids ?? [];
    const byID = this.personalityById();
    return ids
      .map(id => byID.get(id) ?? null)
      .filter((value): value is Personality => value !== null)
      .map(personality => this.toPersonalityVm(personality));
  }

  handleDeleteMode(payload: { moodId: string; event: Event }): void {
    const mood = this.moods().find(item => item.id === payload.moodId);
    if (!mood) return;
    void this.deleteMood(mood, payload.event);
  }

  handleRemoveAssociation(payload: { moodId: string; personalityId: string; event: Event }): void {
    const mood = this.moods().find(item => item.id === payload.moodId);
    if (!mood) return;
    void this.removePersonalityFromMode(mood, payload.personalityId, payload.event);
  }

  handleAddAssociation(payload: { moodId: string; personalityId: string }): void {
    const mood = this.moods().find(item => item.id === payload.moodId);
    if (!mood) return;
    this.addPersonalityToMode(mood, payload.personalityId);
  }

  closeModalFromOverlay(): void {
    this.closeEditMood(this.modalMode() === 'edit');
  }

  setFormName(value: string): void {
    this.formName.set(value);
  }

  setFormDescription(value: string): void {
    this.formDescription.set(value);
  }

  setFormPromptSnippet(value: string): void {
    this.formPromptSnippet.set(value);
  }

  setFormRecommendedModel(value: string): void {
    this.formRecommendedModel.set(value);
  }

  private loadRitualsIfNeeded(): void {
    if (this.allRituals().length > 0 || this.isLoadingRituals()) return;
    this.isLoadingRituals.set(true);
    this.ritualService.listRituals(1, 100).subscribe({
      next: res => {
        this.allRituals.set((res.results ?? []) as Ritual[]);
        this.isLoadingRituals.set(false);
      },
      error: () => this.isLoadingRituals.set(false),
    });
  }

  private loadModelsIfNeeded(): void {
    if (this.allModels().length > 0 || this.isLoadingModels()) return;
    this.isLoadingModels.set(true);
    this.modelService.getModels().subscribe({
      next: models => {
        this.allModels.set(models ?? []);
        this.isLoadingModels.set(false);
      },
      error: () => this.isLoadingModels.set(false),
    });
  }

  private loadMCPServersIfNeeded(): void {
    if (this.allMCPServers().length > 0 || this.isLoadingMCPServers()) return;
    this.isLoadingMCPServers.set(true);
    this.mcpServerService.listMCPServers(1, 100).subscribe({
      next: res => {
        this.allMCPServers.set((res.results ?? []) as MCPServer[]);
        this.isLoadingMCPServers.set(false);
      },
      error: () => this.isLoadingMCPServers.set(false),
    });
  }

  private modelDisplayName(modelID: string): string {
    return this.allModels().find(model => model.id === modelID)?.display_name ?? 'Custom';
  }

  private mcpCountForMood(mood: Mood): number {
    const ritualByID = new Map(this.allRituals().map(ritual => [ritual.id, ritual] as const));
    const ids = new Set<string>();
    for (const ritualID of mood.ritual_ids ?? []) {
      const ritual = ritualByID.get(ritualID);
      for (const serverID of ritual?.mcp_server_ids ?? []) {
        ids.add(serverID);
      }
    }
    return ids.size;
  }

  private mcpLabel(id: string): string {
    return this.mcpNameById().get(id) ?? `MCP ${id.slice(0, 8)}`;
  }

  private toPersonalityVm(personality: Personality): ModePersonalityVm {
    return {
      id: personality.id,
      name: personality.name,
      accentColor: personalityAccent(personality),
      coverUrl: personalityCoverUrl(
        personality,
        [],
        this.galleryService.getImageUrl.bind(this.galleryService),
      ),
      initials: initialsForName(personality.name),
    };
  }
}

function stringArraysEqual(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

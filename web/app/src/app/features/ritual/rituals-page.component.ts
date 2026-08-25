import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, HostListener, inject, OnInit, signal, viewChild } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { forkJoin } from 'rxjs';
import { firstValueFrom } from 'rxjs';

import { Ritual, RitualSort } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualViewService } from '../../core/services/ritual-view.service';
import { RitualService } from '../../core/services/ritual.service';
import { ChatService } from '../../core/services/chat.service';
import { DraftMessageService } from '../../core/services/draft-message.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import {
  parseQueryParams,
  RitualViewFilters,
  serializeFilters,
} from './helpers/ritual-filter.helpers';
import { toRitualRowVm } from './helpers/ritual-vm.helpers';
import { RitualSelectOption } from './components/ritual-filter-bar.component';
import { RitualFormComponent, RitualFormSave } from './components/ritual-form.component';
import { PersonaPickerDialogComponent } from '../personality/picker/persona-picker-dialog.component';
import { PersonaCoverComponent } from '../personality/picker/persona-cover.component';
import { BoltIconComponent, ChevDownIconComponent, EditIconComponent, SearchIconComponent, TrashIconComponent } from '../../shared/ui/icons/icons';
import { personalityAccent, personalityAccentSurface } from '../personality/helpers/personality-vm.helpers';

const NIL_UUID = '00000000-0000-0000-0000-000000000000';

@Component({
  selector: 'app-rituals-page',
  standalone: true,
  imports: [
    CommonModule,
    RitualFormComponent,
    PersonaPickerDialogComponent,
    PersonaCoverComponent,
    BoltIconComponent,
    ChevDownIconComponent,
    SearchIconComponent,
    EditIconComponent,
    TrashIconComponent,
  ],
  templateUrl: './rituals-page.component.html',
  styleUrl: './rituals-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualsPageComponent implements OnInit {
  private readonly view = inject(RitualViewService);
  private readonly ritualService = inject(RitualService);
  private readonly personalityService = inject(PersonalityService);
  private readonly chatService = inject(ChatService);
  private readonly draftMessageService = inject(DraftMessageService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly confirmation = inject(ConfirmationService);

  readonly personalities = signal<RitualSelectOption[]>([]);
  readonly personalityRecords = signal<Personality[]>([]);
  readonly filters = this.view.filters;
  readonly loading = this.view.loading;
  readonly error = this.view.error;
  readonly currentPage = this.view.currentPage;
  readonly totalPages = this.view.totalPages;
  readonly totalCount = this.view.totalCount;
  readonly editorOpen = signal(false);
  readonly editorSaving = signal(false);
  readonly editorError = signal<string | null>(null);
  readonly editingRitual = signal<Ritual | null>(null);
  readonly editingSystem = signal(false);
  readonly creating = signal(false);
  readonly personaPickerOpen = signal(false);
  readonly pendingCallRitual = signal<{ id: string; name: string } | null>(null);
  private readonly ritualForm = viewChild(RitualFormComponent);

  readonly personalityNameById = computed(
    () => new Map(this.personalities().map(personality => [personality.id, personality.label])),
  );
  private readonly personalityById = computed(
    () => new Map(this.personalityRecords().map(personality => [personality.id, personality])),
  );
  readonly allRows = computed(() => {
    const personalRows = this.view.rituals().map(ritual => toRitualRowVm(ritual, this.personalityNameById(), false));
    const systemRows = this.view.systemRituals().map(ritual => toRitualRowVm(ritual, this.personalityNameById(), true));
    return [...systemRows, ...personalRows];
  });
  private readonly associationMode = computed<'all' | 'global' | 'personality'>(() => {
    const filters = this.filters();
    if (filters.globalOnly) return 'global';
    const selected = filters.personalityIds.length
      ? filters.personalityIds
      : (filters.personalityId.trim() ? [filters.personalityId.trim()] : []);
    return selected.length > 0 ? 'personality' : 'all';
  });
  readonly visibleRows = computed(() => {
    const rows = this.allRows();
    const mode = this.associationMode();
    const query = this.filters().query.trim().toLowerCase();
    if (mode !== 'all') {
      return rows.filter(row => !row.isSystem);
    }

    const systemRows = rows.filter(row => row.isSystem);
    const personalRows = rows.filter(row => !row.isSystem);
    const visibleSystemRows = query
      ? systemRows.filter(row =>
          row.name.toLowerCase().includes(query) ||
          row.description.toLowerCase().includes(query) ||
          row.content.toLowerCase().includes(query),
        )
      : systemRows;

    return [...visibleSystemRows, ...personalRows];
  });

  ngOnInit(): void {
    forkJoin({
      personalities: this.personalityService.listPersonalities(1, 200),
    }).subscribe({
      next: result => {
        const list = result.personalities.results ?? [];
        this.personalityRecords.set(list);
        this.personalities.set(list.map(personality => ({ id: personality.id, label: personality.name })));
      },
      error: () => {
        this.personalityRecords.set([]);
        this.personalities.set([]);
      },
    });

    this.route.queryParams.subscribe(params => {
      this.view.applyFilters(parseQueryParams(params));
      if (String(params['create'] ?? '') === '1' && !this.editorOpen()) {
        this.onOpen('new');
        void this.router.navigate([], { queryParams: { create: null }, queryParamsHandling: 'merge', replaceUrl: true });
      }
    });
    this.view.loadSystemRituals();
  }

  onFilterChanged(partial: Partial<RitualViewFilters>): void {
    const next = { ...this.filters(), ...partial };
    this.view.setFilters(partial);
    void this.router.navigate([], { queryParams: serializeFilters(next), replaceUrl: true });
  }

  onClearFilters(): void {
    this.view.clearFilters();
    void this.router.navigate([], { queryParams: {}, replaceUrl: true });
  }

  onOpen(ritualId: string): void {
    this.view.setSelectedRitualId(ritualId);
    if (ritualId === 'new') {
      this.creating.set(true);
      this.editingRitual.set(null);
      this.editingSystem.set(false);
      this.editorError.set(null);
      this.editorOpen.set(true);
      return;
    }

    const personal = this.view.rituals().find(ritual => ritual.id === ritualId);
    if (personal) {
      this.creating.set(false);
      this.editingSystem.set(false);
      this.editingRitual.set(personal);
      this.editorError.set(null);
      this.editorOpen.set(true);
      return;
    }

    const system = this.view.systemRituals().find(ritual => ritual.id === ritualId);
    if (system) {
      this.creating.set(false);
      this.editingSystem.set(true);
      this.editingRitual.set(system);
      this.editorError.set(null);
      this.editorOpen.set(true);
      return;
    }

    this.ritualService.getRitual(ritualId).subscribe({
      next: ritual => {
        this.creating.set(false);
        this.editingSystem.set(false);
        this.editingRitual.set(ritual);
        this.editorError.set(null);
        this.editorOpen.set(true);
      },
      error: () => {
        this.editorError.set('Failed to load skill.');
      },
    });
  }

  closeEditor(): void {
    this.editorOpen.set(false);
    this.editorSaving.set(false);
    this.editorError.set(null);
    this.editingRitual.set(null);
    this.creating.set(false);
    this.editingSystem.set(false);
  }

  /**
   * Backdrop/Escape dismissal: confirm before discarding unsaved edits.
   * The Cancel button calls {@link closeEditor} directly and closes immediately.
   */
  async requestCloseEditor(): Promise<void> {
    if (this.editorSaving()) return;
    if (this.ritualForm()?.hasUnsavedEdits() && !(await this.confirmation.confirmDiscardChanges())) {
      return;
    }
    this.closeEditor();
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.editorOpen()) {
      void this.requestCloseEditor();
    }
  }

  onSaveFromEditor(payload: RitualFormSave): void {
    this.editorSaving.set(true);
    this.editorError.set(null);

    if (this.creating()) {
      this.ritualService.createRitual(payload).subscribe({
        next: () => {
          this.editorSaving.set(false);
          this.closeEditor();
          this.view.refresh();
        },
        error: error => {
          this.editorSaving.set(false);
          this.editorError.set(error instanceof Error ? error.message : 'Failed to create skill');
        },
      });
      return;
    }

    const ritual = this.editingRitual();
    if (!ritual || this.editingSystem()) {
      this.editorSaving.set(false);
      return;
    }

    this.ritualService.updateRitual(ritual.id, payload).subscribe({
      next: updated => {
        this.editorSaving.set(false);
        this.editingRitual.set(updated);
        this.closeEditor();
        this.view.refresh();
      },
      error: error => {
        this.editorSaving.set(false);
        this.editorError.set(error instanceof Error ? error.message : 'Failed to save skill');
      },
    });
  }

  async onDelete(ritualId: string): Promise<void> {
    const confirmed = await this.confirmation.confirm({
      title: 'Delete skill',
      message: 'Are you sure you want to delete this skill?',
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.view.deleteOne(ritualId).subscribe({
      next: () => this.view.load(this.currentPage()),
    });
  }

  onSearch(query: string): void {
    this.onFilterChanged({ query });
  }

  onSortChanged(value: string): void {
    if (value === 'created_desc' || value === 'updated_desc' || value === 'name_asc') {
      this.onFilterChanged({ sort: value as RitualSort });
    }
  }

  changePage(page: number): void {
    if (page < 1 || page > this.totalPages()) return;
    this.view.load(page);
  }

  personalityFor(id: string | null): Personality | null {
    const normalized = this.normalizePersonalityId(id);
    if (!normalized) return null;
    return this.personalityById().get(normalized) ?? null;
  }

  rowAccent(row: { personalityId: string | null }): string {
    const personality = this.personalityFor(row.personalityId);
    if (personality) {
      return personalityAccent(personality);
    }
    return 'var(--color-accent)';
  }

  rowAccentSurface(row: { personalityId: string | null }): string {
    return personalityAccentSurface(this.rowAccent(row));
  }

  rowPersonaLabel(row: { personalityId: string | null; affinityLabel: string; isSystem: boolean }): string {
    if (row.isSystem) return 'SYSTEM';
    if (!this.normalizePersonalityId(row.personalityId)) return 'GLOBAL';
    return row.affinityLabel.trim().toUpperCase();
  }

  async callRitual(row: { id: string; name: string; personalityId: string | null }): Promise<void> {
    const personalityId = this.normalizePersonalityId(row.personalityId);
    if (!personalityId) {
      this.pendingCallRitual.set({ id: row.id, name: row.name });
      this.personaPickerOpen.set(true);
      return;
    }
    await this.createChatWithSkill(row.name, personalityId);
  }

  closePersonaPicker(): void {
    this.personaPickerOpen.set(false);
    this.pendingCallRitual.set(null);
  }

  async onPersonaPickedForCall(personality: Personality): Promise<void> {
    const pending = this.pendingCallRitual();
    this.personaPickerOpen.set(false);
    if (!pending) return;
    this.pendingCallRitual.set(null);
    await this.createChatWithSkill(pending.name, personality.id);
  }

  private async createChatWithSkill(skillName: string, personalityId: string): Promise<void> {
    const ritualInvocation = `/ritual ${skillName}`;
    try {
      const chat = await firstValueFrom(this.chatService.createChat({ name: 'New Chat', personality_id: personalityId }));
      this.draftMessageService.saveDraft(chat.id, ritualInvocation);
      this.chatService.setLastChatId(chat.id);
      await this.router.navigate(['/chat', chat.id]);
    } catch (error) {
      this.editorError.set(error instanceof Error ? error.message : 'Failed to start chat with skill');
    }
  }

  private normalizePersonalityId(id: string | null | undefined): string | null {
    const value = id?.trim();
    if (!value || value === NIL_UUID) return null;
    return value;
  }
}

import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { AgentEntity, AgentEntityFilters, MAX_ENTITY_BODY_CHARS } from '../../../core/models/agent-entity.model';
import { AgentEntityService } from '../../../core/services/agent-entity.service';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { SearchIconComponent } from '../../../shared/ui/icons/icons';
import { GLOBAL_SCOPE_LABEL } from '../helpers/memory-vm.helpers';
import { GLOBAL_PERSONALITY_FILTER } from '../helpers/memory-filter.helpers';
import { entitySegments, EntitySegment, normalizeEntityName } from '../helpers/entity.helpers';
import { MemoryPersonalityOption } from './memory-form.component';

const PAGE_SIZE = 50;

interface EntityVm {
  entity: AgentEntity;
  scopeLabel: string;
  segments: EntitySegment[];
}

/**
 * Memories > Entities: the entity wiki pages agents keep about people, places and projects
 * (ADR 0x034). The user can search, follow [[links]], edit a page's text and delete pages. Agents
 * create pages; there is no create here yet. A page an agent deletes is archived: the Archived view
 * lists those, to restore or delete for good.
 */
@Component({
  selector: 'app-entity-list',
  standalone: true,
  imports: [DatePipe, FormsModule, TooltipDirective, SearchIconComponent],
  templateUrl: './entity-list.component.html',
  styleUrl: './entity-list.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class EntityListComponent implements OnInit {
  private readonly entityService = inject(AgentEntityService);
  private loadRequestID = 0;

  readonly personalities = input<MemoryPersonalityOption[]>([]);

  readonly maxChars = MAX_ENTITY_BODY_CHARS;
  readonly globalPersonalityFilter = GLOBAL_PERSONALITY_FILTER;
  readonly globalScopeLabel = GLOBAL_SCOPE_LABEL;

  readonly entities = signal<AgentEntity[]>([]);
  readonly totalCount = signal(0);
  readonly page = signal(1);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly searchDraft = signal('');
  readonly query = signal('');
  readonly personalityFilter = signal('');
  /** Showing the pages agents archived instead of the live ones. */
  readonly archived = signal(false);
  readonly editingId = signal<string | null>(null);
  readonly draft = signal('');
  readonly saving = signal(false);
  readonly confirmDeleteId = signal<string | null>(null);
  readonly rowError = signal<{ id: string; message: string } | null>(null);

  readonly totalPages = computed(() => Math.max(1, Math.ceil(this.totalCount() / PAGE_SIZE)));
  readonly countLabel = computed(
    () => `${this.totalCount()}${this.archived() ? ' archived' : ''} entit${this.totalCount() === 1 ? 'y' : 'ies'}`,
  );
  readonly draftTooLong = computed(() => this.draft().length > this.maxChars);
  readonly items = computed<EntityVm[]>(() => {
    // A search for an entity's exact name (as when following a [[link]]) shows that entity first,
    // ahead of the pages that merely mention it.
    const exact = normalizeEntityName(this.query());
    const entities = exact ? [...this.entities()].sort((a, b) => Number(b.key === exact) - Number(a.key === exact)) : this.entities();
    return entities.map(entity => ({
      entity,
      scopeLabel: entity.pinned_personality_id ? (entity.pinned_personality_name ?? 'Personality') : this.globalScopeLabel,
      segments: entitySegments(entity.body),
    }));
  });

  ngOnInit(): void {
    this.load(1);
  }

  load(page: number): void {
    const requestID = ++this.loadRequestID;
    this.loading.set(true);
    this.error.set(null);
    this.page.set(page);
    this.entityService.list(page, PAGE_SIZE, this.filters()).subscribe({
      next: response => {
        if (requestID !== this.loadRequestID) return;
        this.entities.set(response.results ?? []);
        this.totalCount.set(response.total_count ?? 0);
        this.loading.set(false);
      },
      error: (err: Error) => {
        if (requestID !== this.loadRequestID) return;
        this.entities.set([]);
        this.totalCount.set(0);
        this.error.set(err.message || 'Failed to load entities');
        this.loading.set(false);
      },
    });
  }

  onSearchSubmit(): void {
    this.query.set(this.searchDraft().trim());
    this.load(1);
  }

  onPersonalityFilterChange(value: string): void {
    this.personalityFilter.set(value);
    this.load(1);
  }

  onArchivedChange(archived: boolean): void {
    this.archived.set(archived);
    this.editingId.set(null);
    this.confirmDeleteId.set(null);
    this.rowError.set(null);
    this.load(1);
  }

  /** Brings back a page an agent archived; it leaves the Archived view. */
  restore(entity: AgentEntity): void {
    if (this.saving()) return;
    this.saving.set(true);
    this.rowError.set(null);
    this.entityService.restore(entity.id).subscribe({
      next: () => {
        this.saving.set(false);
        this.reloadAfterRemoval();
      },
      error: (err: Error) => {
        this.rowError.set({ id: entity.id, message: err.message || 'Could not restore the entity' });
        this.saving.set(false);
      },
    });
  }

  /** Follows a [[link]]: searches for the linked entity by name. */
  followLink(name: string): void {
    this.searchDraft.set(name);
    this.onSearchSubmit();
  }

  startEdit(entity: AgentEntity): void {
    this.confirmDeleteId.set(null);
    this.rowError.set(null);
    this.editingId.set(entity.id);
    this.draft.set(entity.body);
  }

  cancelEdit(): void {
    this.editingId.set(null);
    this.rowError.set(null);
  }

  saveEdit(entity: AgentEntity): void {
    if (this.saving() || this.draftTooLong()) return;
    this.saving.set(true);
    this.entityService.updateBody(entity.id, this.draft()).subscribe({
      next: updated => {
        this.entities.update(list => list.map(e => (e.id === updated.id ? updated : e)));
        this.editingId.set(null);
        this.saving.set(false);
      },
      error: (err: Error) => {
        this.rowError.set({ id: entity.id, message: err.message || 'Could not save the entity' });
        this.saving.set(false);
      },
    });
  }

  confirmDelete(entity: AgentEntity): void {
    this.editingId.set(null);
    this.rowError.set(null);
    this.confirmDeleteId.set(entity.id);
  }

  delete(entity: AgentEntity): void {
    this.saving.set(true);
    this.entityService.delete(entity.id).subscribe({
      next: () => {
        this.saving.set(false);
        this.confirmDeleteId.set(null);
        this.reloadAfterRemoval();
      },
      error: (err: Error) => {
        this.rowError.set({ id: entity.id, message: err.message || 'Could not delete the entity' });
        this.saving.set(false);
      },
    });
  }

  /** Reloads after a row left the current list, stepping back when it was the page's last. */
  private reloadAfterRemoval(): void {
    const lastOnPage = this.entities().length === 1 && this.page() > 1;
    this.load(lastOnPage ? this.page() - 1 : this.page());
  }

  private filters(): AgentEntityFilters {
    const filters: AgentEntityFilters = { query: this.query() };
    if (this.archived()) filters.archived = true;
    const persona = this.personalityFilter();
    if (persona === GLOBAL_PERSONALITY_FILTER) {
      filters.global_only = true;
    } else if (persona) {
      filters.personality_id = persona;
    }
    return filters;
  }
}

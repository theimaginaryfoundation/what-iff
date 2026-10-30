import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { ConnectionModel, ConnectionModelChoice, ProviderConnection } from '../../../core/models/provider-connection.model';
import { ModelService } from '../../../core/services/model.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { apiErrorMessage, hasErrorCode } from '../../../core/utils/api-error.helpers';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';

/** What the list shows. */
export type ModelFilter = 'all' | 'enabled' | 'tools' | 'images';

/** One of the connection's models while the account chooses. */
export interface ChooserRow {
  id: string;
  name: string;
  enabled: boolean;
  /** Whether it takes tools: confirmed, as reported, or undefined when nothing says. */
  tools: boolean | undefined;
  images: boolean | undefined;
  /** As loaded, to send only what changed. */
  readonly initial: Readonly<{ enabled: boolean; tools: boolean | undefined; images: boolean | undefined }>;
}

/**
 * Chooses which of a connection's models the account uses (ADR 0x027): the
 * provider's live listing, searchable, with what each model takes. Enabled
 * models appear in the model picker. Capabilities come from what the provider
 * reports; where it reports nothing, the account says, so a model is never
 * offered tools or images it can't take.
 *
 * Searching filters the listing already loaded, so typing never calls the
 * server: every word must appear in the model's name or id, in any order.
 */
@Component({
  selector: 'app-model-chooser',
  standalone: true,
  imports: [ModalComponent],
  templateUrl: './model-chooser.component.html',
  styleUrl: './model-chooser.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ModelChooserComponent {
  private readonly connections = inject(ProviderConnectionsService);
  private readonly modelService = inject(ModelService);
  private readonly preferences = inject(UserPreferencesService);

  readonly open = input(false);
  readonly connection = input<ProviderConnection | null>(null);

  /** Saved: the connection's enabled models. */
  readonly saved = output<ConnectionModel[]>();
  readonly closed = output<void>();
  /** The account wants to choose another default model first. */
  readonly changeDefault = output<void>();

  readonly filters: { id: ModelFilter; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'enabled', label: 'Enabled' },
    { id: 'tools', label: 'Tools' },
    { id: 'images', label: 'Images' },
  ];
  readonly capabilities: { id: 'tools' | 'images'; label: string }[] = [
    { id: 'tools', label: 'Tools' },
    { id: 'images', label: 'Images' },
  ];

  readonly loading = signal(false);
  readonly loadError = signal('');
  readonly rows = signal<ChooserRow[]>([]);
  readonly query = signal('');
  readonly filter = signal<ModelFilter>('all');
  readonly saving = signal(false);
  readonly saveError = signal('');
  /** The model that can't be turned off because it is the account's default, after an attempt to. */
  readonly defaultBlocked = signal<string | null>(null);
  /** The account's default model's id on this connection, when it is one of its models. */
  readonly defaultModelId = signal<string | null>(null);

  readonly words = computed(() => this.query().trim().toLowerCase().split(/\s+/).filter(Boolean));
  readonly shown = computed(() => {
    const words = this.words();
    const filter = this.filter();
    return this.rows().filter(
      row =>
        matchesWords(row, words) &&
        (filter === 'all' ||
          (filter === 'enabled' && row.enabled) ||
          (filter === 'tools' && row.tools === true) ||
          (filter === 'images' && row.images === true)),
    );
  });
  readonly enabledCount = computed(() => this.rows().filter(row => row.enabled).length);
  readonly narrowed = computed(() => this.words().length > 0 || this.filter() !== 'all');
  /** Enabled models whose capabilities nothing reported and the account hasn't said. */
  readonly unconfirmed = computed(() => this.rows().some(row => row.enabled && (row.tools === undefined || row.images === undefined)));
  readonly changes = computed(() => choicesFrom(this.rows()));

  readonly emptyText = computed(() => {
    const term = this.query().trim();
    const label = FILTER_LABELS[this.filter()];
    if (!label) {
      return `No models match “${term}”.`;
    }
    return term ? `No models ${label} match “${term}”.` : `No models ${label}.`;
  });

  constructor() {
    effect(() => {
      const connection = this.connection();
      if (this.open() && connection) {
        void this.load(connection);
      }
    });
  }

  async load(connection: ProviderConnection): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    this.saveError.set('');
    this.defaultBlocked.set(null);
    this.query.set('');
    this.filter.set('all');
    try {
      const [listed, prefs, models] = await Promise.all([
        firstValueFrom(this.connections.models(connection.id)),
        firstValueFrom(this.preferences.getUserPreferences()),
        firstValueFrom(this.modelService.getModels()),
      ]);
      const defaultModel = models.find(m => m.id === prefs.default_model_id && m.connection_id === connection.id);
      this.defaultModelId.set(defaultModel?.name ?? null);
      this.rows.set(listed.map(toRow));
    } catch (err) {
      this.rows.set([]);
      this.loadError.set(apiErrorMessage(err, `Couldn't list ${connection.name}'s models.`));
    } finally {
      this.loading.set(false);
    }
  }

  /** The text in parts, marking where search words match, for highlighting without HTML. */
  parts(text: string): TextPart[] {
    return highlightParts(text, this.words());
  }

  setQuery(value: string): void {
    this.query.set(value);
  }

  setFilter(filter: ModelFilter): void {
    this.filter.set(filter);
  }

  /** Clears the search and the filter. */
  showAll(): void {
    this.query.set('');
    this.filter.set('all');
  }

  /** Escape clears a search before it closes anything. */
  onSearchKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && this.query()) {
      event.stopPropagation();
      event.preventDefault();
      this.query.set('');
    }
  }

  toggle(row: ChooserRow, enabled: boolean): void {
    if (!enabled && row.id === this.defaultModelId()) {
      // Refused here rather than at Save, so the box never shows a state that won't save.
      this.defaultBlocked.set(row.name);
      this.patch(row.id, { enabled: true });
      return;
    }
    this.defaultBlocked.set(null);
    this.patch(row.id, { enabled });
  }

  toggleCapability(row: ChooserRow, capability: 'tools' | 'images'): void {
    this.patch(row.id, { [capability]: !(row[capability] ?? false) });
  }

  async save(): Promise<void> {
    const connection = this.connection();
    const choices = this.changes();
    if (!connection) {
      return;
    }
    if (choices.length === 0) {
      this.closed.emit();
      return;
    }
    this.saving.set(true);
    this.saveError.set('');
    try {
      const enabled = await firstValueFrom(this.connections.setModels(connection.id, choices));
      // The picker, and a default the first enabled model may have become.
      await Promise.all([firstValueFrom(this.modelService.getModels()), firstValueFrom(this.preferences.getUserPreferences())]);
      this.saved.emit(enabled);
    } catch (err) {
      if (hasErrorCode(err, 'default_model_in_use')) {
        this.defaultBlocked.set(this.rows().find(row => row.id === this.defaultModelId())?.name ?? 'Your default model');
      } else {
        this.saveError.set(apiErrorMessage(err, "Couldn't save your models. Try again."));
      }
    } finally {
      this.saving.set(false);
    }
  }

  close(): void {
    if (!this.saving()) {
      this.closed.emit();
    }
  }

  private patch(id: string, change: Partial<Pick<ChooserRow, 'enabled' | 'tools' | 'images'>>): void {
    this.rows.update(rows => rows.map(row => (row.id === id ? { ...row, ...change } : row)));
  }
}

const FILTER_LABELS: Record<ModelFilter, string> = {
  all: '',
  enabled: 'enabled',
  tools: 'with tools',
  images: 'that take images',
};

function toRow(model: ConnectionModel): ChooserRow {
  const row = {
    id: model.id,
    name: model.display_name || model.id,
    enabled: model.enabled,
    tools: model.tool_support,
    images: model.vision_support,
  };
  return { ...row, initial: { enabled: row.enabled, tools: row.tools, images: row.images } };
}

/** A piece of text, and whether it matched the search. */
export interface TextPart {
  text: string;
  match: boolean;
}

/** Splits text into matched and unmatched parts, case-insensitively, longest words first. */
export function highlightParts(text: string, words: string[]): TextPart[] {
  if (words.length === 0) {
    return [{ text, match: false }];
  }
  const escaped = [...words].sort((a, b) => b.length - a.length).map(w => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  const pattern = new RegExp(`(${escaped.join('|')})`, 'gi');
  return text
    .split(pattern)
    .filter(part => part !== '')
    .map(part => ({ text: part, match: words.some(w => w === part.toLowerCase()) }));
}

/** Every word in the model's name or id, in any order. */
export function matchesWords(row: Pick<ChooserRow, 'id' | 'name'>, words: string[]): boolean {
  const haystack = `${row.name} ${row.id}`.toLowerCase();
  return words.every(word => haystack.includes(word));
}

/** The changes to send: models switched on or off, and enabled models whose capabilities changed. */
export function choicesFrom(rows: ChooserRow[]): ConnectionModelChoice[] {
  const out: ConnectionModelChoice[] = [];
  for (const row of rows) {
    const changed =
      row.enabled !== row.initial.enabled ||
      (row.enabled && (row.tools !== row.initial.tools || row.images !== row.initial.images));
    if (!changed) {
      continue;
    }
    const choice: ConnectionModelChoice = { id: row.id, enabled: row.enabled };
    if (row.enabled) {
      choice.display_name = row.name;
      if (row.tools !== undefined) {
        choice.tool_support = row.tools;
      }
      if (row.images !== undefined) {
        choice.vision_support = row.images;
      }
    }
    out.push(choice);
  }
  return out;
}

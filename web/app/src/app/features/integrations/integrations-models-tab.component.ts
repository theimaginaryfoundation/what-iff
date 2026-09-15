import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';

import { Model } from '../../core/models/model.model';
import { ModelService } from '../../core/services/model.service';
import { UserPreferences } from '../../core/models/user.model';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { providerLabel } from '../chat/helpers/model-picker.helpers';

interface ModelRow {
  model: Model;
  shown: boolean;
}

@Component({
  selector: 'app-integrations-models-tab',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './integrations-models-tab.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class IntegrationsModelsTabComponent implements OnInit {
  private modelService = inject(ModelService);
  private preferences = inject(UserPreferencesService);

  /** Last-read preferences, spread into updates so required fields survive. */
  private current: UserPreferences | null = null;

  rows = signal<ModelRow[]>([]);
  isLoading = signal(false);
  /** Model id currently being saved, so only its row shows as busy. */
  busyId = signal<string | null>(null);
  errorMessage = signal('');

  /** Rows grouped by provider, matching how the picker presents them. */
  groups = computed(() => {
    const byProvider = new Map<string, ModelRow[]>();
    for (const row of this.rows()) {
      const key = row.model.provider ?? 'other';
      byProvider.set(key, [...(byProvider.get(key) ?? []), row]);
    }
    return [...byProvider.entries()].map(([provider, rows]) => ({
      provider,
      label: providerLabel(provider),
      rows,
    }));
  });

  shownCount = computed(() => this.rows().filter(r => r.shown).length);

  /**
   * Hiding every model would leave the picker empty and no obvious way back
   * except this screen, so the last visible one cannot be unchecked here.
   */
  canHide = computed(() => this.shownCount() > 1);

  ngOnInit(): void {
    this.load();
  }

  load(): void {
    this.isLoading.set(true);
    this.errorMessage.set('');
    // The unfiltered list, so hidden models can still be named and restored.
    this.modelService.getAllModelsIncludingHidden().subscribe({
      next: models => {
        this.preferences.getUserPreferences().subscribe({
          next: prefs => {
            this.current = prefs;
            const hidden = new Set(prefs?.hidden_model_ids ?? []);
            this.rows.set(models.map(model => ({ model, shown: !hidden.has(model.id) })));
            this.isLoading.set(false);
          },
          error: err => this.fail(err, 'Could not load your model preferences.'),
        });
      },
      error: err => this.fail(err, 'Could not load models.'),
    });
  }

  toggle(row: ModelRow): void {
    if (row.shown && !this.canHide()) {
      this.errorMessage.set('Keep at least one model visible.');
      return;
    }
    const next = this.rows().map(r => (r.model.id === row.model.id ? { ...r, shown: !r.shown } : r));
    const hidden = next.filter(r => !r.shown).map(r => r.model.id);

    this.busyId.set(row.model.id);
    this.errorMessage.set('');
    this.preferences.updateUserPreferences({ ...this.current!, hidden_model_ids: hidden }).subscribe({
      next: () => {
        this.rows.set(next);
        this.busyId.set(null);
      },
      error: err => {
        this.busyId.set(null);
        this.fail(err, 'Could not save that change.');
      },
    });
  }

  showAll(): void {
    this.busyId.set('*');
    this.errorMessage.set('');
    this.preferences.updateUserPreferences({ ...this.current!, hidden_model_ids: [] }).subscribe({
      next: () => {
        this.rows.set(this.rows().map(r => ({ ...r, shown: true })));
        this.busyId.set(null);
      },
      error: err => {
        this.busyId.set(null);
        this.fail(err, 'Could not show all models.');
      },
    });
  }

  private fail(err: unknown, fallback: string): void {
    this.isLoading.set(false);
    const body = (err as { error?: unknown })?.error;
    if (typeof body === 'string' && body.trim()) {
      this.errorMessage.set(body);
      return;
    }
    if (body && typeof body === 'object') {
      const message = (body as { error?: string; message?: string }).error ?? (body as { message?: string }).message;
      if (message) {
        this.errorMessage.set(message);
        return;
      }
    }
    this.errorMessage.set(fallback);
  }
}

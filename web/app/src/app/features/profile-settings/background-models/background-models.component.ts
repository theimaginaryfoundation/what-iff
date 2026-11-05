import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { firstValueFrom } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { UtilityModels, UtilityRole, UtilityRoleModel } from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { isConnectionGroup } from '../../chat/helpers/model-picker.helpers';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/** What each role is called and covers, in the words a person would use. */
const ROLE_COPY: Record<UtilityRole, { label: string; covers: string }> = {
  memory: { label: 'Memory and summaries', covers: 'Saves memories, keeps the scratchpad and summarizes long threads' },
  simple: { label: 'Simple tasks', covers: 'Names threads, picks expressions and moods, and reads job schedules' },
  personality: { label: 'Personality generation', covers: 'Writes a new personality from your answers' },
};

interface RoleRow {
  role: UtilityRole;
  label: string;
  covers: string;
  /** The account's choice, or '' for the default. */
  chosen: string;
  /** What the default option says it runs on. */
  defaultLabel: string;
  /** A choice the account can no longer use, so it isn't among the options. */
  chosenUnlisted: boolean;
}

/**
 * The models the account's background work runs on, one per utility role (ADR
 * 0x027 section 6). Each role offers the models the account can use; leaving it
 * on Default runs it on the deployment's choice or its built-in model.
 *
 * Self-contained so any build's settings modal can place it.
 */
@Component({
  selector: 'app-background-models',
  standalone: true,
  imports: [TooltipDirective],
  templateUrl: './background-models.component.html',
  styleUrl: './background-models.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BackgroundModelsComponent implements OnInit {
  private readonly service = inject(UtilityModelsService);
  private readonly modelService = inject(ModelService);

  readonly models = signal<readonly Model[]>([]);
  readonly utility = signal<UtilityModels | null>(null);
  readonly saving = signal<UtilityRole | null>(null);
  readonly error = signal('');

  readonly rows = computed<RoleRow[]>(() => {
    const models = this.models();
    const byId = new Map(models.map(m => [m.id, m]));
    return (this.utility()?.roles ?? []).map(r => this.row(r, byId, models));
  });

  constructor() {
    // The account's models change when models are chosen on the Providers tab.
    this.modelService.models$.pipe(takeUntilDestroyed()).subscribe(models => this.models.set(models));
  }

  async ngOnInit(): Promise<void> {
    try {
      this.utility.set(await firstValueFrom(this.service.get()));
    } catch (err) {
      this.error.set(apiErrorMessage(err, 'Could not load background models.'));
    }
    if (this.models().length === 0) {
      try {
        await firstValueFrom(this.modelService.getModels());
      } catch {
        // The selects still offer Default; the error above covers a failed load of the roles.
      }
    }
  }

  modelLabel(model: Model): string {
    return isConnectionGroup(model) && model.connection_name ? `${model.display_name} · ${model.connection_name}` : model.display_name;
  }

  async onChange(row: RoleRow, select: HTMLSelectElement): Promise<void> {
    this.saving.set(row.role);
    this.error.set('');
    try {
      const next = select.value ? this.service.choose(row.role, select.value) : this.service.clear(row.role);
      this.utility.set(await firstValueFrom(next));
    } catch (err) {
      this.error.set(apiErrorMessage(err, 'Could not save that choice.'));
      // Nothing changed, so the select goes back to what is saved.
      select.value = row.chosen;
    } finally {
      this.saving.set(null);
    }
  }

  private row(r: UtilityRoleModel, byId: Map<string, Model>, models: readonly Model[]): RoleRow {
    const fallback = r.deployment_model_id ? byId.get(r.deployment_model_id) : models.find(m => m.name === r.built_in_model);
    const fallbackName = fallback?.display_name ?? r.built_in_model;
    return {
      role: r.role,
      ...ROLE_COPY[r.role],
      chosen: r.account_model_id ?? '',
      defaultLabel: `Default (${fallbackName})`,
      chosenUnlisted: !!r.account_model_id && !byId.has(r.account_model_id),
    };
  }
}

import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Observable, firstValueFrom } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { ReasoningEffort, TurnSettingOption, parseReasoningEffort, reasoningEffortOptions } from '../../../core/models/turn-settings.model';
import {
  UtilityChoice,
  UtilityModels,
  UtilityRole,
  UtilityRoleModel,
  UtilityServiceTier,
} from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { apiErrorMessage, hasErrorCode } from '../../../core/utils/api-error.helpers';
import { UTILITY_ROLE_COPY } from '../../../core/utils/utility-role-copy';
import { isConnectionGroup } from '../../chat/helpers/model-picker.helpers';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/** The model select's value for a role switched off. */
export const OFF_VALUE = 'off';

/** What the tier select offers, after the API's default. */
export const SERVICE_TIER_OPTIONS: readonly TurnSettingOption<UtilityServiceTier>[] = [
  { value: 'priority', label: 'Priority: faster, costs more' },
  { value: 'flex', label: 'Flex: slower, costs less' },
];

interface RoleRow {
  role: UtilityRole;
  label: string;
  covers: string;
  /** What stops while the role is off or has no model. */
  stops: string;
  /** The account's choice: a model's id, OFF_VALUE, or '' when not chosen. */
  chosen: string;
  /** A chosen model the account can no longer use, so it isn't among the options. */
  chosenUnlisted: boolean;
  /** The chosen model, when it is among the account's. */
  model: Model | null;
  effort: ReasoningEffort | null;
  tier: UtilityServiceTier | null;
  /** The effort levels the chosen model takes; empty hides the control. */
  effortOptions: TurnSettingOption<ReasoningEffort>[];
  /** Whether the chosen model takes a tier. */
  offersTier: boolean;
  /** The account chose nothing, and the deployment's choice stands in. */
  deploymentChooses: boolean;
}

/**
 * Whether a model takes a service tier: only OpenAI's own Responses API does, so a catalog OpenAI
 * model. The server checks the same and refuses a tier anywhere else.
 */
export function offersServiceTier(model: Model | null): boolean {
  return !!model && !model.connection_id && model.provider === 'openai';
}

/**
 * The models the account's background work runs on, one per utility role (ADR 0x027
 * section 6). Each role takes one of the account's models, with how hard it thinks and, where
 * the model offers one, its service tier; or is switched off. A role with nothing chosen
 * doesn't run, and says so. A choice is replaced, never cleared. Shown only where accounts
 * choose, which is where they bring their own keys.
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

  readonly offValue = OFF_VALUE;
  readonly tierOptions = SERVICE_TIER_OPTIONS;
  readonly models = signal<readonly Model[]>([]);
  /** Whether the account's models have loaded, so a choice missing from them really is gone. */
  readonly modelsLoaded = signal(false);
  /** The account's models couldn't be loaded, so only Off can be offered. */
  readonly modelsFailed = signal(false);
  readonly utility = signal<UtilityModels | null>(null);
  readonly saving = signal<UtilityRole | null>(null);
  readonly error = signal('');

  readonly rows = computed<RoleRow[]>(() => {
    const byId = new Map(this.models().map(m => [m.id, m]));
    return (this.utility()?.roles ?? []).map(r => this.row(r, byId));
  });

  constructor() {
    // The account's models change when models are chosen on the Providers tab.
    this.modelService.models$.pipe(takeUntilDestroyed()).subscribe(models => {
      this.models.set(models);
      if (models.length > 0) this.modelsLoaded.set(true);
    });
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
        this.modelsLoaded.set(true);
      } catch {
        this.modelsFailed.set(true);
      }
    }
  }

  modelLabel(model: Model): string {
    return isConnectionGroup(model) && model.connection_name ? `${model.display_name} · ${model.connection_name}` : model.display_name;
  }

  /**
   * A new model, or Off. A model keeps the effort and tier set before, where it takes them. The
   * placeholder and an unavailable choice are what is saved already, so picking them changes
   * nothing.
   */
  async onModelChange(row: RoleRow, select: HTMLSelectElement): Promise<void> {
    if (!select.value || select.value === row.chosen) return;
    if (select.value === OFF_VALUE) {
      return this.save(row, select, this.service.switchOff(row.role));
    }
    const model = this.models().find(m => m.id === select.value) ?? null;
    const effort = row.effort && reasoningEffortOptions(model?.reasoning_support).some(o => o.value === row.effort) ? row.effort : null;
    return this.save(
      row,
      select,
      this.service.choose(row.role, {
        model_id: select.value,
        reasoning_effort: effort,
        service_tier: offersServiceTier(model) ? row.tier : null,
      }),
    );
  }

  onEffortChange(row: RoleRow, select: HTMLSelectElement): Promise<void> {
    return this.save(
      row,
      select,
      this.service.choose(row.role, { model_id: row.chosen, reasoning_effort: parseReasoningEffort(select.value), service_tier: row.tier }),
    );
  }

  onTierChange(row: RoleRow, select: HTMLSelectElement): Promise<void> {
    const tier = SERVICE_TIER_OPTIONS.find(o => o.value === select.value)?.value ?? null;
    return this.save(row, select, this.service.choose(row.role, { model_id: row.chosen, reasoning_effort: row.effort, service_tier: tier }));
  }

  private async save(row: RoleRow, select: HTMLSelectElement, write: Observable<UtilityModels>): Promise<void> {
    const before = this.selectedBefore(row, select);
    this.saving.set(row.role);
    this.error.set('');
    try {
      this.utility.set(await firstValueFrom(write));
    } catch (err) {
      const message = apiErrorMessage(err, 'Could not save that choice.');
      // The API says which key is missing; the fix is this app's to say.
      this.error.set(
        hasErrorCode(err, 'provider_not_configured') ? `${message} Add one on the Providers tab, or choose another model.` : message,
      );
      // Nothing changed, so the select goes back to what is saved.
      select.value = before;
    } finally {
      this.saving.set(null);
    }
  }

  /** What a select showed before the person changed it, from the saved row. */
  private selectedBefore(row: RoleRow, select: HTMLSelectElement): string {
    switch (select.dataset['control']) {
      case 'effort':
        return row.effort ?? '';
      case 'tier':
        return row.tier ?? '';
      default:
        return row.chosen;
    }
  }

  private row(r: UtilityRoleModel, byId: Map<string, Model>): RoleRow {
    const choice: UtilityChoice | null = r.account;
    const model = choice?.model_id ? (byId.get(choice.model_id) ?? null) : null;
    return {
      role: r.role,
      ...UTILITY_ROLE_COPY[r.role],
      chosen: choice?.off ? OFF_VALUE : (choice?.model_id ?? ''),
      chosenUnlisted: this.modelsLoaded() && !!choice?.model_id && !model,
      model,
      effort: choice?.reasoning_effort ?? null,
      tier: choice?.service_tier ?? null,
      effortOptions: reasoningEffortOptions(model?.reasoning_support),
      offersTier: offersServiceTier(model),
      deploymentChooses: !choice && !!r.deployment,
    };
  }
}

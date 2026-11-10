import { ChangeDetectionStrategy, Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { firstValueFrom } from 'rxjs';

import { Model } from '../../core/models/model.model';
import { UtilityModels, UtilityRole, UtilityRoleModel } from '../../core/models/utility-model.model';
import { ModelService } from '../../core/services/model.service';
import { UtilityModelsService } from '../../core/services/utility-models.service';
import { apiErrorMessage, hasErrorCode } from '../../core/utils/api-error.helpers';
import { UTILITY_ROLE_COPY } from '../../core/utils/utility-role-copy';
import { OFF_VALUE, backgroundModelLabel } from '../profile-settings/background-models/background-models.component';

/** Whether a role has nothing chosen at any scope, so its work doesn't run. */
export function roleNotSet(role: UtilityRoleModel): boolean {
  return !role.account && !role.deployment;
}

/**
 * The first-run step that chooses the models background work runs on (ADR 0x027 section 6):
 * one select per role with nothing chosen, pre-filled with the account's default chat model,
 * offering the account's models or Off. Thinking and speed stay in Settings. `save()` writes
 * every row and emits the roles after it; the host's button calls it.
 */
@Component({
  selector: 'app-background-models-step',
  standalone: true,
  templateUrl: './background-models-step.component.html',
  styleUrl: './background-models-step.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BackgroundModelsStepComponent implements OnInit {
  private readonly service = inject(UtilityModelsService);
  private readonly modelService = inject(ModelService);

  /** Every role, as the server reports them; the step shows the ones not set. */
  readonly utility = input.required<UtilityModels>();
  /** The account's default chat model, which each row starts on. */
  readonly defaultModelId = input.required<string>();
  /** The roles after every row is saved. */
  readonly saved = output<UtilityModels>();

  readonly offValue = OFF_VALUE;
  readonly modelLabel = backgroundModelLabel;
  readonly models = signal<readonly Model[]>([]);
  /** What each row is set to, where the person changed it: a model's id or OFF_VALUE. */
  private readonly picked = signal<Partial<Record<UtilityRole, string>>>({});
  readonly saving = signal(false);
  readonly error = signal('');

  readonly rows = computed(() => {
    const models = this.models();
    // The default model, or the first of the account's if the default isn't among them.
    const start = models.some(m => m.id === this.defaultModelId()) ? this.defaultModelId() : (models[0]?.id ?? OFF_VALUE);
    const picked = this.picked();
    return this.utility()
      .roles.filter(roleNotSet)
      .map(r => ({ role: r.role, ...UTILITY_ROLE_COPY[r.role], value: picked[r.role] ?? start }));
  });

  constructor() {
    this.modelService.models$.pipe(takeUntilDestroyed()).subscribe(models => this.models.set(models));
  }

  async ngOnInit(): Promise<void> {
    if (this.models().length === 0) {
      try {
        await firstValueFrom(this.modelService.getModels());
      } catch {
        this.error.set("Your models couldn't be loaded. Close this and try again in a moment.");
      }
    }
  }

  pick(role: UtilityRole, select: HTMLSelectElement): void {
    this.picked.update(p => ({ ...p, [role]: select.value }));
  }

  /** Saves every row in turn; on a failure, says why and stops, so what was saved stays saved. */
  async save(): Promise<void> {
    if (this.saving()) return;
    this.saving.set(true);
    this.error.set('');
    let after: UtilityModels | null = null;
    try {
      for (const row of this.rows()) {
        const write = row.value === OFF_VALUE ? this.service.switchOff(row.role) : this.service.choose(row.role, { model_id: row.value });
        after = await firstValueFrom(write);
      }
      if (after) this.saved.emit(after);
    } catch (err) {
      const message = apiErrorMessage(err, 'Could not save your background models.');
      this.error.set(hasErrorCode(err, 'provider_not_configured') ? `${message} Choose another model, or add the key in Settings.` : message);
    } finally {
      this.saving.set(false);
    }
  }
}

import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';

import {
  AgentJobParameter,
  AgentJobParameterValues,
} from '../../../../core/models/agent-job.model';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { HelpHintComponent } from '../../../../shared/ui/help-hint/help-hint.component';
import {
  analyzeParameters,
  hasParameterErrors,
  promptPlaceholders,
  renamePlaceholder,
} from '../../helpers/job-params.helpers';
import { JobParametersBuilderComponent, ParameterRename } from './job-parameters-builder.component';
import { JobParametersFormComponent } from './job-parameters-form.component';

/**
 * The Parameters section of the job form (ADR 0x044). It shows the values form by default;
 * "Edit parameters" switches to the builder, and "Done" (enabled once there are no errors)
 * switches back. Definitions and values are part of the job form's draft and are saved with it.
 */
@Component({
  selector: 'app-job-parameters',
  standalone: true,
  imports: [HelpHintComponent, JobParametersBuilderComponent, JobParametersFormComponent],
  templateUrl: './job-parameters.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobParametersComponent {
  private readonly confirmation = inject(ConfirmationService);

  readonly parameters = input<AgentJobParameter[]>([]);
  readonly values = input<AgentJobParameterValues>({});
  readonly prompt = input('');
  readonly disabled = input(false);

  readonly parametersChange = output<AgentJobParameter[]>();
  readonly valuesChange = output<AgentJobParameterValues>();
  readonly promptChange = output<string>();

  readonly mode = signal<'form' | 'builder'>('form');
  readonly analysis = computed(() => analyzeParameters(this.prompt(), this.parameters()));
  readonly hasErrors = computed(() => hasParameterErrors(this.analysis()));

  edit(): void {
    this.mode.set('builder');
  }

  done(): void {
    if (!this.hasErrors()) this.mode.set('form');
  }

  /** A renamed definition keeps its value, and may take its placeholders in the prompt with it. */
  async onRenamed({ from, to }: ParameterRename): Promise<void> {
    const values = this.values();
    if (from in values && !(to in values)) {
      const next = { ...values, [to]: values[from] };
      delete next[from];
      this.valuesChange.emit(next);
    }
    if (!promptPlaceholders(this.prompt()).includes(from)) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Rename in the prompt too?',
      message: `The prompt uses {{${from}}}. Change it to {{${to}}} as well?`,
      type: 'info',
      confirmText: 'Rename in prompt',
      cancelText: 'Keep the prompt',
    });
    if (confirmed) this.promptChange.emit(renamePlaceholder(this.prompt(), from, to));
  }
}

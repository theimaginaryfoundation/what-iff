import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

import { ChatTurnAdvancedProgress } from '../../../../core/models/job.model';
import { AdvancedRunPlanComponent } from './advanced-run-plan.component';

export { PLAN_STATUS_DISPLAY, type PlanStatusDisplay } from './advanced-run-plan.component';

/**
 * An in-flight Advanced turn's step and plan checklist (ADR 0x026), shown with the live tool
 * timeline. Display-only; the plan is whatever the model last wrote with `update_plan`.
 */
@Component({
  selector: 'app-advanced-run-progress',
  standalone: true,
  imports: [AdvancedRunPlanComponent],
  template: `
    <div class="advanced-run">
      <p class="advanced-run__step" role="status">
        Step {{ progress().step }}
        @if (summary(); as text) {
          <span class="advanced-run__summary"> · {{ text }}</span>
        }
      </p>
      <app-advanced-run-plan [plan]="progress().plan" />
    </div>
  `,
  styles: [
    `
      .advanced-run {
        background: color-mix(in srgb, var(--color-surface-muted) 64%, var(--color-surface-base));
        border: 1px solid var(--color-border-base);
        border-radius: 0.5rem;
        color: var(--color-text-secondary);
        font-size: 0.75rem;
        margin-bottom: 0.375rem;
        padding: 0.4375rem 0.75rem;
        width: min(100%, 35rem);
      }

      .advanced-run__step {
        color: var(--color-text-muted);
        font-size: 0.6875rem;
        font-weight: 600;
        letter-spacing: 0.02em;
        margin: 0;
      }

      .advanced-run__summary {
        font-weight: 400;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AdvancedRunProgressComponent {
  readonly progress = input.required<ChatTurnAdvancedProgress>();

  /** "2 of 5 done" once there is a plan; empty otherwise. */
  readonly summary = computed(() => {
    const plan = this.progress().plan.filter(item => item.status !== 'cancelled');
    if (plan.length === 0) return '';
    const done = plan.filter(item => item.status === 'completed').length;
    return `${done} of ${plan.length} done`;
  });
}

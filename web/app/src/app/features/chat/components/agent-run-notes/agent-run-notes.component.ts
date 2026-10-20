import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { AgentRunView } from '../../helpers/agent-run.helpers';
import { AdvancedRunPlanComponent } from '../advanced-run-progress/advanced-run-plan.component';

/**
 * A saved Advanced run's working notes (ADR 0x026), shown inside the message bubble's collapsed
 * reasoning disclosure: each step's notes (reasoning muted, text the model wrote in the body
 * colour), then the plan as it stood at the end, then why the run stopped if it ended early.
 */
@Component({
  selector: 'app-agent-run-notes',
  standalone: true,
  imports: [AdvancedRunPlanComponent],
  template: `
    @for (step of run().steps; track step.step) {
      <section class="agent-run__step" [attr.aria-label]="'Step ' + step.step">
        <p class="agent-run__step-title">Step {{ step.step }}</p>
        @for (note of step.notes; track $index) {
          <p class="agent-run__note" [class]="'agent-run__note--' + note.kind">{{ note.text }}</p>
        }
      </section>
    }
    @if (run().plan.length > 0) {
      <section class="agent-run__plan" aria-label="Plan">
        <p class="agent-run__step-title">Plan</p>
        <app-advanced-run-plan [plan]="run().plan" />
      </section>
    }
    @if (run().stopLabel; as label) {
      <p class="agent-run__stop">{{ label }}</p>
    }
  `,
  styles: [
    `
      :host {
        display: grid;
        gap: 0.5rem;
        white-space: normal;
      }

      .agent-run__step {
        display: grid;
        gap: 0.25rem;
      }

      .agent-run__step-title {
        color: var(--color-text-muted);
        font-size: 0.6875rem;
        font-weight: 600;
        letter-spacing: 0.02em;
        margin: 0;
      }

      .agent-run__note {
        margin: 0;
        white-space: pre-wrap;
      }

      .agent-run__note--reasoning {
        color: var(--color-text-secondary);
      }

      .agent-run__note--text {
        color: var(--color-text-primary);
      }

      .agent-run__stop {
        color: var(--color-text-muted);
        font-style: italic;
        margin: 0;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AgentRunNotesComponent {
  readonly run = input.required<AgentRunView>();
}

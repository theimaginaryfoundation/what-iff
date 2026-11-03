import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { ToolCall } from '../../../../core/models/toolcall.model';
import { AgentRunView } from '../../helpers/agent-run.helpers';
import { AdvancedRunPlanComponent } from '../advanced-run-progress/advanced-run-plan.component';

/**
 * A saved Advanced run's working notes (ADR 0x026), shown inside the message bubble's collapsed
 * reasoning disclosure: each step's events in order (reasoning muted, text the model wrote in the
 * body colour, tool calls as a compact name with a failure marker that opens the call's details; the
 * message's tool-call list leaves these calls out), then the plan as it stood at the end, then why the run stopped if it ended early.
 */
@Component({
  selector: 'app-agent-run-notes',
  standalone: true,
  imports: [AdvancedRunPlanComponent],
  template: `
    @for (step of run().steps; track step.step) {
      <section class="agent-run__step" [attr.aria-label]="'Step ' + step.step">
        <p class="agent-run__step-title">Step {{ step.step }}</p>
        @for (event of step.events; track $index) {
          @if (event.kind === 'tool_call') {
            <button
              type="button"
              class="agent-run__tool"
              [class.agent-run__tool--error]="event.failed"
              [attr.aria-label]="'Open tool call ' + event.name + (event.failed ? ' (failed)' : '')"
              (click)="openToolCall.emit(event.toolCall)"
            >
              <span class="agent-run__tool-dot" aria-hidden="true"></span>
              <span class="agent-run__tool-name">{{ event.name }}</span>
              @if (event.failed) {
                <span class="agent-run__tool-error">failed</span>
              }
            </button>
          } @else {
            <p class="agent-run__note" [class]="'agent-run__note--' + event.kind">{{ event.text }}</p>
          }
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

      .agent-run__tool {
        align-items: center;
        background: none;
        border: none;
        border-radius: 0.25rem;
        color: var(--color-text-muted);
        cursor: pointer;
        display: flex;
        font: inherit;
        font-size: 0.6875rem;
        gap: 0.375rem;
        justify-self: start;
        margin: 0 0 0 -0.25rem;
        padding: 0.125rem 0.25rem;
        text-align: start;
      }

      .agent-run__tool:hover {
        background: var(--color-surface-muted);
      }

      .agent-run__tool:focus-visible {
        outline: 2px solid var(--focus-ring);
        outline-offset: 1px;
      }

      .agent-run__tool-dot {
        background: var(--color-success, #22c55e);
        border-radius: 999px;
        flex-shrink: 0;
        height: 0.375rem;
        width: 0.375rem;
      }

      .agent-run__tool--error .agent-run__tool-dot {
        background: var(--color-danger, #ef4444);
      }

      .agent-run__tool-name {
        color: var(--color-text-primary);
        font-weight: 600;
      }

      .agent-run__tool-error {
        color: var(--color-danger, #ef4444);
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
  readonly openToolCall = output<ToolCall>();
}

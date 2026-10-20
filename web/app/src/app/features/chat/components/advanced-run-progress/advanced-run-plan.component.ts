import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { ChatTurnPlanItem, ChatTurnPlanStatus } from '../../../../core/models/job.model';

/** How one plan item status is shown: a visible glyph and the words a screen reader hears instead. */
export interface PlanStatusDisplay {
  /** Decorative glyph shown before the item; rendered `aria-hidden`. */
  readonly marker: string;
  /** Visually hidden prefix read before the item, standing in for the glyph. */
  readonly label: string;
}

/**
 * The marker and screen-reader label for each `update_plan` item status (ADR 0x026). The glyphs
 * read as a progress circle filling up, so the checklist scans at a glance without relying on
 * colour. Statuses are validated before they get here (`normalizePlanItems`).
 */
export const PLAN_STATUS_DISPLAY: Readonly<Record<ChatTurnPlanStatus, PlanStatusDisplay>> = {
  /** Not started yet: an empty circle. */
  pending: { marker: '○', label: 'To do' },
  /** The step the model is working on now: a half-filled circle (also bold + accent-coloured). */
  in_progress: { marker: '◐', label: 'In progress' },
  /** Finished: a filled circle (content muted). */
  completed: { marker: '●', label: 'Done' },
  /** Dropped from the plan: a struck-through circle (content muted and struck through). */
  cancelled: { marker: '⊘', label: 'Dropped' },
};

/**
 * An Advanced run's plan checklist (ADR 0x026): the live turn's progress box and a saved
 * message's working notes both show it. Renders nothing for an empty plan.
 */
@Component({
  selector: 'app-advanced-run-plan',
  standalone: true,
  template: `
    @if (plan().length > 0) {
      <ul class="advanced-run__plan" aria-label="Plan">
        @for (item of plan(); track $index) {
          <li class="advanced-run__item" [class]="'advanced-run__item--' + item.status">
            <span class="advanced-run__marker" aria-hidden="true">{{ statusMarker(item.status) }}</span>
            <span class="sr-only">{{ statusLabel(item.status) + ': ' }}</span>
            <span class="advanced-run__content">{{ item.content }}</span>
          </li>
        }
      </ul>
    }
  `,
  styles: [
    `
      :host {
        display: block;
      }

      .advanced-run__plan {
        display: grid;
        gap: 0.125rem;
        list-style: none;
        margin: 0.375rem 0 0;
        padding: 0;
      }

      .advanced-run__item {
        align-items: baseline;
        display: flex;
        gap: 0.5rem;
      }

      .advanced-run__marker {
        flex-shrink: 0;
        width: 0.75rem;
      }

      .advanced-run__item--in_progress {
        color: var(--color-text-primary);
        font-weight: 600;
      }

      .advanced-run__item--in_progress .advanced-run__marker {
        color: var(--color-accent);
      }

      .advanced-run__item--completed .advanced-run__content,
      .advanced-run__item--cancelled .advanced-run__content {
        color: var(--color-text-muted);
      }

      .advanced-run__item--cancelled .advanced-run__content {
        text-decoration: line-through;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AdvancedRunPlanComponent {
  readonly plan = input.required<readonly ChatTurnPlanItem[]>();

  statusLabel(status: ChatTurnPlanStatus): string {
    return PLAN_STATUS_DISPLAY[status].label;
  }

  statusMarker(status: ChatTurnPlanStatus): string {
    return PLAN_STATUS_DISPLAY[status].marker;
  }
}

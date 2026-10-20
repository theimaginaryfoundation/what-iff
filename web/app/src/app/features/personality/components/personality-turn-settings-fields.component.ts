import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import {
  AGENT_LOOP_DESCRIPTIONS,
  AGENT_LOOP_OPTIONS,
  AgentLoop,
  REASONING_EFFORT_DESCRIPTION,
  REASONING_EFFORT_OPTIONS,
  ReasoningEffort,
  parseAgentLoop,
  parseReasoningEffort,
} from '../../../core/models/turn-settings.model';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/**
 * The personality's reasoning effort and agent loop (ADR 0x026), shared by the compact edit modal
 * and the full-page editor. They seed new threads with this personality, over the account default;
 * null ("Use account default") leaves it to the account.
 */
@Component({
  selector: 'app-personality-turn-settings-fields',
  standalone: true,
  imports: [TooltipDirective],
  template: `
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="flex min-w-0 flex-col gap-1">
        <span class="text-xs font-semibold text-(--color-text-secondary)">Reasoning effort</span>
        <select
          class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
          [disabled]="disabled()"
          [uiTooltip]="reasoningEffortDescription"
          (change)="reasoningEffortChange.emit(parseEffort($any($event.target).value))"
        >
          <option value="" [selected]="!reasoningEffort()">Use account default</option>
          @for (option of reasoningEffortOptions; track option.value) {
            <option [value]="option.value" [selected]="option.value === reasoningEffort()">{{ option.label }}</option>
          }
        </select>
      </label>
      <label class="flex min-w-0 flex-col gap-1">
        <span class="text-xs font-semibold text-(--color-text-secondary)">Agent loop</span>
        <select
          class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
          [disabled]="disabled()"
          [uiTooltip]="agentLoopDescriptions.advanced"
          (change)="agentLoopChange.emit(parseLoop($any($event.target).value))"
        >
          <option value="" [selected]="!agentLoop()">Use account default</option>
          @for (option of agentLoopOptions; track option.value) {
            <option [value]="option.value" [selected]="option.value === agentLoop()">{{ option.label }}</option>
          }
        </select>
      </label>
    </div>
    <p class="mt-1 text-[11px] text-(--color-text-muted)">
      New threads with this personality start with these; each thread can change them later.
    </p>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityTurnSettingsFieldsComponent {
  readonly reasoningEffort = input<ReasoningEffort | null>(null);
  readonly agentLoop = input<AgentLoop | null>(null);
  readonly disabled = input(false);

  readonly reasoningEffortChange = output<ReasoningEffort | null>();
  readonly agentLoopChange = output<AgentLoop | null>();

  readonly reasoningEffortOptions = REASONING_EFFORT_OPTIONS;
  readonly reasoningEffortDescription = REASONING_EFFORT_DESCRIPTION;
  readonly agentLoopOptions = AGENT_LOOP_OPTIONS;
  readonly agentLoopDescriptions = AGENT_LOOP_DESCRIPTIONS;
  readonly parseEffort = parseReasoningEffort;
  readonly parseLoop = parseAgentLoop;
}

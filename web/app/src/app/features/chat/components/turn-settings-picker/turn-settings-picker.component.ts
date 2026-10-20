/*
 * Design note: this component deliberately implements a lightweight popover by hand rather than
 * using an overlay library (e.g. CDK Overlay). The panel is an absolutely positioned child of the
 * trigger's wrapper; `document:click` outside the host and `document:keydown.escape` close it
 * (Escape also returns focus to the trigger); each instance gets a random `panelId` so
 * `aria-controls` and the radio-group names stay unique when several pickers render; and the panel
 * is a `role="dialog"` the trigger announces via `aria-haspopup="dialog"` / `aria-expanded`.
 * It keeps the composer free of an overlay dependency for one small menu. If this ever needs
 * viewport-aware repositioning, scroll handling or a focus trap, move it onto CDK Overlay and keep
 * these behaviours (outside click, Escape + focus return, unique ids, dialog semantics).
 */
import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, input, output, signal } from '@angular/core';

import {
  AGENT_LOOP_DESCRIPTIONS,
  AGENT_LOOP_LABELS,
  AGENT_LOOP_OPTIONS,
  AgentLoop,
  REASONING_EFFORT_DESCRIPTION,
  ReasoningEffort,
  ReasoningSupport,
  TurnSettingValues,
  effectiveReasoningEffort,
  reasoningEffortLabel,
  reasoningEffortOptions,
  resolveEffectiveTurnSettings,
} from '../../../../core/models/turn-settings.model';
import { BrainIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';
import { MODE_SINGULAR } from '../../../mood/mode-ui.labels';

/** The active mode as far as turn settings go: its name and the values it overrides. */
export type TurnSettingsMode = TurnSettingValues & { name: string };

/**
 * Composer control for the thread's reasoning effort and agent loop (ADR 0x026). The trigger
 * shows what the next turn uses (the active mode's value over the thread's); the popover edits
 * the thread's own values. The reasoning part is hidden when the model has no effort control.
 */
@Component({
  selector: 'app-turn-settings-picker',
  standalone: true,
  imports: [BrainIconComponent, TooltipDirective],
  template: `
    <div class="turn-settings">
      <button
        type="button"
        class="turn-settings__trigger"
        [class.turn-settings__trigger--active]="highlighted()"
        aria-haspopup="dialog"
        [attr.aria-expanded]="open()"
        [attr.aria-controls]="open() ? panelId : null"
        [attr.aria-label]="triggerAriaLabel()"
        [disabled]="disabled()"
        [uiTooltip]="triggerTooltip()"
        (click)="toggle()"
      >
        <ui-brain-icon [size]="12" />
        <span class="turn-settings__trigger-label">{{ triggerLabel() }}</span>
        @if (setByMode()) {
          <span class="turn-settings__trigger-mode">· set by {{ modeNoun }}</span>
        }
      </button>
      @if (open()) {
        <div [id]="panelId" class="turn-settings__panel" role="dialog" aria-label="Thread settings">
          @if (effortOptions().length > 0) {
            <fieldset class="turn-settings__group">
              <legend class="turn-settings__legend">Reasoning</legend>
              <p class="turn-settings__help">{{ effortDescription }}</p>
              @if (resolved().reasoningEffort.source === 'mode') {
                <p class="turn-settings__mode-note" [uiTooltip]="modeOverrideTooltip()">
                  {{ modeName() }} {{ modeNoun }} sets {{ effectiveEffortLabel() }} while it's active.
                </p>
              }
              <label class="turn-settings__option">
                <input type="radio" [name]="panelId + '-effort'" [checked]="threadEffortChoice() === null" (change)="pickEffort(null)" />
                <span>Model default</span>
              </label>
              @for (option of effortOptions(); track option.value) {
                <label class="turn-settings__option">
                  <input
                    type="radio"
                    [name]="panelId + '-effort'"
                    [checked]="threadEffortChoice() === option.value"
                    (change)="pickEffort(option.value)"
                  />
                  <span>{{ option.label }}</span>
                </label>
              }
            </fieldset>
          }
          <fieldset class="turn-settings__group">
            <legend class="turn-settings__legend">Agent loop</legend>
            @if (resolved().agentLoop.source === 'mode') {
              <p class="turn-settings__mode-note" [uiTooltip]="modeOverrideTooltip()">
                {{ modeName() }} {{ modeNoun }} sets {{ effectiveLoopLabel() }} while it's active.
              </p>
            }
            @for (option of loopOptions; track option.value) {
              <label class="turn-settings__option turn-settings__option--described">
                <input
                  type="radio"
                  [name]="panelId + '-loop'"
                  [checked]="(threadAgentLoop() ?? 'standard') === option.value"
                  (change)="pickLoop(option.value)"
                />
                <span>
                  <span class="turn-settings__option-label">{{ option.label }}</span>
                  <span class="turn-settings__option-desc">{{ loopDescriptions[option.value] }}</span>
                </span>
              </label>
            }
          </fieldset>
        </div>
      }
    </div>
  `,
  styles: [
    `
      .turn-settings {
        position: relative;
      }

      .turn-settings__trigger {
        align-items: center;
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 0.5rem;
        box-sizing: border-box;
        color: var(--color-text-secondary);
        cursor: pointer;
        display: inline-flex;
        font-size: 0.6875rem;
        gap: 0.25rem;
        height: 2.25rem;
        line-height: 1;
        max-width: 12rem;
        padding: 0 0.5rem;
        white-space: nowrap;
      }

      .turn-settings__trigger--active {
        border-color: color-mix(in srgb, var(--color-accent) 45%, var(--color-border-base));
        color: var(--color-text-primary);
      }

      .turn-settings__trigger-label,
      .turn-settings__trigger-mode {
        overflow: hidden;
        text-overflow: ellipsis;
      }

      .turn-settings__trigger-mode {
        color: var(--color-text-muted);
      }

      .turn-settings__trigger:disabled {
        cursor: not-allowed;
        opacity: 0.5;
      }

      .turn-settings__panel {
        background: var(--color-surface-elevated, var(--color-surface-base));
        border: 1px solid var(--color-border-base);
        border-radius: 0.5rem;
        bottom: calc(100% + 0.375rem);
        box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
        display: flex;
        flex-direction: column;
        gap: 0.5rem;
        left: 0;
        max-height: min(26rem, 60vh);
        overflow-y: auto;
        padding: 0.625rem;
        position: absolute;
        width: min(18rem, calc(100vw - 1.5rem));
        z-index: 58;
      }

      .turn-settings__group {
        border: 0;
        display: grid;
        gap: 0.25rem;
        margin: 0;
        min-width: 0;
        padding: 0;
      }

      .turn-settings__group + .turn-settings__group {
        border-top: 1px solid var(--color-border-base);
        padding-top: 0.5rem;
      }

      .turn-settings__legend {
        color: var(--color-text-primary);
        font-size: 0.75rem;
        font-weight: 700;
        padding: 0;
      }

      .turn-settings__help,
      .turn-settings__mode-note {
        color: var(--color-text-muted);
        font-size: 0.6875rem;
        line-height: 1.4;
        margin: 0;
      }

      .turn-settings__mode-note {
        background: color-mix(in srgb, var(--color-accent) 10%, var(--color-surface-base));
        border-radius: 0.375rem;
        color: var(--color-text-secondary);
        padding: 0.25rem 0.5rem;
      }

      .turn-settings__option {
        align-items: center;
        border-radius: 0.375rem;
        color: var(--color-text-secondary);
        cursor: pointer;
        display: flex;
        font-size: 0.75rem;
        gap: 0.5rem;
        padding: 0.25rem 0.375rem;
      }

      .turn-settings__option--described {
        align-items: flex-start;
      }

      .turn-settings__option:hover {
        background: var(--color-surface-muted);
      }

      .turn-settings__option input {
        accent-color: var(--color-accent);
        margin: 0.125rem 0 0;
      }

      .turn-settings__option-label {
        color: var(--color-text-primary);
        display: block;
        font-weight: 600;
      }

      .turn-settings__option-desc {
        color: var(--color-text-muted);
        display: block;
        font-size: 0.6875rem;
        line-height: 1.4;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TurnSettingsPickerComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  /** The thread's own values (what the popover edits). */
  readonly threadReasoningEffort = input<ReasoningEffort | null>(null);
  readonly threadAgentLoop = input<AgentLoop | null>(null);
  /** The active mode, when one is; its values override the thread's while it is active. */
  readonly activeMode = input<TurnSettingsMode | null>(null);
  /** The selected model's effort support; `none` or unknown hides the reasoning part. */
  readonly reasoningSupport = input<ReasoningSupport | null>(null);
  readonly disabled = input(false);

  readonly reasoningEffortChange = output<ReasoningEffort | null>();
  readonly agentLoopChange = output<AgentLoop | null>();

  readonly open = signal(false);
  readonly panelId = `turn-settings-${Math.random().toString(36).slice(2, 10)}`;
  readonly modeNoun = MODE_SINGULAR.toLowerCase();
  readonly effortDescription = REASONING_EFFORT_DESCRIPTION;
  readonly loopOptions = AGENT_LOOP_OPTIONS;
  readonly loopDescriptions = AGENT_LOOP_DESCRIPTIONS;

  readonly resolved = computed(() =>
    resolveEffectiveTurnSettings({ reasoning_effort: this.threadReasoningEffort(), agent_loop: this.threadAgentLoop() }, this.activeMode()),
  );
  readonly effortOptions = computed(() => reasoningEffortOptions(this.reasoningSupport()));
  /** The thread's effort as this model runs it, so the matching choice is checked (e.g. High → On). */
  readonly threadEffortChoice = computed(() => effectiveReasoningEffort(this.reasoningSupport(), this.threadReasoningEffort()));
  readonly effectiveEffortLabel = computed(
    () => reasoningEffortLabel(this.reasoningSupport(), this.resolved().reasoningEffort.value) ?? 'Model default',
  );
  readonly effectiveLoopLabel = computed(() => AGENT_LOOP_LABELS[this.resolved().agentLoop.value ?? 'standard']);
  readonly modeName = computed(() => this.activeMode()?.name?.trim() || 'This');
  /** True when the mode supplies a value that is shown (a hidden reasoning part does not count). */
  readonly setByMode = computed(() => {
    const { reasoningEffort, agentLoop } = this.resolved();
    return agentLoop.source === 'mode' || (reasoningEffort.source === 'mode' && this.effortOptions().length > 0);
  });
  readonly highlighted = computed(
    () =>
      this.resolved().agentLoop.value === 'advanced' || (this.effortOptions().length > 0 && this.resolved().reasoningEffort.value !== null),
  );

  readonly triggerLabel = computed(() => {
    const loop = this.resolved().agentLoop.value ?? 'standard';
    if (this.effortOptions().length === 0) return AGENT_LOOP_LABELS[loop];
    const effort = reasoningEffortLabel(this.reasoningSupport(), this.resolved().reasoningEffort.value) ?? 'Default';
    return loop === 'advanced' ? `${effort} · ${AGENT_LOOP_LABELS.advanced}` : effort;
  });

  readonly modeOverrideTooltip = computed(
    () =>
      `The ${this.activeMode()?.name?.trim() || 'active'} ${this.modeNoun} sets this while it's active, overriding the thread's setting. ` +
      `Changing it here changes the thread's own setting, which applies when the ${this.modeNoun} is off.`,
  );

  readonly triggerTooltip = computed(() =>
    this.setByMode() ? this.modeOverrideTooltip() : 'Reasoning effort and agent loop for this thread',
  );

  readonly triggerAriaLabel = computed(() => {
    const parts: string[] = [];
    if (this.effortOptions().length > 0) parts.push(`reasoning ${this.effectiveEffortLabel()}`);
    parts.push(`agent loop ${this.effectiveLoopLabel()}`);
    const suffix = this.setByMode() ? `, set by the ${this.activeMode()?.name?.trim() || 'active'} ${this.modeNoun}` : '';
    return `Thread settings: ${parts.join(', ')}${suffix}`;
  });

  toggle(): void {
    if (this.disabled()) return;
    this.open.update(open => !open);
  }

  pickEffort(effort: ReasoningEffort | null): void {
    this.reasoningEffortChange.emit(effort);
  }

  pickLoop(loop: AgentLoop): void {
    this.agentLoopChange.emit(loop);
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: Event): void {
    const target = event.target;
    if (target instanceof Node && !this.host.nativeElement.contains(target)) this.open.set(false);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (!this.open()) return;
    this.open.set(false);
    this.host.nativeElement.querySelector<HTMLButtonElement>('.turn-settings__trigger')?.focus();
  }
}

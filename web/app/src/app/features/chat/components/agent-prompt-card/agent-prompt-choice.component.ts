import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { CheckboxComponent, RadioComponent } from '../../../../shared/ui';
import { optionLabelParts } from './agent-prompt-card.helpers';

/** How a choice was made: a pointer's press, or the keyboard (arrow keys move a radio group's choice). */
export interface PromptChoicePick {
  checked: boolean;
  byPointer: boolean;
}

/**
 * One answer in a prompt's list: a radio or checkbox, its label and an optional description, as a
 * quiet full-width row. A label ending in "(Recommended)", which is how the agent marks its advice,
 * shows that as a tag.
 */
@Component({
  selector: 'app-prompt-choice',
  standalone: true,
  imports: [CheckboxComponent, RadioComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <label class="choice" [class.choice--on]="checked()" (pointerdown)="pressed = true">
      <span class="choice__control">
        @if (multiple()) {
          <ui-checkbox [checked]="checked()" (checkedChange)="pick($event)" />
        } @else {
          <ui-radio [name]="name()" [checked]="checked()" (selected)="pick(true)" />
        }
      </span>
      <span class="choice__text">
        <span class="choice__label">
          {{ parts().text }}
          @if (parts().recommended) {
            <span class="choice__tag">Recommended</span>
          }
        </span>
        @if (description()) {
          <span class="choice__desc">{{ description() }}</span>
        }
      </span>
    </label>
  `,
  styles: [`
    :host { display: block; }
    .choice {
      border-radius: 0.5rem;
      cursor: pointer;
      display: flex;
      gap: 0.625rem;
      padding: 0.4375rem 0.5rem;
      transition: background 120ms ease;
    }
    .choice:hover { background: color-mix(in srgb, var(--text-primary) 5%, transparent); }
    .choice--on, .choice--on:hover { background: var(--accent-bg); }
    .choice__control { align-items: center; display: inline-flex; height: 1.25rem; }
    .choice__text { display: grid; gap: 0.0625rem; min-width: 0; }
    .choice__label { color: var(--text-primary); font-size: 0.875rem; line-height: 1.25rem; overflow-wrap: anywhere; }
    .choice__tag {
      color: var(--accent);
      font-size: 0.6875rem;
      font-weight: 600;
      margin-left: 0.25rem;
      white-space: nowrap;
    }
    .choice__desc { color: var(--text-muted); font-size: 0.8125rem; line-height: 1.125rem; }
  `],
})
export class AgentPromptChoiceComponent {
  /** The radio group; ignored for a checkbox. */
  readonly name = input('');
  readonly label = input.required<string>();
  readonly description = input<string | undefined>();
  readonly multiple = input(false);
  readonly checked = input(false);
  readonly picked = output<PromptChoicePick>();

  readonly parts = computed(() => optionLabelParts(this.label()));

  /** Set by the row's pointerdown, so a pick by arrow keys is told apart from a click. */
  protected pressed = false;

  pick(checked: boolean): void {
    this.picked.emit({ checked, byPointer: this.pressed });
    this.pressed = false;
  }
}

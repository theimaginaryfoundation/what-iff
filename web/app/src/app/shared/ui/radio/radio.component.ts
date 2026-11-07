import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

/**
 * The app's radio: a real `<input type="radio">` (so labels, arrow keys within a `name` group and
 * `role=radio` keep working) drawn to match `ui-checkbox`.
 *
 * It renders only the circle. Wrap it in your own `<label>` with the text, as you would a native
 * input, and give every radio of a group the same `name`. Use `ariaLabel` when there is no visible
 * text. `(selected)` fires when this radio becomes the group's choice, by pointer or keyboard.
 */
@Component({
  selector: 'ui-radio',
  standalone: true,
  templateUrl: './radio.component.html',
  styleUrl: './radio.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RadioComponent {
  readonly name = input.required<string>();
  readonly checked = input(false);
  readonly disabled = input(false);
  readonly value = input<string | null>(null);
  /** Accessible name when the radio has no visible label. */
  readonly ariaLabel = input<string | null>(null);
  /** `id` of the inner input, for a `<label for>` elsewhere. */
  readonly inputId = input<string | null>(null);
  /** `data-testid` of the inner input. */
  readonly testId = input<string | null>(null);

  readonly selected = output<void>();

  onChange(event: Event): void {
    event.stopPropagation();
    if ((event.target as HTMLInputElement).checked) this.selected.emit();
  }
}

import { ChangeDetectionStrategy, Component, ElementRef, computed, forwardRef, input, output, signal, viewChild } from '@angular/core';
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';

import { TooltipDirective } from '../tooltip/tooltip.directive';

/**
 * The app's checkbox: a real `<input type="checkbox">` (so labels, keyboard, `role=checkbox`,
 * `aria-checked="mixed"` and `input[type=checkbox]` test selectors all keep working) with the
 * Memory Manager's custom box and checkmark on top.
 *
 * It renders only the box. Wrap it in your own `<label>` with the text, as you would a native
 * input; the label's click reaches the inner input. Use `ariaLabel` when there is no visible text.
 *
 * Events: `(change)` re-emits the native change event, so `$event.target` is the inner input
 * (`$any($event.target).checked`, `saveCheckbox($any($event.target), ...)` work unchanged);
 * `(checkedChange)` gives the new state as a boolean, and with `[checked]` makes `[(checked)]`.
 * It is also a ControlValueAccessor, for `ngModel` and reactive forms.
 */
@Component({
  selector: 'ui-checkbox',
  standalone: true,
  imports: [TooltipDirective],
  templateUrl: './checkbox.component.html',
  styleUrl: './checkbox.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [{ provide: NG_VALUE_ACCESSOR, useExisting: forwardRef(() => CheckboxComponent), multi: true }],
})
export class CheckboxComponent implements ControlValueAccessor {
  readonly checked = input(false);
  /** Shows a dash instead of a tick ("some selected"); exposed to assistive tech as mixed. */
  readonly indeterminate = input(false);
  readonly disabled = input(false);
  /** Accessible name when the box has no visible label. */
  readonly ariaLabel = input<string | null>(null);
  /** `id` of the inner input, for a `<label for>` elsewhere. */
  readonly inputId = input<string | null>(null);
  readonly name = input<string | null>(null);
  /** Extra classes on the inner input: a hook for a caller's tests or layout, not for restyling the box. */
  readonly inputClass = input('');
  /** `data-testid` of the inner input. */
  readonly testId = input<string | null>(null);
  /** Tooltip on the inner input (see `uiTooltip`). */
  readonly tooltip = input('');

  /** The native change event; its target is the inner input. */
  readonly change = output<Event>();
  readonly checkedChange = output<boolean>();

  // Set by a form directive (ngModel / formControl); null when the caller binds [checked].
  private readonly formValue = signal<boolean | null>(null);
  private readonly formDisabled = signal(false);
  private onFormChange: (value: boolean) => void = () => undefined;
  private onFormTouched: () => void = () => undefined;
  private readonly box = viewChild<ElementRef<HTMLInputElement>>('box');

  /** `ui-checkbox` plus the caller's `inputClass`: one binding, so the base class can never be dropped. */
  readonly inputClasses = computed(() => `ui-checkbox ${this.inputClass()}`.trim());
  readonly isChecked = computed(() => this.formValue() ?? this.checked());
  readonly isDisabled = computed(() => this.disabled() || this.formDisabled());

  onChange(event: Event): void {
    // The native event bubbles to the host, where a `(change)` binding would see it a second time
    // after the output below; stop it here so callers get exactly one.
    event.stopPropagation();
    const value = (event.target as HTMLInputElement).checked;
    if (this.formValue() !== null) {
      this.formValue.set(value);
      this.onFormChange(value);
    }
    this.change.emit(event);
    this.checkedChange.emit(value);
  }

  onBlur(): void {
    this.onFormTouched();
  }

  writeValue(value: unknown): void {
    this.formValue.set(!!value);
    // Also write the DOM now: if the box was clicked since the last render, the binding still holds
    // the old value and would skip a write that equals it (Angular's own checkbox accessor does this).
    const box = this.box()?.nativeElement;
    if (box) box.checked = !!value;
  }

  registerOnChange(fn: (value: boolean) => void): void {
    this.onFormChange = fn;
  }

  registerOnTouched(fn: () => void): void {
    this.onFormTouched = fn;
  }

  setDisabledState(disabled: boolean): void {
    this.formDisabled.set(disabled);
  }
}

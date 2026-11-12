import { ChangeDetectionStrategy, Component, input, model, output } from '@angular/core';

/**
 * The Thread Title field of the new-thread dialog: a label over a text input.
 *
 * A local stand-in until `ui-field` / `ui-input` exist (design brief, A2). It uses the same classes
 * as the personality search box under it, so the two fields match in height and look: the default
 * border, and the accent (purple) border and ring on focus, like the marked personality row. The text
 * is selected on focus, so the default name is replaced by typing. Spacing is on the 4px rhythm.
 *
 * The input's native `select` event is stopped here. It bubbles, and the dialog that hosts this field
 * has an output named `select`, which Angular also binds as a DOM listener: unstopped, selecting the
 * text would look like picking a personality and create a thread.
 */
@Component({
  selector: 'app-thread-title-field',
  standalone: true,
  template: `
    <label class="flex flex-col gap-1">
      <span class="text-xs font-medium text-(--color-text-secondary)">{{ label() }}</span>
      <input
        type="text"
        class="rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
        data-testid="new-thread-title"
        data-autofocus
        maxlength="255"
        autocomplete="off"
        [placeholder]="placeholder()"
        [value]="value()"
        (input)="value.set($any($event.target).value)"
        (focus)="$any($event.target).select()"
        (select)="$event.stopPropagation()"
        (keydown.enter)="submitted.emit()"
      />
    </label>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadTitleFieldComponent {
  readonly label = input<string>('Thread Title');
  readonly placeholder = input<string>('');
  readonly value = model<string>('');
  /** Enter in the field. */
  readonly submitted = output<void>();
}

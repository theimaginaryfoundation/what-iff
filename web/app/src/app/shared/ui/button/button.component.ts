import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { SpinnerComponent } from '../spinner/spinner.component';
import { isActivationKey } from '../helpers/keyboard.helpers';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'sm' | 'md';
export type ButtonType = 'button' | 'submit' | 'reset';

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'border-transparent bg-[var(--cta)] text-[var(--cta-text)] hover:brightness-95',
  secondary: 'border-[var(--btn-secondary-border)] bg-[var(--btn-secondary)] text-[var(--btn-secondary-text)] hover:border-[var(--border-hover)]',
  ghost: 'border-transparent bg-transparent text-[var(--text-secondary)] hover:bg-[var(--accent-bg)] hover:text-[var(--text-primary)]',
  danger: 'border-transparent bg-[var(--danger)] text-white hover:brightness-95',
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  sm: 'min-h-9 px-3 py-1.5 text-xs',
  md: 'min-h-11 px-4 py-2 text-sm',
};

@Component({
  selector: 'ui-button',
  standalone: true,
  imports: [SpinnerComponent],
  templateUrl: './button.component.html',
  styleUrl: './button.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ButtonComponent {
  readonly variant = input<ButtonVariant>('secondary');
  readonly size = input<ButtonSize>('md');
  readonly type = input<ButtonType>('button');
  readonly disabled = input(false);
  readonly loading = input(false);
  readonly activate = output<Event>();

  readonly buttonClass = computed(() => [
    'inline-flex items-center justify-center gap-2 rounded-lg border font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--focus-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-base)] disabled:cursor-not-allowed disabled:opacity-50',
    VARIANT_CLASSES[this.variant()],
    SIZE_CLASSES[this.size()],
  ].join(' '));

  onClick(event: MouseEvent): void {
    if (this.isDisabled()) {
      event.preventDefault();
      return;
    }

    this.activate.emit(event);
  }

  onKeydown(event: KeyboardEvent): void {
    if (!isActivationKey(event) || this.isDisabled()) {
      return;
    }

    event.preventDefault();
    this.activate.emit(event);
  }

  isDisabled(): boolean {
    return this.disabled() || this.loading();
  }
}

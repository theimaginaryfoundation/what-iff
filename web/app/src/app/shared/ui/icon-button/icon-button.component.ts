import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { isActivationKey } from '../helpers/keyboard.helpers';
import type { ButtonVariant } from '../button/button.component';

export type IconButtonSize = 'xs' | 'md';

const SIZE_CLASSES: Record<IconButtonSize, string> = {
  xs: 'min-h-7 min-w-7 p-1 [@media(pointer:coarse)]:min-h-9 [@media(pointer:coarse)]:min-w-9',
  md: 'min-h-11 min-w-11 p-2',
};

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'border-transparent bg-[var(--cta)] text-[var(--cta-text)] hover:brightness-95',
  secondary: 'border-[var(--btn-secondary-border)] bg-[var(--btn-secondary)] text-[var(--btn-secondary-text)] hover:border-[var(--border-hover)]',
  ghost: 'border-transparent bg-transparent text-[var(--text-secondary)] hover:bg-[var(--accent-bg)] hover:text-[var(--text-primary)]',
  danger: 'border-transparent bg-[var(--danger)] text-white hover:brightness-95',
};

@Component({
  selector: 'ui-icon-button',
  standalone: true,
  templateUrl: './icon-button.component.html',
  styleUrl: './icon-button.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class IconButtonComponent {
  readonly ariaLabel = input.required<string>();
  readonly variant = input<ButtonVariant>('ghost');
  readonly size = input<IconButtonSize>('md');
  readonly disabled = input(false);
  readonly activate = output<Event>();

  readonly buttonClass = computed(() => [
    'inline-flex items-center justify-center rounded-lg border transition focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--focus-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-base)] disabled:cursor-not-allowed disabled:opacity-50',
    VARIANT_CLASSES[this.variant()],
    SIZE_CLASSES[this.size()],
  ].join(' '));

  onClick(event: MouseEvent): void {
    if (this.disabled()) {
      event.preventDefault();
      return;
    }

    this.activate.emit(event);
  }

  onKeydown(event: KeyboardEvent): void {
    if (!isActivationKey(event) || this.disabled()) {
      return;
    }

    event.preventDefault();
    this.activate.emit(event);
  }
}

import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { isActivationKey } from '../helpers/keyboard.helpers';

@Component({
  selector: 'ui-tag',
  standalone: true,
  templateUrl: './tag.component.html',
  styleUrl: './tag.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TagComponent {
  readonly selected = input(false);
  readonly disabled = input(false);
  readonly toggle = output<Event>();

  readonly tagClass = computed(() => [
    'inline-flex min-h-9 items-center rounded-full border px-3 py-1 text-xs font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--focus-ring)] disabled:cursor-not-allowed disabled:opacity-50',
    this.selected()
      ? 'border-[var(--border-hover)] bg-[var(--cta-bg)] text-[var(--cta-selected-text)]'
      : 'border-[var(--border)] bg-[var(--bg-card)] text-[var(--text-secondary)] hover:bg-[var(--accent-bg)]',
  ].join(' '));

  onActivate(event: Event): void {
    if (this.disabled()) {
      event.preventDefault();
      return;
    }

    this.toggle.emit(event);
  }

  onKeydown(event: KeyboardEvent): void {
    if (!isActivationKey(event)) {
      return;
    }

    event.preventDefault();
    this.onActivate(event);
  }
}

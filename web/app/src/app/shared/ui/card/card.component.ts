import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

export type CardVariant = 'flat' | 'elevated';

@Component({
  selector: 'ui-card',
  standalone: true,
  templateUrl: './card.component.html',
  styleUrl: './card.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CardComponent {
  readonly variant = input<CardVariant>('flat');

  readonly cardClass = computed(() => [
    'rounded-2xl border border-[var(--border)] bg-[var(--bg-card)] text-[var(--text-primary)]',
    this.variant() === 'elevated' ? 'shadow-[var(--card-shadow)] transition-shadow hover:shadow-[var(--card-shadow-hover)]' : '',
  ].join(' '));
}

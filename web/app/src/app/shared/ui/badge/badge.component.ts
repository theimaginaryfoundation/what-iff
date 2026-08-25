import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

export type BadgeIntent = 'neutral' | 'info' | 'success' | 'warning' | 'danger';

const INTENT_CLASSES: Record<BadgeIntent, string> = {
  neutral: 'ui-badge--neutral',
  info: 'ui-badge--info',
  success: 'ui-badge--success',
  warning: 'ui-badge--warning',
  danger: 'ui-badge--danger',
};

@Component({
  selector: 'ui-badge',
  standalone: true,
  templateUrl: './badge.component.html',
  styleUrl: './badge.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BadgeComponent {
  readonly intent = input<BadgeIntent>('neutral');
  readonly badgeClass = computed(() => `ui-badge inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${INTENT_CLASSES[this.intent()]}`);
}

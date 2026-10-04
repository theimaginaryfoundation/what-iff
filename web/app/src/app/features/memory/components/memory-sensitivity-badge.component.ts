import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

import { MemorySensitivity } from '../../../core/models/memory.model';
import { sensitivityDescription, sensitivityLabel } from '../helpers/memory-sensitivity.helpers';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/**
 * Quiet pill showing how freely a memory may be used (Public / Personal / Sensitive). Personal is
 * the default, so it stays neutral; the other two get a faint tint. The tooltip explains the level.
 */
@Component({
  selector: 'app-memory-sensitivity-badge',
  standalone: true,
  imports: [TooltipDirective],
  template: `<span class="sensitivity-badge" [attr.data-sensitivity]="sensitivity()" [uiTooltip]="hint()">{{ label() }}</span>`,
  styleUrl: './memory-sensitivity-badge.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemorySensitivityBadgeComponent {
  readonly sensitivity = input.required<MemorySensitivity>();

  readonly label = computed(() => sensitivityLabel(this.sensitivity()));
  readonly hint = computed(() => sensitivityDescription(this.sensitivity()));
}

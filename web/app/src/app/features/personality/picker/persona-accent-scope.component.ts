import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

import { personalityAccent, personalityAccentSurface } from '../helpers/personality-vm.helpers';
import { Personality } from '../../../core/models/personality.model';

/**
 * Wraps content with CSS custom properties scoped to a specific personality so
 * descendants (cards, picker rows, detail headers) can refer to
 * `--persona-accent` and `--persona-accent-surface` for borders, rings and
 * tinted backgrounds.
 *
 * Accepts either a full Personality (preferred) or a manual accent value when
 * the consumer needs to override the deterministic hash.
 */
@Component({
  selector: 'persona-accent-scope',
  standalone: true,
  template: `<ng-content />`,
  host: {
    '[style.--persona-accent]': 'accent()',
    '[style.--persona-accent-surface]': 'accentSurface()',
    '[style.display]': '"contents"',
  },
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonaAccentScopeComponent {
  readonly personality = input<Pick<Personality, 'id' | 'name' | 'accent_color'> | null | undefined>(null);
  readonly accentOverride = input<string | null | undefined>(null);

  readonly accent = computed(() => {
    const override = this.accentOverride();
    if (override) return override;
    const personality = this.personality();
    if (!personality) return 'hsl(220 65% 52%)';
    return personalityAccent(personality);
  });

  readonly accentSurface = computed(() => personalityAccentSurface(this.accent()));
}

import {
  ChangeDetectionStrategy,
  Component,
  input,
  output,
} from '@angular/core';

import { Personality } from '../../../core/models/personality.model';
import {
  PersonalityCardAction,
  PersonalityCardComponent,
} from './personality-card.component';

export interface PersonalityCardActionEvent {
  action: PersonalityCardAction;
  personality: Personality;
}

/**
 * Responsive grid wrapper for `app-personality-card`. Honors the design
 * spec's responsive columns (4/3/2/1 from lg → default) and exposes
 * a single composite action output so the page can route open/edit/delete
 * intents in one place.
 */
@Component({
  selector: 'app-personality-card-grid',
  standalone: true,
  imports: [PersonalityCardComponent],
  template: `
    <div
      class="personality-card-grid grid gap-4"
      style="grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));"
      role="list"
    >
      @for (personality of personalities(); track personality.id) {
        <div role="listitem" class="personality-card-grid__cell">
          <app-personality-card
            [personality]="personality"
            [defaultPersonalityId]="defaultPersonalityId()"
            (action)="onAction(personality, $event)"
          />
        </div>
      }
    </div>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityCardGridComponent {
  readonly personalities = input.required<readonly Personality[]>();
  readonly defaultPersonalityId = input<string | null>(null);

  readonly action = output<PersonalityCardActionEvent>();

  onAction(personality: Personality, action: PersonalityCardAction): void {
    this.action.emit({ action, personality });
  }
}

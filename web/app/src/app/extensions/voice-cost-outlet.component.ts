import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { Model } from '../core/models/model.model';

/** Where a voice's cost would show: beside a model in a picker, on a preview, or by autoplay. */
export type VoiceCostPlace = 'picker' | 'preview' | 'autoplay';

/**
 * Mount point for what a voice costs (ADR 0x052 §4). It renders nothing in this build, where the
 * provider bills the key's owner; a build that meters speech replaces this file to show its price
 * where the person chooses (`picker`), tries (`preview`, with the characters it reads) or turns on
 * reading every reply (`autoplay`).
 */
@Component({
  selector: 'app-voice-cost-outlet',
  standalone: true,
  template: ``,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VoiceCostOutletComponent {
  readonly model = input<Model | null>(null);
  readonly place = input<VoiceCostPlace>('picker');
  /** The characters a preview reads. */
  readonly characters = input(0);
}

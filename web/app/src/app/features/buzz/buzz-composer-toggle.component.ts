import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { RelayComposerSource, RelayComposerToggleComponent } from '../relay/relay-composer-toggle.component';
import { BuzzBinding, BuzzService, buzzBindingLabel } from './buzz.service';

/**
 * Above the composer in a Buzz relay thread: "Post the next reply to #channel on Buzz", and the
 * note when mentions are paused because the thread left the sandbox. Rendered by chat-page next
 * to the composer; the behaviour is the shared {@link RelayComposerToggleComponent}.
 */
@Component({
  selector: 'app-buzz-composer-toggle',
  standalone: true,
  imports: [RelayComposerToggleComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<app-relay-composer-toggle [source]="source" />`,
})
export class BuzzComposerToggleComponent {
  readonly source: RelayComposerSource<BuzzBinding> = {
    api: inject(BuzzService),
    platform: 'Buzz',
    tab: 'buzz',
    triggers: 'mentions',
    label: b => buzzBindingLabel(b),
    toggleSuffix: ' on Buzz',
  };
}

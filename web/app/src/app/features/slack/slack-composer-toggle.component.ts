import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { RelayComposerSource, RelayComposerToggleComponent } from '../relay/relay-composer-toggle.component';
import { SlackBinding, SlackService, slackBindingLabel } from './slack.service';

/**
 * Above the composer in a Slack relay thread: "Post the next reply to #channel in Slack", and the
 * note when mentions are paused because the thread left the sandbox. Rendered by chat-page next
 * to the composer; the behaviour is the shared {@link RelayComposerToggleComponent}.
 */
@Component({
  selector: 'app-slack-composer-toggle',
  standalone: true,
  imports: [RelayComposerToggleComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<app-relay-composer-toggle [source]="source" />`,
})
export class SlackComposerToggleComponent {
  readonly source: RelayComposerSource<SlackBinding> = {
    api: inject(SlackService),
    platform: 'Slack',
    tab: 'slack',
    triggers: 'mentions',
    label: b => slackBindingLabel(b),
    toggleSuffix: ' in Slack',
  };
}

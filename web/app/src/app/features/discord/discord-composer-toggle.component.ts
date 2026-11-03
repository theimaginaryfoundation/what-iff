import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { RelayComposerSource, RelayComposerToggleComponent } from '../relay/relay-composer-toggle.component';
import { DiscordBinding, DiscordService, bindingLabel } from './discord.service';

/**
 * Above the composer in a Discord relay thread: "Post the next reply to #channel", and the note
 * when tags are paused because the thread left the sandbox. Rendered by chat-page next to the
 * composer; the behaviour is the shared {@link RelayComposerToggleComponent}.
 */
@Component({
  selector: 'app-discord-composer-toggle',
  standalone: true,
  imports: [RelayComposerToggleComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<app-relay-composer-toggle [source]="source" />`,
})
export class DiscordComposerToggleComponent {
  readonly source: RelayComposerSource<DiscordBinding> = {
    api: inject(DiscordService),
    platform: 'Discord',
    tab: 'discord',
    triggers: 'tags',
    label: bindingLabel,
    pausedRemedy: ', or move the channel to a new sandboxed thread,',
  };
}

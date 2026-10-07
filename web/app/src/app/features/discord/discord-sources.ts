import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of, switchMap } from 'rxjs';

import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { ThreadAutomation, ThreadAutomationSource } from '../../core/services/thread-automation-source';
import { DiscordTabComponent } from './discord-tab.component';
import { DiscordBinding, DiscordService, bindingLabel } from './discord.service';

/** The Integrations "Discord" tab. */
export const DISCORD_TAB: IntegrationTab = {
  id: 'discord',
  label: 'Discord',
  tooltip: 'Give a persona its own Discord bot and relay channels into threads',
  component: DiscordTabComponent,
};

/** Adds the Discord tab when the server runs the relay. */
@Injectable()
export class DiscordIntegrationTabSource extends IntegrationTabSource {
  private readonly discord = inject(DiscordService);

  tabs(): Observable<IntegrationTab[]> {
    return this.discord.available().pipe(map(on => (on ? [DISCORD_TAB] : [])));
  }
}

/** Maps a binding to the Jobs-tab entry for its relay thread. */
export function bindingAutomation(b: DiscordBinding): ThreadAutomation {
  const broken = b.status !== 'active';
  const paused = !broken && !b.inbound_enabled;
  return {
    chatId: b.chat_id,
    name: `Discord · ${bindingLabel(b)}`,
    statusText: broken ? 'Broken' : paused ? 'Post only' : 'Live',
    tone: broken ? 'danger' : paused ? 'neutral' : 'success',
    statusHint: broken
      ? (b.last_error ?? 'The bot can no longer post here')
      : paused
        ? 'Replies can be posted from the app; tags in Discord are not answered'
        : 'Tags in Discord are answered here and posted back',
    link: ['/integrations'],
  };
}

/** Lists relay threads in the Thread Manager's Jobs tab. */
@Injectable()
export class DiscordThreadAutomationSource extends ThreadAutomationSource {
  private readonly discord = inject(DiscordService);

  list(): Observable<ThreadAutomation[]> {
    return this.discord.available().pipe(
      switchMap(on => (on ? this.discord.listBindings() : of([] as DiscordBinding[]))),
      map(bindings => bindings.map(bindingAutomation)),
      catchError(() => of([])),
    );
  }
}

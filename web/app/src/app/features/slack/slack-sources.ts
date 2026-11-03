import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of, switchMap } from 'rxjs';

import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { ThreadAutomation, ThreadAutomationSource } from '../../core/services/thread-automation-source';
import { SlackTabComponent } from './slack-tab.component';
import { SlackBinding, SlackService, slackBindingLabel } from './slack.service';

/** The Integrations "Slack" tab. */
export const SLACK_TAB: IntegrationTab = {
  id: 'slack',
  label: 'Slack',
  tooltip: 'Give a persona its own Slack app and relay channels into threads',
  component: SlackTabComponent,
};

/** Adds the Slack tab when the relay is on for the user. */
@Injectable()
export class SlackIntegrationTabSource extends IntegrationTabSource {
  private readonly slack = inject(SlackService);

  tabs(): Observable<IntegrationTab[]> {
    return this.slack.available().pipe(map(on => (on ? [SLACK_TAB] : [])));
  }
}

/** Maps a binding to the Jobs-tab entry for its relay thread. */
export function slackBindingAutomation(b: SlackBinding): ThreadAutomation {
  const broken = b.status !== 'active';
  const paused = !broken && !b.inbound_enabled;
  return {
    chatId: b.chat_id,
    name: `Slack · ${slackBindingLabel(b)}`,
    statusText: broken ? 'Broken' : paused ? 'Post only' : 'Live',
    tone: broken ? 'danger' : paused ? 'neutral' : 'success',
    statusHint: broken
      ? (b.last_error ?? 'The persona can no longer post here')
      : paused
        ? 'Replies can be posted from the app; mentions in Slack are not answered'
        : 'Mentions in Slack are answered here and posted back',
    link: ['/integrations'],
  };
}

/** Lists Slack relay threads in the Thread Manager's Jobs tab. */
@Injectable()
export class SlackThreadAutomationSource extends ThreadAutomationSource {
  private readonly slack = inject(SlackService);

  list(): Observable<ThreadAutomation[]> {
    return this.slack.available().pipe(
      switchMap(on => (on ? this.slack.listBindings() : of([] as SlackBinding[]))),
      map(bindings => bindings.map(slackBindingAutomation)),
      catchError(() => of([])),
    );
  }
}

import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of, switchMap } from 'rxjs';

import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { ThreadAutomation, ThreadAutomationSource } from '../../core/services/thread-automation-source';
import { BuzzTabComponent } from './buzz-tab.component';
import { BuzzBinding, BuzzService, buzzBindingLabel } from './buzz.service';

/** The Integrations "Buzz" tab. */
export const BUZZ_TAB: IntegrationTab = {
  id: 'buzz',
  label: 'Buzz',
  tooltip: 'Give a persona its own Buzz identity and relay community channels into threads',
  component: BuzzTabComponent,
};

/** Adds the Buzz tab when the server runs the relay. */
@Injectable()
export class BuzzIntegrationTabSource extends IntegrationTabSource {
  private readonly buzz = inject(BuzzService);

  tabs(): Observable<IntegrationTab[]> {
    return this.buzz.available().pipe(map(on => (on ? [BUZZ_TAB] : [])));
  }
}

/** Maps a binding to the Jobs-tab entry for its relay thread. */
export function buzzBindingAutomation(b: BuzzBinding): ThreadAutomation {
  const broken = b.status !== 'active';
  const paused = !broken && !b.inbound_enabled;
  return {
    chatId: b.chat_id,
    name: `Buzz · ${buzzBindingLabel(b)}`,
    statusText: broken ? 'Broken' : paused ? 'Post only' : 'Live',
    tone: broken ? 'danger' : paused ? 'neutral' : 'success',
    statusHint: broken
      ? (b.last_error ?? 'The persona can no longer post here')
      : paused
        ? 'Replies can be posted from the app; mentions in Buzz are not answered'
        : 'Mentions in Buzz are answered here and posted back',
    link: ['/integrations'],
  };
}

/** Lists Buzz relay threads in the Thread Manager's Jobs tab. */
@Injectable()
export class BuzzThreadAutomationSource extends ThreadAutomationSource {
  private readonly buzz = inject(BuzzService);

  list(): Observable<ThreadAutomation[]> {
    return this.buzz.available().pipe(
      switchMap(on => (on ? this.buzz.listBindings() : of([] as BuzzBinding[]))),
      map(bindings => bindings.map(buzzBindingAutomation)),
      catchError(() => of([])),
    );
  }
}

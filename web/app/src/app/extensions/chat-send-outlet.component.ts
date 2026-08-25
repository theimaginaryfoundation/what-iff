import { ChangeDetectionStrategy, Component } from '@angular/core';

/**
 * Mount point rendered by chat-page just above the composer. Empty in the
 * default build; another build replaces this file to render its own content
 * there. chat-page renders <app-chat-send-outlet /> unconditionally, so no
 * chat-page edits are needed to light it up.
 */
@Component({
  selector: 'app-chat-send-outlet',
  standalone: true,
  template: '',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ChatSendOutletComponent {}

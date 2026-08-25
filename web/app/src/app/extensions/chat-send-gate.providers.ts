import { Provider } from '@angular/core';

import {
  ChatSendGate,
  NoopChatSendGate,
} from '../features/chat/services/chat-send-gate';

/**
 * DI providers for the chat send-flow gate (swap-point file).
 *
 * This build binds the no-op gate, so chat never blocks sending. Another build
 * replaces this file to bind an implementation that gates sending based on its
 * own state. `app.config.ts` spreads these into the application config.
 */
export const chatSendGateProviders: Provider[] = [
  { provide: ChatSendGate, useClass: NoopChatSendGate },
];

import { Injectable } from '@angular/core';

import { Model } from '../../../core/models/model.model';
import { defaultComposerPlaceholder } from '../helpers/chat-send.helpers';

/**
 * Extension point for the chat send flow: lets a build gate message sending,
 * customize the composer placeholder, and react to send/lifecycle events. The
 * default build never gates sending; another build supplies an implementation
 * that does (e.g. based on usage state). The concrete implementation is
 * supplied via DI.
 */
export abstract class ChatSendGate {
  /** True when message sending should currently be blocked. */
  abstract blocked(): boolean;
  /** Placeholder for the composer input, given the active personality name. */
  abstract composerPlaceholder(personalityName: string | null): string;
  /** Called once when the chat page is entered. */
  abstract enterChat(): void;
  /** Refresh any state that gates sending (e.g. after a send, or on re-entry). */
  abstract refresh(): void;
  /** Called after a send succeeds, so the gate can clear any transient block. */
  abstract sendSucceeded(): void;
  /** Inspect a failed send and update gate state accordingly. */
  abstract handleSendError(error: unknown): void;
  /** Notify the gate that the active model changed. */
  abstract setActiveModel(model: Model | null): void;
  /** Reset in-memory state (call on logout). */
  abstract clearCache(): void;
}

/**
 * Default gate: sending is never blocked and the lifecycle hooks do nothing.
 */
@Injectable()
export class NoopChatSendGate extends ChatSendGate {
  blocked(): boolean {
    return false;
  }

  composerPlaceholder(personalityName: string | null): string {
    return defaultComposerPlaceholder(personalityName);
  }

  enterChat(): void {
    // Nothing to load.
  }

  refresh(): void {
    // Nothing to refresh.
  }

  sendSucceeded(): void {
    // No state to clear.
  }

  handleSendError(): void {
    // No state to update.
  }

  setActiveModel(): void {
    // The active model does not affect sending here.
  }

  clearCache(): void {
    // No state to reset.
  }
}

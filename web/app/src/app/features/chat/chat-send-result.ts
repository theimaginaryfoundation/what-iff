/**
 * Result of {@link ChatSessionService.sendMessage}.
 * Use the discriminant `status` — do not rely on truthiness of the whole object.
 */
export type ChatSendMessageResult =
  | { status: 'sent' }
  | { status: 'skipped' }
  | { status: 'failed'; error: unknown };

/** Optional extras for {@link ChatSessionService.sendMessage}. */
export interface ChatSendMessageOptions {
  /** Text prepended to the posted message only (never to the saved/restored draft). */
  contextPrefix?: string;
}

export function isChatSendFailed(
  result: ChatSendMessageResult,
): result is { status: 'failed'; error: unknown } {
  return result.status === 'failed';
}

export function isChatSendSucceeded(result: ChatSendMessageResult): result is { status: 'sent' } {
  return result.status === 'sent';
}

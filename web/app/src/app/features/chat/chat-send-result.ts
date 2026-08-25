/**
 * Result of {@link ChatSessionService.sendMessage}.
 * Use the discriminant `status` — do not rely on truthiness of the whole object.
 */
export type ChatSendMessageResult =
  | { status: 'sent' }
  | { status: 'skipped' }
  | { status: 'failed'; error: unknown };

export function isChatSendFailed(
  result: ChatSendMessageResult,
): result is { status: 'failed'; error: unknown } {
  return result.status === 'failed';
}

export function isChatSendSucceeded(result: ChatSendMessageResult): result is { status: 'sent' } {
  return result.status === 'sent';
}

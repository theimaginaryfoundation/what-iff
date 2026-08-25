import { ChatMessage } from '../../../core/models/message.model';
import { ToolCall } from '../../../core/models/toolcall.model';

import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from '../chat.constants';

export type GroupedItem =
  | MessageGroupItem
  | ToolCallGroupItem
  | ModelChangeDividerItem
  | SystemMessageItem;

export interface MessageGroupItem {
  readonly kind: 'message-group';
  readonly origin: ChatMessage['origin'];
  readonly assistantPersonalityKey?: string;
  readonly assistantExpressionSignature?: string;
  messages: readonly ChatMessage[];
}

export interface ToolCallGroupItem {
  readonly kind: 'tool-call-group';
  readonly message: ChatMessage;
  readonly toolCalls: readonly ToolCall[];
}

export interface ModelChangeDividerItem {
  readonly kind: 'model-change-divider';
  readonly model: string;
  readonly previousModel?: string;
  readonly messageId: string;
}

export interface SystemMessageItem {
  readonly kind: 'system-message';
  readonly message: ChatMessage;
}

/**
 * Converts flat messages into render-ready groups. The helper intentionally
 * knows only about message shape, not Angular, so list rendering can stay dumb.
 *
 * Assistant rows may have an empty `message` body when the model only emitted tool calls
 * (tool-heavy turns); such rows still get a message-group so the tool-call strip renders.
 * The chat UI treats empty body as valid in this pipeline (server persists empty strings).
 */
export function groupMessages(messages: readonly ChatMessage[]): GroupedItem[] {
  const groups: GroupedItem[] = [];
  let currentGroup: MessageGroupItem | null = null;
  let previousModel: string | undefined;

  for (const message of messages) {
    const model = normalizedModel(message.generation_model);
    if (model && previousModel && model !== previousModel) {
      groups.push({
        kind: 'model-change-divider',
        model,
        previousModel,
        messageId: message.id,
      });
      currentGroup = null;
    }
    if (model) previousModel = model;

    if (isSystemMessage(message)) {
      groups.push({ kind: 'system-message', message });
      currentGroup = null;
      continue;
    }

    if (message.tool_calls?.length) {
      groups.push({ kind: 'tool-call-group', message, toolCalls: message.tool_calls });
      currentGroup = null;
      if (!hasRenderableMessageBody(message)) {
        continue;
      }
    }

    if (
      currentGroup &&
      currentGroup.origin === message.origin &&
      currentGroup.assistantPersonalityKey === assistantPersonalityKey(message) &&
      currentGroup.assistantExpressionSignature === assistantExpressionSignature(message)
    ) {
      currentGroup.messages = [...currentGroup.messages, message];
      continue;
    }

    currentGroup = {
      kind: 'message-group',
      origin: message.origin,
      assistantPersonalityKey: assistantPersonalityKey(message),
      assistantExpressionSignature: assistantExpressionSignature(message),
      messages: [message],
    };
    groups.push(currentGroup);
  }

  return groups;
}

function isSystemMessage(message: ChatMessage): boolean {
  return message.origin !== 'User' && message.origin !== 'Assistant';
}

function normalizedModel(model?: string): string | undefined {
  const trimmed = model?.trim();
  return trimmed ? trimmed : undefined;
}

function hasRenderableMessageBody(message: ChatMessage): boolean {
  return Boolean(message.message?.trim() || message.attachments?.length);
}

function assistantPersonalityKey(message: ChatMessage): string | undefined {
  if (message.origin !== 'Assistant') return undefined;
  const personality = message.generation_personality?.trim();
  return personality ? personality.toLowerCase() : '__default_assistant__';
}

function assistantExpressionSignature(message: ChatMessage): string | undefined {
  if (message.origin !== 'Assistant') return undefined;
  const key = message.generation_expression_key?.trim().toLowerCase() || '__default_expression_key__';
  const image = message.generation_expression_image_url?.trim() || '__default_expression_image__';
  return `${key}::${image}`;
}

/** Timestamp shown only for pending bubble chrome (hidden while composing); stable so grouping does not thrash. */
const CHAT_PENDING_ASSISTANT_PLACEHOLDER_SENT_AT = '1970-01-01T00:00:00.000Z';

/**
 * Synthetic assistant row pre-painted while a chat job is polled before any Assistant message exists after the latest user turn.
 */
export function pendingAssistantPlaceholderMessage(opts: {
  chatId: string;
  draftText: string;
  generationPersonality: string;
  thinkingImageUrl: string | null;
}): ChatMessage {
  return {
    id: CHAT_PENDING_ASSISTANT_MESSAGE_ID,
    chat_id: opts.chatId,
    message: opts.draftText,
    origin: 'Assistant',
    sent_at: CHAT_PENDING_ASSISTANT_PLACEHOLDER_SENT_AT,
    generation_personality: opts.generationPersonality,
    generation_expression_key: 'thinking',
    generation_expression_image_url: opts.thinkingImageUrl ?? undefined,
  };
}

/**
 * True while a chat_message job is pending (not streaming) and no Assistant message has arrived yet after the last user message.
 */
export function needsPendingAssistantPlaceholder(
  messages: readonly ChatMessage[],
  assistantJobPending: boolean,
): boolean {
  if (!assistantJobPending) return false;
  if (messages.length === 0) return true;
  let lastUserIdx = -1;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].origin === 'User') {
      lastUserIdx = i;
      break;
    }
  }
  if (lastUserIdx < 0) return false;
  for (let i = lastUserIdx + 1; i < messages.length; i++) {
    if (messages[i].origin === 'Assistant') return false;
  }
  return true;
}

export function appendPendingAssistantGroup(groups: readonly GroupedItem[], pendingMessage: ChatMessage): GroupedItem[] {
  const item: MessageGroupItem = {
    kind: 'message-group',
    origin: 'Assistant',
    assistantPersonalityKey: assistantPersonalityKey(pendingMessage),
    assistantExpressionSignature: assistantExpressionSignature(pendingMessage),
    messages: [pendingMessage],
  };
  return [...groups, item];
}

/**
 * The latest user turn that failed async generation and has no assistant reply after it in the list.
 * Used for composer retry affordance and resuming job polling after refresh.
 */
export function lastUserTurnWithGenerationError(messages: readonly ChatMessage[]): ChatMessage | null {
  let lastUserIdx = -1;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].origin === 'User') {
      lastUserIdx = i;
      break;
    }
  }
  if (lastUserIdx < 0) {
    return null;
  }
  const userMsg = messages[lastUserIdx];
  const err = userMsg.last_error_message?.trim();
  if (!err) {
    return null;
  }
  const hasAssistantAfter = messages.slice(lastUserIdx + 1).some(m => m.origin === 'Assistant');
  if (hasAssistantAfter) {
    return null;
  }
  return userMsg;
}

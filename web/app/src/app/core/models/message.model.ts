import { FileAttachment } from './file-attachment.model';
import { Ritual } from './ritual.model';
import { ToolCall } from './toolcall.model';

export type MessageOrigin = 'User' | 'Assistant';
export type MessageReadStatus = 'read' | 'unread';

/** One segment-kind row of the per-turn context breakdown ("Context X-ray"). */
export interface ContextSegmentStat {
  /** Segment kind, e.g. "system_prompt", "memory_context", "history_turn", "user_message". */
  kind: string;
  /** How many segments of this kind were present in the turn. */
  segments: number;
  /** Estimated tokens for this kind, except vendor_prompt_other which reconciles usage. */
  tokens: number;
  /** Whether any segment of this kind was part of the cacheable prompt prefix. */
  cacheable: boolean;
  /** Image payloads carried by this kind (tokens do not account for images). */
  images?: number;
}

/**
 * Snapshot of the model context assembled for one assistant turn: what was "in the
 * model's head" for that reply. Total tokens use vendor-reported input usage when available;
 * named buckets are estimates plus a reconciliation remainder.
 */
export interface ContextBreakdown {
  /** Schema version of this snapshot; readers branch on it for shape evolution. */
  version?: number;
  segments: ContextSegmentStat[];
  total_tokens: number;
  budget_tokens: number;
  model?: string;
  provider?: string;
  captured_at: string;
  /** Authoritative settled credit charge when supplied by the metering implementation. */
  charged_credits?: number;
}

export interface ChatMessage {
  id: string;
  chat_id: string;
  message: string;
  origin: MessageOrigin;
  read_status?: MessageReadStatus;
  response_id?: string;
  generation_model?: string;
  generation_personality?: string;
  generation_mood_id?: string;
  /** Display name of the mood when this message was generated (assistant). */
  generation_mood_name?: string;
  /** Base64-encoded JPEG mood portrait (size depends on mood save time; newer ~128×128). */
  generation_mood_thumbnail?: string;
  generation_expression_key?: string;
  generation_expression_image_url?: string;
  generation_expression_label?: string | null;
  generation_expression_reasoning?: string | null;
  sent_at: string;
  attachments?: FileAttachment[];
  rituals?: Ritual[];
  tool_calls?: ToolCall[];
  /** Set when async generation failed for this user turn; cleared after a successful reply. */
  last_error_message?: string | null;
  /** When scratchpad + memory + summary checkpoint completed after this assistant reply (if any). */
  checkpoint_completed_at?: string | null;
  /** Per-turn model-context composition captured at generation time (assistant messages). */
  context_breakdown?: ContextBreakdown | null;
  /** Whether the user has bookmarked this message for long-thread navigation. */
  bookmarked?: boolean;
}

/** Lightweight bookmark entry for the thread navigator (from GET /chat/{id}/bookmarks). */
export interface MessageBookmark {
  id: string;
  origin: MessageOrigin;
  snippet: string;
  sent_at: string;
}

export interface ChatMessageFilters {
  origin?: MessageOrigin;
  search?: string;
  min_date?: string;
  max_date?: string;
}

export interface CreateChatMessageRequest {
  message: string;
  origin: MessageOrigin;
  response_id?: string;
  attachments?: FileAttachment[];
  rituals?: Ritual[];
  // IANA timezone name from the browser (e.g. "America/Los_Angeles")
  client_timezone?: string;
}

export interface ChatMessageResponse {
  id: string;
  job_id: string;
  type: string;
}

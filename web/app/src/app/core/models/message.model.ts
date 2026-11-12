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
  /** The thread's checkpoint ceiling: past it (measured by checkpoint_input_tokens), older turns get summarized. */
  budget_tokens: number;
  /**
   * What the checkpoint compares to budget_tokens: total_tokens on a Standard turn, the first
   * call's input on an Advanced one. Absent on older snapshots.
   */
  checkpoint_input_tokens?: number;
  /** The loop the turn ran. Absent on older snapshots. */
  agent_loop?: 'standard' | 'advanced' | (string & {});
  /**
   * Advanced turns: the run's own ceiling. A call past it makes the run compact its earlier steps
   * before the next one. total_tokens (the final call) is read against this, not budget_tokens.
   */
  run_budget_tokens?: number;
  /** Advanced turns: how many times the run compacted its context. */
  run_compactions?: number;
  model?: string;
  provider?: string;
  captured_at: string;
}

/**
 * One thing that happened within a step of a saved Advanced run: `reasoning` (native reasoning) or
 * `text` (text the model wrote) carry `text`; `tool_call` carries the id of one of the message's
 * tool_calls. Open-ended: readers skip kinds they do not know.
 */
export interface AgentRunEvent {
  kind: 'reasoning' | 'text' | 'tool_call' | (string & {});
  text?: string;
  tool_call_id?: string;
}

/** What happened in one step of a saved Advanced run, in order, before its final reply. */
export interface AgentRunStep {
  /** The step, 1-based; the message's tool_calls carry the same number. */
  step: number;
  events?: AgentRunEvent[];
}

/** One item of a saved Advanced run's plan. The status is open-ended: render unknown values generically. */
export interface AgentRunPlanItem {
  content: string;
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled' | (string & {});
}

/** Why a saved Advanced run ended. Open-ended: render unknown values generically. */
export type AgentRunEndReason = 'done' | 'stalled' | 'duration_limit' | 'step_limit' | 'quota' | 'cancelled' | 'failed' | (string & {});

/**
 * A saved Advanced run (ADR 0x026) on the assistant message it produced: what happened per step
 * (notes and tool calls), the plan as it stood at the end, and why the run ended. The message text is the final reply.
 */
export interface AgentRun {
  /** The record's format (1). Readers render what they know of a newer version rather than reject it. */
  version?: number;
  steps?: AgentRunStep[];
  plan?: AgentRunPlanItem[];
  end_reason?: AgentRunEndReason;
}

/** The agent job a scheduled run is for (ADR 0x040). */
export interface JobRun {
  agent_job_id: string;
  /** The job's title when it ran; absent for an untitled job. */
  title?: string;
  /** The job's prompt as it ran. It is never saved as a message. */
  prompt?: string;
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
  /** Client-only, on the pending-reply placeholder: why the reply has not started (shown instead of the dots). */
  pending_status?: string;
  /** Client-only, on the pending-reply placeholder: whose turn it is when not the user's (a job's run). */
  pending_label?: string;
  /**
   * The agent job a scheduled run's reply was written for, with the prompt it ran (ADR 0x040). On
   * the pending-reply placeholder, the client sets it from the run's ticket.
   */
  job_run?: JobRun | null;
  /** Reasoning/thinking the model reported for this assistant turn (GLM, MiMo). Display-only. */
  model_reasoning?: string | null;
  /** Advanced turns only (ADR 0x026): the run's notes, plan and end reason. Replaces model_reasoning there. */
  agent_run?: AgentRun | null;
  sent_at: string;
  attachments?: FileAttachment[];
  rituals?: Ritual[];
  tool_calls?: ToolCall[];
  /** Document comments the message is about (ADR 0x046): the one that sent it, or the agent's. */
  comment_refs?: CommentRef[];
  /** Set when async generation failed for this user turn; cleared after a successful reply. */
  last_error_message?: string | null;
  /** Stable code for last_error_message when it has one (e.g. provider_not_configured). */
  last_error_code?: string | null;
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

/** A document comment a chat message is about (ADR 0x046), shown as a link to its thread. */
export interface CommentRef {
  file_id: string;
  file_name: string;
  thread_id: string;
  /** The comment within the thread (the thread's id for its first comment). */
  comment_id?: string;
  /** What the thread is on, short; empty for the whole file. */
  quote?: string;
}

/**
 * The document comment a message is sent from (ADR 0x046): its text is the message, and what the
 * thread is on and the thread before it reach the agent without being shown.
 */
export interface CommentMessage {
  file_id: string;
  thread_id: string;
  comment_id: string;
  quote: string;
  earlier: { author_name: string; text: string }[];
}

export interface CreateChatMessageRequest {
  message: string;
  origin: MessageOrigin;
  response_id?: string;
  attachments?: FileAttachment[];
  rituals?: Ritual[];
  // IANA timezone name from the browser (e.g. "America/Los_Angeles")
  client_timezone?: string;
  /**
   * Sent while the user's turn in this chat is still running (ADR 0x042): that turn takes it in at
   * its next step; if it finishes first, the message runs as its own turn.
   */
  steer?: boolean;
  /** The message is a document comment that mentioned the agent (ADR 0x046). */
  comment?: CommentMessage;
}

export interface ChatMessageResponse {
  id: string;
  job_id: string;
  type: string;
}

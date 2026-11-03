export type JobStatus =
  | 'pending'
  | 'processing'
  | 'inference_complete'
  | 'expression_complete'
  | 'compaction_complete'
  | 'complete'
  | 'cancelled'
  | 'failed';

export interface Job {
  id: string;
  user_id: string;
  job_type: string;
  reference: string;
  status: JobStatus;
  error?: string;
  /** Stable code for error when it has one (e.g. provider_not_configured). */
  error_code?: string;
  result_id?: string;
  progress_current?: number;
  progress_total?: number;
  status_message?: string;
  /**
   * Opaque JSON-encoded progress payload for long-running jobs (parse per job_type).
   * For `chat_import` this decodes to {@link ChatImportProgress}.
   */
  progress?: string;
  /**
   * Incremental assistant text chunks emitted while inference is still in progress.
   */
  draft_deltas?: string[];
  /**
   * Live model reasoning chunks (GLM, MiMo). Unlike draft_deltas this can be reset
   * (emptied and re-sent) when a truncated call is retried — render the whole array.
   */
  draft_reasoning?: string[];
  created_at: string;
  updated_at: string;
}

/** Decoded shape of {@link Job.progress} for `chat_message` jobs: the live tool-call timeline. */
export interface ChatTurnProgress {
  tool_calls: ChatTurnToolCall[];
  /** Set while the turn is queued behind an earlier turn in its chat: the phase it waits on. */
  waiting_on?: ChatTurnWaiting;
  /** Advanced turns only (ADR 0x026): the current step, 1-based. */
  step?: number;
  /** Advanced turns only: the model's current plan checklist. */
  plan?: ChatTurnPlanItem[];
}

export const CHAT_TURN_PLAN_STATUSES = ['pending', 'in_progress', 'completed', 'cancelled'] as const;
export type ChatTurnPlanStatus = (typeof CHAT_TURN_PLAN_STATUSES)[number];

/** An Advanced turn's live step and plan, decoded from {@link ChatTurnProgress}. */
export interface ChatTurnAdvancedProgress {
  /** Current step, 1-based. */
  step: number;
  plan: ChatTurnPlanItem[];
}

/** One item of an Advanced turn's plan (the `update_plan` tool's checklist). */
export interface ChatTurnPlanItem {
  content: string;
  status: ChatTurnPlanStatus;
}

/** What a queued turn is waiting on (the server's turn gate): an earlier turn's reply. */
export const CHAT_TURN_WAITING_STATES = ['reply'] as const;
export type ChatTurnWaiting = (typeof CHAT_TURN_WAITING_STATES)[number];

/** One tool call in {@link ChatTurnProgress}; input/output are truncated previews. */
export interface ChatTurnToolCall {
  /** Provider tool-call id, unique within the turn. */
  id: string;
  name: string;
  input?: string;
  status: 'running' | 'complete' | 'error';
  /** Result (or error text) preview, set once the call finishes. */
  output?: string;
  round: number;
  /** Advanced turns only: the step (1-based) the call ran in. */
  step?: number;
  started_at: string;
  finished_at?: string;
}

/** Decoded shape of {@link Job.progress} for `chat_import` jobs. */
export interface ChatImportProgress {
  phase: 'uploading' | 'parsing' | 'importing' | 'complete' | 'failed' | string;
  source?: string;
  total: number;
  imported: number;
  skipped: number;
  /** Chat IDs created during this import run (present when phase is complete). */
  imported_ids?: string[];
}

export interface JobFilters {
  job_type?: string;
  reference?: string;
  status?: JobStatus;
}

/** Active non-terminal chat_message job for a user turn (resume / polling). */
export interface ActiveChatMessageJob {
  job_id: string;
  status: JobStatus;
  /** The user turn the job answers (set by the thread-level lookup). */
  message_id?: string;
}

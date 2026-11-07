import { JobRun } from './message.model';

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
  /** On a scheduled run's queue ticket, the agent job it runs for and its prompt (ADR 0x040). */
  job_run?: JobRun | null;
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
  /** The question the turn is waiting on the person to answer (ADR 0x042); absent otherwise. */
  prompt?: AgentPrompt;
}

/** Who is asking: the agent's own `ask_user` call, or an MCP server's elicitation. */
export type AgentPromptSource = 'agent' | 'mcp_elicitation';
/** The card's shape. */
export type AgentPromptKind = 'questions' | 'approval' | 'form' | 'url';
export type AgentPromptAction = 'accept' | 'decline' | 'cancel';

export interface AgentPromptOption {
  label: string;
  description?: string;
}

export interface AgentPromptQuestion {
  /** Keys the answer (q1, q2, ...). */
  id: string;
  header?: string;
  question: string;
  /** Absent: an open question answered in free text. */
  options?: AgentPromptOption[];
  multi_select?: boolean;
}

export interface AgentPromptPayload {
  kind: AgentPromptKind;
  title?: string;
  message?: string;
  questions?: AgentPromptQuestion[];
  /** An elicitation form's JSON Schema (flat object of primitive fields). */
  schema?: ElicitationSchema;
  /** A URL-mode elicitation's link. */
  url?: string;
  /** The MCP connector asking (elicitation). */
  server_name?: string;
  tool_name?: string;
}

/** A pending prompt as the live turn shows it ({@link ChatTurnProgress.prompt}). */
export interface AgentPrompt {
  id: string;
  source: AgentPromptSource;
  payload: AgentPromptPayload;
  expires_at: string;
  tool_call_id?: string;
}

export interface AgentPromptAnswer {
  selected?: string[];
  text?: string;
}

/** Body of `POST /chat/{chatId}/prompts/{promptId}/answer`. */
export interface AgentPromptResponse {
  action: AgentPromptAction;
  answers?: Record<string, AgentPromptAnswer>;
  content?: Record<string, unknown>;
  note?: string;
}

/** The subset of JSON Schema MCP elicitation allows: a flat object of primitive fields. */
export interface ElicitationSchema {
  type?: 'object';
  properties?: Record<string, ElicitationProperty>;
  required?: string[];
}

export interface ElicitationProperty {
  type: 'string' | 'number' | 'integer' | 'boolean' | 'array';
  title?: string;
  description?: string;
  enum?: unknown[];
  /** Legacy (2025-06-18) display names for `enum`. */
  enumNames?: string[];
  oneOf?: { const: unknown; title?: string }[];
  items?: { type?: string; enum?: unknown[]; anyOf?: { const: unknown; title?: string }[] };
  default?: unknown;
  format?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  minItems?: number;
  maxItems?: number;
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
  /**
   * The kind of turn (set by the thread-level lookup): `chat_message` answers a user's message,
   * `agent_job_run` is a scheduled job's or webhook's run (ADR 0x039).
   */
  job_type?: 'chat_message' | 'agent_job_run';
  /** The user turn the job answers (set by the thread-level lookup); absent for a job's run. */
  message_id?: string;
  /**
   * The agent job a scheduled run is for (ADR 0x040). Only a scheduled run has one: a webhook
   * background run or the onboarding welcome is a chat-keyed `agent_job_run` too.
   */
  job_run?: JobRun | null;
}

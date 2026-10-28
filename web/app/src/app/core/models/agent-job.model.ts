import { AgentLoop, AgentLoopUpdate, ReasoningEffort, ReasoningEffortUpdate } from './turn-settings.model';

export type AgentJobScheduleType = 'cron' | 'at';
export type AgentJobStatus = 'active' | 'paused' | 'complete' | 'failed';

export interface AgentJob {
  id: string;
  user_id: string;
  chat_id?: string | null;
  personality_id?: string | null;
  model_id?: string | null;
  title?: string | null;
  prompt: string;
  schedule_input: string;
  schedule_type: AgentJobScheduleType;
  schedule?: string | null;
  run_at?: string | null;
  timezone: string;
  /** Recurring schedule runs in the user's current (browser-reported) timezone instead of `timezone`. */
  follow_user_timezone?: boolean;
  /** Timezone the schedule runs in right now: the user's current one for a following job, else `timezone`. */
  effective_timezone?: string;
  status: AgentJobStatus;
  next_run_at?: string | null;
  last_run_at?: string | null;
  last_error?: string | null;
  run_count: number;
  /** Reasoning effort for this job's runs; overrides the thread and mode (ADR 0x026). Null when not set. */
  reasoning_effort?: ReasoningEffort | null;
  /** Agent loop for this job's runs; overrides the thread and mode. Null when not set. */
  agent_loop?: AgentLoop | null;
  rituals?: import('./ritual.model').Ritual[];
  created_at: string;
  updated_at: string;
}

export interface AgentJobFilters {
  status?: AgentJobStatus;
  schedule_type?: AgentJobScheduleType;
  search?: string;
}

export interface CreateAgentJobRequest {
  title?: string | null;
  prompt: string;
  schedule_input: string;
  timezone?: string | null;
  follow_user_timezone?: boolean;
  chat_id?: string | null;
  personality_id?: string | null;
  model_id?: string | null;
  reasoning_effort?: ReasoningEffort | null;
  agent_loop?: AgentLoop | null;
}

export interface UpdateAgentJobRequest {
  title?: string | null;
  prompt?: string | null;
  chat_id?: string | null;
  personality_id?: string | null;
  model_id?: string | null;
  schedule_input?: string | null;
  timezone?: string | null;
  follow_user_timezone?: boolean;
  /** Omit to keep; `''` clears. */
  reasoning_effort?: ReasoningEffortUpdate;
  /** Omit to keep; `''` clears. */
  agent_loop?: AgentLoopUpdate;
}

export interface UpdateAgentJobStatusRequest {
  status: 'active' | 'paused';
}

export interface AgentJobScheduleParseRequest {
  schedule_input: string;
  timezone?: string;
  now?: string;
}

export interface AgentJobSchedulePreview {
  schedule_type: AgentJobScheduleType;
  schedule: string | null;
  run_at: string | null;
  timezone: string;
  human_summary: string;
  next_runs: string[];
}


import { AgentLoop, AgentLoopUpdate, ReasoningEffort, ReasoningEffortUpdate } from './turn-settings.model';

export type AgentJobScheduleType = 'cron' | 'at';
/** `needs_values`: a parameter has no value, so the job can't run until it gets one (ADR 0x044). */
export type AgentJobStatus = 'active' | 'paused' | 'complete' | 'failed' | 'needs_values';

/** The kind of input a job parameter takes (ADR 0x044). */
export type AgentJobParameterType = 'single_select' | 'multi_select' | 'text' | 'list';

/** How a multi_select or list value renders into the prompt. */
export type AgentJobParameterFormat = 'comma' | 'bullets';

/** One `{{name}}` placeholder of a job prompt. Every defined parameter is required. */
export interface AgentJobParameter {
  name: string;
  label?: string;
  description?: string;
  type: AgentJobParameterType;
  /** The choices, for single_select and multi_select. */
  options?: string[];
  /** multi_select and list only. */
  format?: AgentJobParameterFormat;
}

/** A string for text and single_select, a list for multi_select and list. */
export type AgentJobParameterValue = string | string[];

/** The owner's values by parameter name. Never published with a shared job. */
export type AgentJobParameterValues = Record<string, AgentJobParameterValue>;

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
  /** Definitions of the prompt's `{{name}}` placeholders, in form order. */
  parameters?: AgentJobParameter[];
  parameter_values?: AgentJobParameterValues;
  /** Parameters with no usable value, in definition order (derived by the server). */
  missing_parameters?: string[];
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
  chat_id?: string | null;
  personality_id?: string | null;
  model_id?: string | null;
  reasoning_effort?: ReasoningEffort | null;
  agent_loop?: AgentLoop | null;
  parameters?: AgentJobParameter[];
  parameter_values?: AgentJobParameterValues;
}

export interface UpdateAgentJobRequest {
  title?: string | null;
  prompt?: string | null;
  chat_id?: string | null;
  personality_id?: string | null;
  model_id?: string | null;
  schedule_input?: string | null;
  timezone?: string | null;
  /** Omit to keep; `''` clears. */
  reasoning_effort?: ReasoningEffortUpdate;
  /** Omit to keep; `''` clears. */
  agent_loop?: AgentLoopUpdate;
  /** Replace the definitions; omit to keep. Saved together with the prompt. */
  parameters?: AgentJobParameter[];
  /** Replace the values; omit to keep. */
  parameter_values?: AgentJobParameterValues;
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


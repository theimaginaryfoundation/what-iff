import { AgentLoop, ReasoningEffort } from './turn-settings.model';

export interface Mood {
  id: string;
  name: string;
  description: string;
  prompt_snippet: string;
  /** Optional model name to switch to when this mood becomes active. */
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids: string[];
  ritual_ids: string[];
  /** UUIDs of personalities this mood is attached to (populated on GET /mood/{id}). */
  personality_ids?: string[];
  thumbnail_data?: string; // base64-encoded JPEG (~128×128 for newly saved moods)
  /**
   * Reasoning effort for turns while this mode is active; overrides the thread's (ADR 0x026), except
   * in a sandboxed thread. Null when not set.
   */
  reasoning_effort?: ReasoningEffort | null;
  /** Agent loop for turns while this mode is active; overrides the thread's. Null when not set. */
  agent_loop?: AgentLoop | null;
  created_at: string;
  updated_at: string;
}

export interface CreateMoodRequest {
  name: string;
  description: string;
  prompt_snippet: string;
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids?: string[];
  ritual_ids?: string[];
  reasoning_effort?: ReasoningEffort | null;
  agent_loop?: AgentLoop | null;
}

export interface UpdateMoodRequest {
  name: string;
  description: string;
  prompt_snippet: string;
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids?: string[];
  ritual_ids?: string[];
  /** Omit to keep it; null clears it. */
  reasoning_effort?: ReasoningEffort | null;
  /** Omit to keep it; null clears it. */
  agent_loop?: AgentLoop | null;
}

/** Replaces all personality associations for a mood. */
export interface AttachMoodToPersonalitiesRequest {
  personality_ids: string[];
}

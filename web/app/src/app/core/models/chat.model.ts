export interface Chat {
  id: string;
  user_id: string;
  name: string;
  unread_count?: number;
  last_message_time?: string;
  model_id?: string;
  model_name?: string;
  personality_id?: string;
  personality_name?: string;
  disabled_tools?: string[];
  tags?: string[];
  is_favorite?: boolean;
  /** When true, thread is in the archive (hidden from default lists). */
  archived?: boolean;
  /** UUID of the effective mood currently used for generation. */
  active_mood_id?: string | null;
  /** True when chat mood policy is Auto; false when manually pinned. */
  is_auto_mood?: boolean;
  /**
   * Lazy-summarization lifecycle for imported threads:
   * '' (none) | 'pending' | 'processing' | 'ready' | 'failed'. Set when an imported thread is
   * restored (unarchived) and its summary is generated in the background.
   */
  rehydration_state?: string;
  created_at: string;
  updated_at: string;
}

export interface ChatFilters {
  name?: string;
  search?: string;
  tag?: string;
  personality_id?: string;
  is_favorite?: boolean;
  /** When true, list archived threads only. When false or omitted, active threads only. */
  archived?: boolean;
  min_date?: string;
  max_date?: string;
  /** Filter to chats imported from this source (e.g. 'openai', 'anthropic'). */
  source?: string;
  /** Comma-separated chat IDs to restrict results to (max 200). */
  ids?: string;
}

export interface CreateChatRequest {
  name: string;
  last_message_time?: string;
  /** Optional. Personality UUID to assign to the new chat. Must be owned by the user. */
  personality_id?: string;
  /** Optional. Active model UUID to assign to the new chat. */
  model_id?: string;
  tags?: string[];
  is_favorite?: boolean;
}

export interface UpdateChatRequest {
  name: string;
  last_message_time?: string;
  model_id?: string;
  personality_id?: string;
  disabled_tools?: string[];
  tags?: string[];
  is_favorite?: boolean;
}

export interface PatchChatRequest {
  name?: string;
  last_message_time?: string;
  model_id?: string;
  personality_id?: string;
  disabled_tools?: string[];
  tags?: string[];
  is_favorite?: boolean;
  /** Set effective active mood for generation. */
  active_mood_id?: string | null;
  /** Set selection policy: true=Auto, false=manual. */
  is_auto_mood?: boolean;
  /** Set to true to explicitly clear the active mood (Auto mood). */
  clear_active_mood?: boolean;
  archived?: boolean;
}

export interface ChatContext {
  chat_id: string;
  active_scratchpad: string;
  summary: string;
}

export interface PatchChatContextRequest {
  active_scratchpad: string;
}

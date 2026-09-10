export interface Personality {
  id: string;
  name: string;
  system_prompt: string;
  scratchpad?: string;
  scratchpad_history?: string[];
  /** @deprecated Ignored by the server; column retained for compatibility. */
  archival_model?: string;
  scratchpad_update_prompt?: string;
  /** @deprecated Ignored by the server; column retained for compatibility. */
  memory_search_prompt?: string;
  /** @deprecated Ignored by the server; column retained for compatibility. */
  memory_write_prompt?: string;
  auto_pin_memories: boolean;
  /** UUIDs of moods attached to this personality as available moods. */
  mood_ids?: string[];
  /** Image gallery file attachment ID used as the personality's portrait. */
  cover_image_id: string | null;
  /** Read-only authenticated URL for the cover image. */
  cover_image_url: string | null;
  /** Optional custom accent color for this personality. */
  accent_color?: string | null;
  /** Optional normalized portrait focus circle for circular thumbnails. */
  thumbnail_circle?: PersonalityThumbnailCircle | null;
  /** When false, expression frames are hidden in chat and expression picking is skipped. */
  expressions_enabled: boolean;
  /** Preferred image generation style (e.g. 'auto', 'anime', 'none'). */
  image_style: string;
  created_at: string;
  updated_at: string;
  stats: PersonalityUsageStats;
}

export interface PersonalityPromptChange {
  id: string;
  user_id: string;
  personality_id: string;
  old_prompt: string;
  new_prompt: string;
  action: 'edit' | 'revert';
  reverted_change_id?: string | null;
  created_at: string;
}

export interface PersonalityThumbnailCircle {
  cx: number;
  cy: number;
  r: number;
}

export interface PersonalityUsageStats {
  chat_count: number;
  last_used_at: string | null;
}

export interface PersonalityExpression {
  expression_key: string;
  /** Short display text and optional when-to-use hint for picker + continuity (max 80 chars server-side). */
  label: string | null;
  image_id: string | null;
  image_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface UpdatePersonalityExpressionRequest {
  image_id?: string | null;
  label?: string | null;
}

export interface PromptDefaults {
  scratchpad_update_prompt: string;
  memory_query_prompt: string;
  memory_extraction_prompt: string;
}

export interface PersonalityFilters {
  name?: string;
  query?: string;
  min_date?: string;
  max_date?: string;
  personality_ids?: string[];
}

export interface CreatePersonalityRequest {
  name: string;
  system_prompt: string;
  /** Optional cover image gallery file attachment ID owned by the user. */
  cover_image_id?: string | null;
  /** Optional custom accent color in hex format. */
  accent_color?: string | null;
  /** Optional normalized portrait focus circle for thumbnails. */
  thumbnail_circle?: PersonalityThumbnailCircle | null;
}

export interface UpdatePersonalityRequest {
  name: string;
  system_prompt: string;
  scratchpad?: string;
  /** @deprecated Ignored by the server. */
  archival_model?: string;
  scratchpad_update_prompt?: string;
  /** @deprecated Ignored by the server. */
  memory_search_prompt?: string;
  /** @deprecated Ignored by the server. */
  memory_write_prompt?: string;
  auto_pin_memories?: boolean;
  /** Send a UUID to set, null to clear. PUT semantics: omitting will clear the cover. */
  cover_image_id?: string | null;
  /** Optional custom accent color in hex format. */
  accent_color?: string | null;
  /** Optional normalized portrait focus circle for thumbnails. */
  thumbnail_circle?: PersonalityThumbnailCircle | null;
  /** When false, expression picking is skipped and expression frames are hidden in chat. */
  expressions_enabled?: boolean;
  /** Preferred image generation style. */
  image_style?: string;
}

export interface PaginatedPersonalityResponse {
  results: Personality[];
  total_count: number;
  page: number;
}

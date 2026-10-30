/** One entity wiki page an agent wrote (ADR 0x034). Shown in Memories > Entities. */
export interface AgentEntity {
  id: string;
  user_id: string;
  /** Normalized lookup key (lowercase, single spaces). */
  key: string;
  /** The name as first written. */
  title: string;
  body: string;
  /** Set when only this personality sees the entity; absent for a global one. */
  pinned_personality_id?: string;
  pinned_personality_name?: string;
  last_personality_id?: string;
  last_chat_id?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentEntityFilters {
  query?: string;
  personality_id?: string;
  global_only?: boolean;
}

export interface PaginatedAgentEntityResponse {
  results: AgentEntity[];
  total_count: number;
  totalPages?: number;
  page: number;
}

/** Entity text limit, matching the server's models.MaxEntityBodyRunes. */
export const MAX_ENTITY_BODY_CHARS = 5000;

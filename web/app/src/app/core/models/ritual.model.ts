export interface Ritual {
  id: string;
  name: string;
  description: string;
  content: string;
  hotkeys: string;
  personality_id: string | null;
  mcp_server_ids?: string[];
  created_at: string;
  updated_at: string;
}

export interface CreateRitualRequest {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  mcp_server_ids?: string[];
}

export interface UpdateRitualRequest {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  mcp_server_ids?: string[];
}

export interface RitualFilters {
  name?: string;
  search?: string;
  personality_id?: string;
  personality_ids?: string[];
  global_only?: boolean;
  has_hotkeys?: boolean;
  sort?: RitualSort;
  min_date?: string;
  max_date?: string;
}

export type RitualSort = 'name_asc' | 'created_desc' | 'updated_desc';

export interface PaginatedRitualResponse {
  results: Ritual[];
  total_count: number;
  page: number;
}

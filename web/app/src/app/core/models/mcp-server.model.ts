export interface MCPServer {
  id: string;
  user_id: string;
  name: string;
  description: string;
  server_url: string;
  error_message?: string;
  default_enabled: boolean;
  /** Rituals that load this MCP when triggered (same edge as ritual `mcp_server_ids`). */
  ritual_ids?: string[];
  created_at: string;
  updated_at: string;
}

export interface MCPServerFilters {
  search?: string;
}

export interface CreateMCPServerRequest {
  name: string;
  description: string;
  server_url: string;
  authentication?: string;
  default_enabled: boolean;
}

export interface UpdateMCPServerRequest {
  name?: string;
  description?: string;
  server_url?: string;
  authentication?: string | null;
  default_enabled?: boolean;
  /** Replaces linked rituals; omit for no change. */
  ritual_ids?: string[];
}


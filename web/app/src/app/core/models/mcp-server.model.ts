export interface MCPServer {
  id: string;
  user_id: string;
  name: string;
  description: string;
  server_url: string;
  status: string;
  status_reason?: string;
  last_checked_at?: string;
  last_healthy_at?: string;
  tool_count?: number;
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

export interface TestMCPServerConnectionRequest {
  server_url: string;
  authentication?: string | null;
  connector_id?: string;
}

export interface TestMCPServerConnectionResponse {
  pass: boolean;
  tool_count: number;
  message?: string;
}


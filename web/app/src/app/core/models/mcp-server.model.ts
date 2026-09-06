export interface MCPServer {
  id: string;
  user_id: string;
  name: string;
  description: string;
  server_url: string;
  auth_mode: 'header' | 'oauth';
  oauth_auth_url?: string;
  oauth_token_url?: string;
  oauth_client_id?: string;
  oauth_scopes?: string[];
  oauth_pkce_policy?: 'required' | 'supported' | 'not_supported';
  oauth_access_token_expires_at?: string;
  oauth_refresh_token_expires_at?: string;
  oauth_authenticated_at?: string;
  oauth_last_refresh_at?: string;
  oauth_refresh_fail_count?: number;
  oauth_has_refresh_token?: boolean;
  oauth_has_access_token?: boolean;
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
  auth_mode?: 'header' | 'oauth';
  authentication?: string;
  default_enabled: boolean;
  oauth_auth_url?: string;
  oauth_token_url?: string;
  oauth_client_id?: string;
  oauth_client_secret?: string;
  oauth_scopes?: string[];
  oauth_pkce_policy?: 'required' | 'supported' | 'not_supported';
}

export interface UpdateMCPServerRequest {
  name?: string;
  description?: string;
  server_url?: string;
  auth_mode?: 'header' | 'oauth';
  authentication?: string | null;
  default_enabled?: boolean;
  /** Replaces linked rituals; omit for no change. */
  ritual_ids?: string[];
  oauth_auth_url?: string;
  oauth_token_url?: string;
  oauth_client_id?: string;
  oauth_client_secret?: string | null;
  oauth_scopes?: string[];
  oauth_pkce_policy?: 'required' | 'supported' | 'not_supported';
}

export interface TestMCPServerConnectionRequest {
  server_url: string;
  auth_mode?: 'header' | 'oauth';
  authentication?: string | null;
  connector_id?: string;
}

export interface TestMCPServerConnectionResponse {
  pass: boolean;
  tool_count: number;
  message?: string;
}

export interface StartMCPServerOAuthRequest {
  redirect_after?: string;
}

export interface StartMCPServerOAuthResponse {
  authorization_url: string;
}


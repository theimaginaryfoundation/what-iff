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
  /** Set when the sign-in was read from the server (issue #517): the token's audience. */
  oauth_resource?: string;
  /** The authorization server the sign-in was read from; empty for settings typed in. */
  oauth_issuer?: string;
  oauth_client_registration?: MCPOAuthRegistration;
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
  /**
   * Sets the sign-in up from the server URL: read from the server, with a client registered
   * unless oauth_client_id is given. The endpoint, scope and PKCE fields are then ignored.
   */
  oauth_auto?: boolean;
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
  /** Reads the sign-in from the server again, as on create. */
  oauth_auto?: boolean;
}

/** How a connector's OAuth client is set up. */
export type MCPOAuthRegistration = 'manual' | 'metadata_document' | 'dynamic';

/** What an MCP server says about signing in to it (POST /mcp-servers/oauth/discover). */
export interface MCPServerOAuthDiscovery {
  resource: string;
  resource_name?: string;
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  registration_endpoint?: string;
  scopes?: string[];
  client_id_metadata_document_supported: boolean;
  iss_parameter_supported: boolean;
  legacy?: boolean;
  /** How a client will be got on save; manual means a client ID has to be typed in. */
  registration: MCPOAuthRegistration;
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

export interface MCPServerOAuthConfigResponse {
  /** The redirect_uri this server sends to OAuth providers; differs per deployment. */
  redirect_uri: string;
}


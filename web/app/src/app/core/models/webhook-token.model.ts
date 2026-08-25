export interface WebhookToken {
  id: string;
  user_id: string;
  name: string;
  status: 'active' | 'revoked';
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateWebhookTokenRequest {
  name: string;
}

export interface CreateWebhookTokenResponse {
  token: WebhookToken;
  api_token: string;
}

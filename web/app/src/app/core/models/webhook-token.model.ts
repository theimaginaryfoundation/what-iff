/**
 * What a webhook token may do. `messages:write` posts messages into a thread. `chat:read` reads
 * threads, messages, job status and persona names (never system prompts or memories).
 */
export type WebhookScope = 'messages:write' | 'chat:read';

export interface WebhookScopeOption {
  scope: WebhookScope;
  /** Short name for chips and checkboxes. */
  label: string;
  /** One line saying what the scope lets the token do. */
  description: string;
}

export const WEBHOOK_SCOPE_OPTIONS: readonly WebhookScopeOption[] = [
  {
    scope: 'messages:write',
    label: 'Post messages',
    description: 'Post messages into your threads and trigger replies.'
  },
  {
    scope: 'chat:read',
    label: 'Read threads',
    description: 'Read your threads, messages, job status and persona names. Never system prompts or memories.'
  }
];

export interface WebhookToken {
  id: string;
  user_id: string;
  name: string;
  status: 'active' | 'revoked';
  /** Tokens created before scopes existed report `messages:write`. */
  scopes: WebhookScope[];
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateWebhookTokenRequest {
  name: string;
  /** Omit for `messages:write` only, which is what tokens have always done. */
  scopes?: WebhookScope[];
}

export interface CreateWebhookTokenResponse {
  token: WebhookToken;
  api_token: string;
}

import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { relayAvailability } from '../relay/relay-availability';
import { RelayIdKind } from '../relay/relay-id-chips.component';

/** Slack relay (ADR 0x032). Shapes mirror the Slack* schemas in openapi.yaml. */

export type SlackAppStatus = 'active' | 'invalid_token' | 'disabled';
export type SlackBindingStatus = 'active' | 'broken';

/** The manifest of a Slack app that speaks as a persona, and the link that creates it. */
export interface SlackManifest {
  manifest: Record<string, unknown>;
  /** Opens Slack's "create an app" flow with the manifest filled in. */
  create_url: string;
  /** The bot token scopes the app asks for. */
  scopes: string[];
  /** The events the app subscribes to. */
  events: string[];
}

/** A persona's own Slack app in one workspace. Its tokens are never returned. */
export interface SlackApp {
  id: string;
  personality_id: string;
  team_id: string;
  team_name: string;
  team_url: string;
  /** Slack's id for the app (A…). */
  app_id: string;
  /** The app's bot user: what people mention. */
  bot_user_id: string;
  bot_id: string;
  bot_name: string;
  status: SlackAppStatus;
  /** Why Slack refused the app, or why its connection is retrying. */
  last_error?: string | null;
  last_connected_at?: string | null;
  /** The app's settings page in Slack, where its icon is uploaded. */
  settings_url: string;
  created_at: string;
  updated_at: string;
}

export interface SlackChannel {
  id: string;
  name: string;
  is_private: boolean;
  /** Whether the app is in the channel (it only hears channels it is in). */
  is_member: boolean;
  is_archived: boolean;
  num_members?: number;
}

export interface SlackBinding {
  id: string;
  /** What Iff's app row, not Slack's app id. */
  app_id: string;
  team_name?: string;
  chat_id: string;
  channel_id: string;
  channel_name: string;
  is_private: boolean;
  inbound_enabled: boolean;
  /** Slack user ids (U…) that may mention the persona; empty means anyone not denied. */
  allow_user_ids: string[];
  deny_user_ids: string[];
  /** Let other apps' bots start turns, behind the relay's loop guard. */
  answer_agents: boolean;
  /** The owner's acknowledgement that the bound thread is not sandboxed. */
  allow_unrestricted: boolean;
  /** Whether the bound thread is sandboxed right now (computed by the API; absent when unknown). */
  chat_sandboxed?: boolean;
  status: SlackBindingStatus;
  last_error?: string | null;
  last_activity_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface SlackMessageLink {
  id: string;
  binding_id: string;
  direction: 'inbound' | 'outbound';
  chat_message_id?: string | null;
  message_ts?: string;
  thread_ts: string;
  posted_ts: string[];
  author_user_id?: string;
  author_name?: string;
  reply_to_link_id?: string | null;
  status: 'received' | 'pending' | 'sent' | 'failed';
  error?: string | null;
  attempts: number;
  agent_depth: number;
  created_at: string;
}

export interface SlackChatState {
  bindings: SlackBinding[];
  pending_binding_ids: string[];
  links: SlackMessageLink[];
}

export interface CreateSlackAppRequest {
  personality_id: string;
  /** The Bot User OAuth Token (xoxb-…). */
  bot_token: string;
  /** An app-level token (xapp-…) with connections:write. */
  app_token: string;
}

export interface UpdateSlackAppRequest {
  bot_token?: string;
  app_token?: string;
  /** Re-read the workspace's and the bot's names from Slack. */
  refresh?: boolean;
  /** Pause (false) or resume (true). */
  enabled?: boolean;
}

export interface CreateSlackBindingRequest {
  app_id: string;
  channel_id: string;
  channel_name: string;
  is_private: boolean;
  /** Omit for a new sandboxed relay thread. */
  chat_id?: string;
  inbound_enabled?: boolean;
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  answer_agents?: boolean;
  /** Required to bind an existing thread that is not sandboxed. */
  allow_unrestricted?: boolean;
}

export interface UpdateSlackBindingRequest {
  chat_id?: string;
  inbound_enabled?: boolean;
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  channel_name?: string;
  answer_agents?: boolean;
  allow_unrestricted?: boolean;
  reactivate?: boolean;
}

/** The prefix of each token Slack gives out, which the connect form checks before sending. */
export const SLACK_BOT_TOKEN_PREFIX = 'xoxb-';
export const SLACK_APP_TOKEN_PREFIX = 'xapp-';

/** Whether a typed token is clearly the wrong kind: long enough to tell, and without the expected prefix. */
export function tokenLooksWrong(token: string, prefix: string): boolean {
  const t = token.trim();
  return t.length >= prefix.length && !t.startsWith(prefix);
}

/**
 * Slack member ids in the allow and deny chips: U… or W… (a profile's ⋮ menu, Copy member ID).
 * A pasted mention (`<@U0123ABC>`) works too. Kept upper-case, as Slack writes them.
 */
export const SLACK_USER_IDS: RelayIdKind = {
  parse: entry => {
    const m = /^<?@?([UW][A-Z0-9]{2,})(?:\|[^>]*)?>?$/i.exec(entry);
    return m ? m[1].toUpperCase() : null;
  },
  invalid: 'Slack member ids start with U or W (open a profile, ⋮, Copy member ID).',
  monospace: true,
};

/** "#general · Acme" for a binding (the workspace name is optional). */
export function slackBindingLabel(b: Pick<SlackBinding, 'channel_name' | 'channel_id' | 'team_name'>, teamName?: string): string {
  const channel = `#${b.channel_name || b.channel_id}`;
  const team = teamName ?? b.team_name;
  return team ? `${channel} · ${team}` : channel;
}

/** API client for /api/slack (internal/slackplugin). */
@Injectable({ providedIn: 'root' })
export class SlackService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiUrl}/slack`;
  private availability$: Observable<boolean> | null = null;

  /**
   * Whether the relay is on. It is off unless the operator switches it on
   * (SLACK_RELAY_ENABLED=true). Where it is off, every /api/slack route answers 404; an answer that
   * cannot be read (status 0) counts as off too, so a feature the server may not have stays hidden
   * when we cannot tell. Asked once per session.
   */
  available(): Observable<boolean> {
    this.availability$ ??= relayAvailability(this.http, `${this.base}/bindings`, { failClosed: true });
    return this.availability$;
  }

  manifest(personalityId: string): Observable<SlackManifest> {
    return this.http.get<SlackManifest>(`${this.base}/manifest`, { params: { personality_id: personalityId } });
  }

  listApps(): Observable<SlackApp[]> {
    return this.http.get<SlackApp[]>(`${this.base}/apps`);
  }

  addApp(body: CreateSlackAppRequest): Observable<SlackApp> {
    return this.http.post<SlackApp>(`${this.base}/apps`, body);
  }

  updateApp(id: string, body: UpdateSlackAppRequest): Observable<SlackApp> {
    return this.http.patch<SlackApp>(`${this.base}/apps/${id}`, body);
  }

  removeApp(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/apps/${id}`);
  }

  listChannels(appId: string): Observable<SlackChannel[]> {
    return this.http.get<SlackChannel[]>(`${this.base}/apps/${appId}/channels`);
  }

  /** Adds the app to a public channel; private ones need `/invite` in Slack. */
  joinChannel(appId: string, channelId: string): Observable<SlackChannel> {
    return this.http.post<SlackChannel>(`${this.base}/apps/${appId}/channels/${channelId}/join`, {});
  }

  listBindings(): Observable<SlackBinding[]> {
    return this.http.get<SlackBinding[]>(`${this.base}/bindings`);
  }

  addBinding(body: CreateSlackBindingRequest): Observable<SlackBinding> {
    return this.http.post<SlackBinding>(`${this.base}/bindings`, body);
  }

  updateBinding(id: string, body: UpdateSlackBindingRequest): Observable<SlackBinding> {
    return this.http.patch<SlackBinding>(`${this.base}/bindings/${id}`, body);
  }

  removeBinding(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/bindings/${id}`);
  }

  chatState(chatId: string): Observable<SlackChatState> {
    return this.http.get<SlackChatState>(`${this.base}/chats/${chatId}`);
  }

  setPostNextReply(chatId: string, bindingId: string, on: boolean): Observable<void> {
    const url = `${this.base}/chats/${chatId}/pending/${bindingId}`;
    return on ? this.http.put<void>(url, {}) : this.http.delete<void>(url);
  }
}

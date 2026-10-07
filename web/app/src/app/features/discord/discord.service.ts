import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of, shareReplay } from 'rxjs';

import { environment } from '../../../environments/environment';

/** Discord relay. Shapes mirror internal/models/discord.go. */

export type DiscordBotStatus = 'active' | 'invalid_token' | 'disabled';
export type DiscordBindingStatus = 'active' | 'broken';

export interface DiscordBot {
  id: string;
  personality_id: string;
  application_id: string;
  bot_user_id: string;
  bot_username: string;
  message_content: boolean;
  status: DiscordBotStatus;
  last_error?: string | null;
  created_at: string;
  updated_at: string;
  invite_url: string;
}

export interface DiscordBinding {
  id: string;
  bot_id: string;
  chat_id: string;
  guild_id: string;
  channel_id: string;
  guild_name: string;
  channel_name: string;
  inbound_enabled: boolean;
  allow_user_ids: string[];
  deny_user_ids: string[];
  /**
   * The owner's acknowledgement that the bound thread is not sandboxed: anyone allowed to tag the
   * bot can use what it reads. Without it the relay answers only sandboxed threads.
   */
  allow_unrestricted: boolean;
  /** Whether the bound thread is sandboxed right now (computed by the API; absent when unknown). */
  chat_sandboxed?: boolean;
  status: DiscordBindingStatus;
  last_error?: string | null;
  last_activity_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface DiscordMessageLink {
  id: string;
  binding_id: string;
  direction: 'inbound' | 'outbound';
  chat_message_id?: string | null;
  discord_channel_id: string;
  posted_message_ids: string[];
  author_id?: string;
  author_name?: string;
  status: 'received' | 'pending' | 'sent' | 'failed';
  error?: string | null;
  created_at: string;
}

export interface DiscordChatState {
  bindings: DiscordBinding[];
  pending_binding_ids: string[];
  links: DiscordMessageLink[];
}

export interface DiscordGuild {
  id: string;
  name: string;
}

export interface DiscordChannel {
  id: string;
  name: string;
}

export interface CreateDiscordBindingRequest {
  bot_id: string;
  guild_id: string;
  guild_name: string;
  channel_id: string;
  channel_name: string;
  chat_id?: string | null;
  inbound_enabled?: boolean;
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  /** Required to bind an existing thread that is not sandboxed. */
  allow_unrestricted?: boolean;
}

export interface UpdateDiscordBindingRequest {
  chat_id?: string;
  inbound_enabled?: boolean;
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  /** Acknowledge (true) or withdraw (false) that the bound thread is not sandboxed. */
  allow_unrestricted?: boolean;
  reactivate?: boolean;
}

/** What anyone allowed to tag the bot gets from a thread that is not sandboxed; shown wherever it is acknowledged. */
export const UNRESTRICTED_WARNING =
  'Anyone allowed to tag the bot can use what this thread reads: your name, memories, other conversations, files, scratchpad and tools.';

/** The note shown while a binding's allow list is empty. */
export const EMPTY_ALLOW_NOTE = 'Anyone in this channel can talk to this persona.';

/**
 * Whether a thread needs the owner's acknowledgement to be driven from Discord: anything that is
 * not sandboxed (an unset flag means not sandboxed). Mirrors the backend rule.
 */
export function needsRelayAcknowledgement(sandboxed: boolean | null | undefined): boolean {
  return sandboxed !== true;
}

/** "#general · Home" for a binding. */
export function bindingLabel(b: Pick<DiscordBinding, 'channel_name' | 'channel_id' | 'guild_name'>): string {
  const channel = `#${b.channel_name || b.channel_id}`;
  return b.guild_name ? `${channel} · ${b.guild_name}` : channel;
}

/** API client for /api/discord (internal/discordplugin). */
@Injectable({ providedIn: 'root' })
export class DiscordService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiUrl}/discord`;
  private availability$: Observable<boolean> | null = null;

  /**
   * Whether the server runs the relay. An operator can switch it off
   * (DISCORD_RELAY_ENABLED=false), and then /api/discord is not routed: a 404
   * here means off. Any other outcome means on (an error then shows where the
   * user can see it). Asked once per session.
   */
  available(): Observable<boolean> {
    if (!this.availability$) {
      this.availability$ = this.http.get<unknown>(`${this.base}/bindings`).pipe(
        map(() => true),
        catchError((e: unknown) => of(!(e instanceof HttpErrorResponse && e.status === 404))),
        shareReplay({ bufferSize: 1, refCount: false }),
      );
    }
    return this.availability$;
  }

  listBots(): Observable<DiscordBot[]> {
    return this.http.get<DiscordBot[]>(`${this.base}/bots`);
  }

  addBot(personalityId: string, token: string): Observable<DiscordBot> {
    return this.http.post<DiscordBot>(`${this.base}/bots`, { personality_id: personalityId, token });
  }

  updateBot(id: string, body: { token?: string; refresh?: boolean; enabled?: boolean }): Observable<DiscordBot> {
    return this.http.patch<DiscordBot>(`${this.base}/bots/${id}`, body);
  }

  removeBot(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/bots/${id}`);
  }

  syncProfile(id: string): Observable<{ bot: DiscordBot; avatar_synced: boolean }> {
    return this.http.post<{ bot: DiscordBot; avatar_synced: boolean }>(`${this.base}/bots/${id}/sync-profile`, {});
  }

  listGuilds(botId: string): Observable<DiscordGuild[]> {
    return this.http.get<DiscordGuild[]>(`${this.base}/bots/${botId}/guilds`);
  }

  listChannels(botId: string, guildId: string): Observable<DiscordChannel[]> {
    return this.http.get<DiscordChannel[]>(`${this.base}/bots/${botId}/guilds/${guildId}/channels`);
  }

  listBindings(): Observable<DiscordBinding[]> {
    return this.http.get<DiscordBinding[]>(`${this.base}/bindings`);
  }

  addBinding(body: CreateDiscordBindingRequest): Observable<DiscordBinding> {
    return this.http.post<DiscordBinding>(`${this.base}/bindings`, body);
  }

  updateBinding(id: string, body: UpdateDiscordBindingRequest): Observable<DiscordBinding> {
    return this.http.patch<DiscordBinding>(`${this.base}/bindings/${id}`, body);
  }

  removeBinding(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/bindings/${id}`);
  }

  chatState(chatId: string): Observable<DiscordChatState> {
    return this.http.get<DiscordChatState>(`${this.base}/chats/${chatId}`);
  }

  setPostNextReply(chatId: string, bindingId: string, on: boolean): Observable<void> {
    const url = `${this.base}/chats/${chatId}/pending/${bindingId}`;
    return on ? this.http.put<void>(url, {}) : this.http.delete<void>(url);
  }
}

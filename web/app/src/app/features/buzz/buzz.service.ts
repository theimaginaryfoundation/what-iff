import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { apiErrorEnvelope, hasErrorCode } from '../../core/utils/api-error.helpers';
import { relayAvailability } from '../relay/relay-availability';
import { RELAY_EMPTY_ALLOW_NOTE, RELAY_UNRESTRICTED_WARNING } from '../relay/relay-copy';
import { RelayIdKind } from '../relay/relay-id-chips.component';
import { fullNpub, normalizePubkey, shortNpub } from './npub';

/** Buzz relay. Shapes mirror internal/models/buzz.go and internal/buzzplugin/handlers.go. */

export type BuzzAgentStatus = 'active' | 'disabled';
export type BuzzCommunityStatus = 'active' | 'needs_access' | 'error' | 'disabled';
export type BuzzBindingStatus = 'active' | 'broken';

export interface BuzzCommunity {
  id: string;
  agent_id: string;
  /** Normalised to wss://host[:port] by the server. */
  relay_url: string;
  name: string;
  relay_pubkey: string;
  /** Whether the owner gave an attestation for the persona's key (the attestation itself is never returned). */
  attested: boolean;
  owner_pubkey?: string | null;
  /** The join terms version accepted for the persona. */
  policy_version?: string | null;
  status: BuzzCommunityStatus;
  last_error?: string | null;
  last_connected_at?: string | null;
  created_at: string;
  updated_at: string;
}

/** A persona's Buzz identity (its keypair; the secret never leaves the server) and its communities. */
export interface BuzzAgent {
  id: string;
  personality_id: string;
  pubkey: string;
  npub: string;
  status: BuzzAgentStatus;
  last_error?: string | null;
  created_at: string;
  updated_at: string;
  persona_name: string;
  communities: BuzzCommunity[];
}

export interface BuzzChannel {
  id: string;
  name: string;
  about?: string;
  /** Public channels can be joined; private ones need a member to add the persona. */
  public: boolean;
  /** Whether the persona is in the channel. */
  member: boolean;
  archived?: boolean;
}

export interface BuzzBinding {
  id: string;
  community_id: string;
  community_name?: string;
  chat_id: string;
  channel_id: string;
  channel_name: string;
  inbound_enabled: boolean;
  /** Hex pubkeys. */
  allow_pubkeys: string[];
  deny_pubkeys: string[];
  /** Answer mentions from other agents (events carrying an owner attestation). */
  answer_agents: boolean;
  /** The owner's acknowledgement that the bound thread is not sandboxed. */
  allow_unrestricted: boolean;
  /** Whether the bound thread is sandboxed right now (computed by the API; absent when unknown). */
  chat_sandboxed?: boolean;
  status: BuzzBindingStatus;
  last_error?: string | null;
  last_activity_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface BuzzMessageLink {
  id: string;
  binding_id: string;
  direction: 'inbound' | 'outbound';
  chat_message_id?: string | null;
  event_id?: string | null;
  thread_root: string;
  posted_event_ids: string[];
  author_pubkey?: string;
  author_name?: string;
  reply_to_link_id?: string | null;
  status: 'received' | 'pending' | 'sent' | 'failed';
  error?: string | null;
  attempts: number;
  created_at: string;
}

export interface BuzzChatState {
  bindings: BuzzBinding[];
  pending_binding_ids: string[];
  links: BuzzMessageLink[];
}

/** A community's join terms (Markdown), which the owner accepts for the persona before claiming an invite. */
export interface BuzzJoinPolicy {
  terms_markdown: string;
  privacy_markdown: string;
  age_attestation_required: boolean;
  version: string;
}

export interface BuzzPolicyPreview {
  name: string;
  relay_url: string;
  policy: BuzzJoinPolicy | null;
}

export interface AddBuzzCommunityRequest {
  /** The community's address, or an invite link to it. */
  relay_url: string;
  /** An invite link or code, when relay_url is not already one. */
  invite?: string;
  /** The owner's NIP-OA attestation for the persona's key (the JSON auth tag). */
  attestation?: string;
  /** The join terms version the owner accepted for the persona. */
  accept_terms_version?: string;
  /** The owner's confirmation of the community's age requirement, when it has one. */
  confirm_age?: boolean;
}

export interface UpdateBuzzCommunityRequest {
  enabled?: boolean;
  /** Replaces the attestation; "" removes it. */
  attestation?: string;
  invite?: string;
  accept_terms_version?: string;
  confirm_age?: boolean;
  /** Probe access again (after an admin added the persona, say). */
  recheck?: boolean;
}

export interface CreateBuzzBindingRequest {
  community_id: string;
  channel_id: string;
  channel_name: string;
  /** Omit for a new sandboxed relay thread. */
  chat_id?: string;
  inbound_enabled?: boolean;
  allow_pubkeys?: string[];
  deny_pubkeys?: string[];
  answer_agents?: boolean;
  /** Required to bind an existing thread that is not sandboxed. */
  allow_unrestricted?: boolean;
}

export interface UpdateBuzzBindingRequest {
  chat_id?: string;
  inbound_enabled?: boolean;
  allow_pubkeys?: string[];
  deny_pubkeys?: string[];
  answer_agents?: boolean;
  channel_name?: string;
  allow_unrestricted?: boolean;
  reactivate?: boolean;
}

/** What anyone allowed to mention the persona gets from a thread that is not sandboxed. */
export const BUZZ_UNRESTRICTED_WARNING = RELAY_UNRESTRICTED_WARNING;

/**
 * Buzz public keys in the allow and deny chips: typed or pasted as npub or hex, kept as hex (as the
 * API stores them), shown as a shortened npub with the full one on hover.
 */
export const BUZZ_PUBKEYS: RelayIdKind = {
  parse: normalizePubkey,
  invalid: 'Buzz public keys are an npub1… or 64 hex characters.',
  display: shortNpub,
  title: fullNpub,
  monospace: true,
};

/** The note shown while a binding's allow list is empty. */
export const BUZZ_EMPTY_ALLOW_NOTE = RELAY_EMPTY_ALLOW_NOTE;

/** "#general · Acme" for a binding (the community name is optional). */
export function buzzBindingLabel(b: Pick<BuzzBinding, 'channel_name' | 'channel_id' | 'community_name'>, communityName?: string): string {
  const channel = `#${b.channel_name || b.channel_id}`;
  const community = communityName ?? b.community_name;
  return community ? `${channel} · ${community}` : channel;
}

/**
 * The community's join terms when a request was refused because they have not been accepted
 * for the persona (409, code `terms_required`); null for any other failure.
 */
export function termsRequired(error: unknown): BuzzJoinPolicy | null {
  if (!(error instanceof HttpErrorResponse) || error.status !== 409 || !hasErrorCode(error, 'buzz_terms_required')) return null;
  const details = apiErrorEnvelope(error)?.details as { policy?: BuzzJoinPolicy | null } | undefined;
  return details?.policy ?? null;
}

/** What the owner agreed to in the terms dialog, sent with the resubmitted claim. */
export interface BuzzTermsAcceptance {
  accept_terms_version: string;
  confirm_age: boolean;
}

/** API client for /api/buzz (internal/buzzplugin). */
@Injectable({ providedIn: 'root' })
export class BuzzService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiUrl}/buzz`;
  private availability$: Observable<boolean> | null = null;

  /**
   * Whether the relay is on for this user. It is experimental: off unless the operator switches it
   * on (BUZZ_RELAY_ENABLED=true), and a build may turn it on per user. Where it is off, /api/buzz
   * answers 404: a 404 here means off. So does a request that never got an answer (status 0: the
   * network, or a response the browser would not let us read), because an experimental feature
   * should stay hidden when we cannot tell. Any other error means on, so it shows where the user
   * can see it. Asked once per session.
   */
  available(): Observable<boolean> {
    if (!this.availability$) {
      this.availability$ = relayAvailability(this.http, `${this.base}/bindings`, { failClosed: true });
    }
    return this.availability$;
  }

  listAgents(): Observable<BuzzAgent[]> {
    return this.http.get<BuzzAgent[]>(`${this.base}/agents`);
  }

  createAgent(personalityId: string): Observable<BuzzAgent> {
    return this.http.post<BuzzAgent>(`${this.base}/agents`, { personality_id: personalityId });
  }

  setAgentEnabled(id: string, enabled: boolean): Observable<BuzzAgent> {
    return this.http.patch<BuzzAgent>(`${this.base}/agents/${id}`, { enabled });
  }

  removeAgent(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/agents/${id}`);
  }

  /** Republishes the persona's name and portrait to every community; `failed` lists the relay URLs that refused. */
  syncProfile(id: string): Observable<{ published: number; failed: string[] }> {
    return this.http.post<{ published: number; failed: string[] }>(`${this.base}/agents/${id}/sync-profile`, {});
  }

  addCommunity(agentId: string, body: AddBuzzCommunityRequest): Observable<BuzzCommunity> {
    return this.http.post<BuzzCommunity>(`${this.base}/agents/${agentId}/communities`, body);
  }

  previewPolicy(relayUrl: string): Observable<BuzzPolicyPreview> {
    return this.http.get<BuzzPolicyPreview>(`${this.base}/policy`, { params: { relay_url: relayUrl } });
  }

  updateCommunity(id: string, body: UpdateBuzzCommunityRequest): Observable<BuzzCommunity> {
    return this.http.patch<BuzzCommunity>(`${this.base}/communities/${id}`, body);
  }

  removeCommunity(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/communities/${id}`);
  }

  listChannels(communityId: string): Observable<BuzzChannel[]> {
    return this.http.get<BuzzChannel[]>(`${this.base}/communities/${communityId}/channels`);
  }

  joinChannel(communityId: string, channelId: string): Observable<void> {
    return this.http.post<void>(`${this.base}/communities/${communityId}/channels/${channelId}/join`, {});
  }

  listBindings(): Observable<BuzzBinding[]> {
    return this.http.get<BuzzBinding[]>(`${this.base}/bindings`);
  }

  addBinding(body: CreateBuzzBindingRequest): Observable<BuzzBinding> {
    return this.http.post<BuzzBinding>(`${this.base}/bindings`, body);
  }

  updateBinding(id: string, body: UpdateBuzzBindingRequest): Observable<BuzzBinding> {
    return this.http.patch<BuzzBinding>(`${this.base}/bindings/${id}`, body);
  }

  removeBinding(id: string): Observable<void> {
    return this.http.delete<void>(`${this.base}/bindings/${id}`);
  }

  chatState(chatId: string): Observable<BuzzChatState> {
    return this.http.get<BuzzChatState>(`${this.base}/chats/${chatId}`);
  }

  setPostNextReply(chatId: string, bindingId: string, on: boolean): Observable<void> {
    const url = `${this.base}/chats/${chatId}/pending/${bindingId}`;
    return on ? this.http.put<void>(url, {}) : this.http.delete<void>(url);
  }
}

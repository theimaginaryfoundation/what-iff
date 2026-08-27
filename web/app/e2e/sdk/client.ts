import createClient from 'openapi-fetch';
import type { components, paths } from './schema.d.ts';

export type Personality = components['schemas']['Personality'];
export type Chat = components['schemas']['Chat'];
export type ChatMessage = components['schemas']['ChatMessage'];
export type ChatMessageResponse = components['schemas']['ChatMessageResponse'];
export type Memory = components['schemas']['Memory'];
export type MemoryCreateRequest = components['schemas']['MemoryCreateRequest'];
export type MemoryBatchCreateResponse = components['schemas']['MemoryBatchCreateResponse'];
export type Ritual = components['schemas']['Ritual'];
export type WebhookToken = components['schemas']['WebhookToken'];
export type CreateWebhookTokenResponse = components['schemas']['CreateWebhookTokenResponse'];
export type AgentJob = components['schemas']['AgentJob'];
export type AgentJobSchedulePreview = components['schemas']['AgentJobSchedulePreview'];
export type Job = components['schemas']['Job'];
export type RefreshTokenResponse = components['schemas']['RefreshTokenResponse'];
export type Model = components['schemas']['Model'];
export type PersonalityExpression = components['schemas']['PersonalityExpression'];
export type FileAttachment = components['schemas']['FileAttachment'];
export type ChatContext = components['schemas']['ChatContext'];
export type SearchResponse = components['schemas']['SearchResponse'];

/**
 * Hand-written wrapper around the generated OpenAPI types. This is the only
 * file the e2e suite talks HTTP through — see e2e/sdk/README.md.
 */
/**
 * Read per call, not once at module load. Configs publish
 * `E2E_API_BASE_URL` onto `process.env` while they evaluate (see
 * playwright.config.mock-llm.api.ts), and a module-level
 * constant would capture whatever was set at *import* time. That happens to be
 * correct today only because configs are evaluated before test files import
 * this module — a load-order coincidence, not a guarantee, and exactly the
 * kind that breaks silently later.
 */
function apiBaseUrl(): string {
  return process.env['E2E_API_BASE_URL'] ?? 'http://localhost:8080/api';
}

export type ApiClient = ReturnType<typeof createClient<paths>>;

/**
 * Builds a typed client against the backend. Pass a bearer token to have it
 * injected on every request; omit it for unauthenticated calls (register,
 * login).
 */
export function createApiClient(token?: string): ApiClient {
  return createClient<paths>({
    baseUrl: apiBaseUrl(),
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  });
}

export interface RegisterUserInput {
  username: string;
  email: string;
  password: string;
  termsAccepted?: boolean;
}

export interface LoginResult {
  accessToken: string;
  refreshToken: string;
}

/** Registers a new user and returns the tokens issued for the new account. */
export async function registerUser(client: ApiClient, user: RegisterUserInput): Promise<LoginResult> {
  const { data, error, response } = await client.POST('/user/register', {
    body: {
      username: user.username,
      email: user.email,
      password: user.password,
      terms_accepted: user.termsAccepted ?? true,
    },
  });
  if (error || !data) {
    // Status only: the request body held a password, and auth error responses
    // are the ones most likely to echo it back. See `safeDetail`.
    throw new ApiError(`register failed: ${response.status}`, response.status);
  }
  if (!data.access_token || !data.refresh_token) {
    throw new Error('register response missing tokens');
  }
  return { accessToken: data.access_token, refreshToken: data.refresh_token };
}

/** Logs in and returns the access/refresh tokens. Does not require an authenticated client. */
export async function loginUser(client: ApiClient, credentials: { username: string; password: string }): Promise<LoginResult> {
  const { data, error, response } = await client.POST('/user/login', {
    body: credentials,
  });
  if (error || !data) {
    // Status only — same reasoning as `registerUser`.
    throw new ApiError(`login failed: ${response.status}`, response.status);
  }
  if (!data.access_token || !data.refresh_token) {
    throw new Error('login response missing tokens');
  }
  return { accessToken: data.access_token, refreshToken: data.refresh_token };
}

/** Exchanges a refresh token for a new access/refresh token pair. Does not require an authenticated client. */
export async function refreshToken(client: ApiClient, refreshTokenValue: string): Promise<LoginResult> {
  const { data, error, response } = await client.POST('/user/refresh', {
    body: { refresh_token: refreshTokenValue },
  });
  if (error || !data) {
    // Status only — same reasoning as `registerUser`.
    throw new ApiError(`refresh failed: ${response.status}`, response.status);
  }
  if (!data.access_token || !data.refresh_token) {
    throw new Error('refresh response missing tokens');
  }
  return { accessToken: data.access_token, refreshToken: data.refresh_token };
}

/** Permanently deletes the account of the client's own authenticated user. */
export async function deleteUser(client: ApiClient): Promise<void> {
  const { error, response } = await client.DELETE('/user/delete', {});
  if (error) {
    // Status only — account endpoints get the same treatment as auth.
    throw new ApiError(`delete user failed: ${response.status}`, response.status);
  }
}

/** Thrown by `fail`; carries the HTTP status so callers can branch on it (e.g. treat 404 as already-gone). */
export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

const MAX_DETAIL_LENGTH = 200;

/**
 * Reduces a backend error body to a short, known-safe string for the thrown
 * message. Only the top-level `error`/`message`/`code` fields are read, and
 * only when they are strings — the body itself is never serialised, because
 * these messages land in Playwright output, traces, and CI logs, where an
 * echoed request payload (credentials, tokens) would then be durable. Callers
 * that need the full body should inspect the response themselves.
 */
function safeDetail(error: unknown): string {
  if (typeof error === 'string') {
    return error.slice(0, MAX_DETAIL_LENGTH);
  }
  if (error && typeof error === 'object') {
    for (const key of ['error', 'message', 'code'] as const) {
      const value = (error as Record<string, unknown>)[key];
      if (typeof value === 'string' && value) {
        return value.slice(0, MAX_DETAIL_LENGTH);
      }
    }
  }
  return '';
}

/**
 * Throws when a call came back non-2xx. Keeps the helpers below to "typed
 * call + fail loudly" with no business logic of their own.
 */
function fail(what: string, status: number, error: unknown): never {
  const detail = safeDetail(error);
  throw new ApiError(`${what} failed: ${status}${detail ? ` ${detail}` : ''}`, status);
}

// --- Models ----------------------------------------------------------------

/** Lists the models a user may select for a chat. Returns a bare array, not a paginated envelope. */
export async function listModels(client: ApiClient): Promise<Model[]> {
  const { data, error, response } = await client.GET('/model', {});
  if (error || !data) {
    fail('list models', response.status, error);
  }
  return data;
}

// --- Personalities ---------------------------------------------------------

export interface CreatePersonalityInput {
  name: string;
  systemPrompt: string;
}

/** Creates a personality. */
export async function createPersonality(client: ApiClient, input: CreatePersonalityInput): Promise<Personality> {
  const { data, error, response } = await client.POST('/personality', {
    body: {
      name: input.name,
      system_prompt: input.systemPrompt,
    },
  });
  if (error || !data) {
    fail('create personality', response.status, error);
  }
  return data;
}

export interface ListPersonalitiesParams {
  page?: number;
  limit?: number;
  name?: string;
  query?: string;
  /**
   * Comma-separated personality IDs, spelled exactly as the endpoint takes
   * them — this is a filter on the list, not a batch-get, and malformed
   * UUIDs come back 400 rather than being skipped.
   */
  personality_ids?: string;
}

export interface PersonalityPage {
  results: Personality[];
  /** Total matching personalities, ignoring pagination. */
  totalCount: number;
}

/**
 * Lists the authenticated user's personalities with the pagination envelope
 * intact. Callers that assert on how many personalities a filter matched
 * need `totalCount`; everything else wants `listPersonalities` below.
 */
export async function listPersonalitiesPage(client: ApiClient, params: ListPersonalitiesParams = {}): Promise<PersonalityPage> {
  const { data, error, response } = await client.GET('/personality', { params: { query: params } });
  if (error || !data) {
    fail('list personalities', response.status, error);
  }
  return { results: data.results ?? [], totalCount: data.total_count ?? 0 };
}

/** Lists the authenticated user's personalities. */
export async function listPersonalities(client: ApiClient, params: ListPersonalitiesParams = {}): Promise<Personality[]> {
  return (await listPersonalitiesPage(client, params)).results;
}

/** Fetches a single personality by ID. */
export async function getPersonality(client: ApiClient, id: string): Promise<Personality> {
  const { data, error, response } = await client.GET('/personality/{id}', { params: { path: { id } } });
  if (error || !data) {
    fail('get personality', response.status, error);
  }
  return data;
}

export interface UpdatePersonalityInput {
  name: string;
  systemPrompt: string;
  /**
   * Gallery image to use as the cover portrait. `null` clears it — which is
   * why this is spelled as an explicit optional-or-null rather than being
   * omitted, since omitting it is how you leave the cover alone.
   */
  coverImageId?: string | null;
}

/**
 * Replaces a personality. PUT, not PATCH: `name` and `system_prompt` are
 * required on every call, so callers pass the values they want to keep.
 */
export async function updatePersonality(client: ApiClient, id: string, input: UpdatePersonalityInput): Promise<Personality> {
  const { data, error, response } = await client.PUT('/personality/{id}', {
    params: { path: { id } },
    body: {
      name: input.name,
      system_prompt: input.systemPrompt,
      cover_image_id: input.coverImageId,
    },
  });
  if (error || !data) {
    fail('update personality', response.status, error);
  }
  return data;
}

// --- Personality expressions -----------------------------------------------

/** Lists a personality's user-defined expression slots. Returns a bare array. */
export async function listPersonalityExpressions(client: ApiClient, personalityId: string): Promise<PersonalityExpression[]> {
  const { data, error, response } = await client.GET('/personality/{id}/expressions', {
    params: { path: { id: personalityId } },
  });
  if (error || !data) {
    fail('list personality expressions', response.status, error);
  }
  return data;
}

export interface UpsertPersonalityExpressionInput {
  /** Gallery image to assign; `null` clears it, omitted leaves it unchanged. */
  imageId?: string | null;
  /** Label to set; `null` clears it, omitted leaves it unchanged. */
  label?: string | null;
}

/** Creates or updates one expression slot. Upsert: the same call makes a new key or edits an existing one. */
export async function upsertPersonalityExpression(
  client: ApiClient,
  personalityId: string,
  expressionKey: string,
  input: UpsertPersonalityExpressionInput,
): Promise<PersonalityExpression> {
  const { data, error, response } = await client.PUT('/personality/{id}/expressions/{expression_key}', {
    params: { path: { id: personalityId, expression_key: expressionKey } },
    body: { image_id: input.imageId, label: input.label },
  });
  if (error || !data) {
    fail('upsert personality expression', response.status, error);
  }
  return data;
}

/** Removes one expression slot from a personality. */
export async function deletePersonalityExpression(client: ApiClient, personalityId: string, expressionKey: string): Promise<void> {
  const { error, response } = await client.DELETE('/personality/{id}/expressions/{expression_key}', {
    params: { path: { id: personalityId, expression_key: expressionKey } },
  });
  if (error) {
    fail('delete personality expression', response.status, error);
  }
}

export interface UploadFileInput {
  filename: string;
  contentType: string;
  bytes: Uint8Array;
}

/**
 * Uploads a file attachment to a personality as `multipart/form-data`.
 *
 * `body` is a placeholder: the generated types model the binary field as a
 * string, and `bodySerializer` throws it away and builds the real FormData.
 * Going through the typed client anyway keeps the bearer token in one place
 * — a hand-rolled `fetch` here would have to re-plumb auth.
 */
export async function uploadPersonalityAttachment(client: ApiClient, personalityId: string, file: UploadFileInput): Promise<FileAttachment> {
  const { data, error, response } = await client.POST('/personality/{id}/file-attachment', {
    params: { path: { id: personalityId } },
    body: { attachment: file.filename },
    bodySerializer: () => {
      const form = new FormData();
      form.append('attachment', new Blob([new Uint8Array(file.bytes).buffer], { type: file.contentType }), file.filename);
      return form;
    },
  });
  if (error || !data) {
    fail('upload personality attachment', response.status, error);
  }
  return data;
}

/**
 * Lists the caller's file attachments, newest page first.
 *
 * `GET /file-attachment` was absent from the generated types until the path
 * item it lives under was un-nested in openapi.yaml, which is why this helper
 * is newer than the endpoint.
 */
export async function listFileAttachments(
  client: ApiClient,
  query: { page?: number; limit?: number } = {},
): Promise<FileAttachment[]> {
  const { data, error, response } = await client.GET('/file-attachment', { params: { query } });
  if (error || !data) {
    fail('list file attachments', response.status, error);
  }
  return data.results ?? [];
}

/** Deletes a file attachment, and the vendor-side file with it. */
export async function deleteFileAttachment(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/file-attachment/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete file attachment', response.status, error);
  }
}

/** Deletes a personality by ID. */
export async function deletePersonality(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/personality/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete personality', response.status, error);
  }
}

// --- Chats / threads -------------------------------------------------------

export interface CreateChatInput {
  name: string;
  personalityId?: string;
  modelId?: string;
  tags?: string[];
  isFavorite?: boolean;
}

/** Creates a chat thread. */
export async function createChat(client: ApiClient, input: CreateChatInput): Promise<Chat> {
  const { data, error, response } = await client.POST('/chat', {
    body: {
      name: input.name,
      personality_id: input.personalityId,
      model_id: input.modelId,
      tags: input.tags,
      is_favorite: input.isFavorite,
    },
  });
  if (error || !data) {
    fail('create chat', response.status, error);
  }
  return data;
}

/** Lists the authenticated user's chats. */
export async function listChats(client: ApiClient): Promise<Chat[]> {
  const { data, error, response } = await client.GET('/chat', {});
  if (error || !data) {
    fail('list chats', response.status, error);
  }
  return data.results ?? [];
}

/** Fetches a single chat thread by ID. */
export async function getChat(client: ApiClient, id: string): Promise<Chat> {
  const { data, error, response } = await client.GET('/chat/{id}', { params: { path: { id } } });
  if (error || !data) {
    fail('get chat', response.status, error);
  }
  return data;
}

export interface UpdateChatInput {
  name?: string;
  tags?: string[];
  isFavorite?: boolean;
  /** PATCH-only: hides the thread from default lists. */
  archived?: boolean;
  /** Reassigns the thread to a different model; the next turn generates with it. */
  modelId?: string;
  /** Reassigns the thread to a different personality. */
  personalityId?: string;
}

/**
 * Partially updates a chat (rename, tag, favorite, archive). Uses PATCH,
 * which is the only verb on this path that accepts `archived`.
 */
export async function updateChat(client: ApiClient, id: string, input: UpdateChatInput): Promise<Chat> {
  const { data, error, response } = await client.PATCH('/chat/{id}', {
    params: { path: { id } },
    body: {
      name: input.name,
      tags: input.tags,
      is_favorite: input.isFavorite,
      archived: input.archived,
      model_id: input.modelId,
      personality_id: input.personalityId,
    },
  });
  if (error || !data) {
    fail('update chat', response.status, error);
  }
  return data;
}

/** Deletes a chat thread by ID. */
export async function deleteChat(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/chat/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete chat', response.status, error);
  }
}

/**
 * Posts a user message to a chat. The backend answers 202 and generates the
 * reply asynchronously — this returns as soon as the message is accepted.
 */
export async function sendChatMessage(client: ApiClient, chatId: string, message: string): Promise<ChatMessageResponse> {
  const { data, error, response } = await client.POST('/chat/{chatId}/chat-message', {
    params: { path: { chatId } },
    body: { message, origin: 'User' },
  });
  if (error || !data) {
    fail('send chat message', response.status, error);
  }
  return data;
}

export interface ListChatMessagesParams {
  page?: number;
  limit?: number;
  origin?: components['schemas']['MessageOrigin'];
  search?: string;
}

/** Lists messages in a chat, paginated. */
export async function listChatMessages(client: ApiClient, chatId: string, params: ListChatMessagesParams = {}): Promise<ChatMessage[]> {
  const { data, error, response } = await client.GET('/chat/{chatId}/chat-message', {
    params: { path: { chatId }, query: params },
  });
  if (error || !data) {
    fail('list chat messages', response.status, error);
  }
  return data.results ?? [];
}

// --- Chat message bookmarks ------------------------------------------------

export type ChatMessageBookmark = components['schemas']['ChatMessageBookmark'];

/** Sets or clears a bookmark on one chat message. Returns the updated message. */
export async function setMessageBookmark(client: ApiClient, chatId: string, messageId: string, bookmarked: boolean): Promise<ChatMessage> {
  const { data, error, response } = await client.PATCH('/chat/{chatId}/chat-message/{messageId}/bookmark', {
    params: { path: { chatId, messageId } },
    body: { bookmarked },
  });
  if (error || !data) {
    fail('set message bookmark', response.status, error);
  }
  return data;
}

/** Lists all bookmarked messages in a chat as lightweight snippets. */
export async function listChatBookmarks(client: ApiClient, chatId: string): Promise<ChatMessageBookmark[]> {
  const { data, error, response } = await client.GET('/chat/{chatId}/bookmarks', {
    params: { path: { chatId } },
  });
  if (error || !data) {
    fail('list chat bookmarks', response.status, error);
  }
  return data;
}

// --- Chat context ----------------------------------------------------------

/** Reads the scratchpad and checkpoint summary backing a chat session. */
export async function getChatContext(client: ApiClient, chatId: string): Promise<ChatContext> {
  const { data, error, response } = await client.GET('/chat/{id}/context', { params: { path: { id: chatId } } });
  if (error || !data) {
    fail('get chat context', response.status, error);
  }
  return data;
}

/** Overwrites the active scratchpad for the chat's active personality (last write wins). */
export async function patchChatContext(client: ApiClient, chatId: string, activeScratchpad: string): Promise<ChatContext> {
  const { data, error, response } = await client.PATCH('/chat/{id}/context', {
    params: { path: { id: chatId } },
    body: { active_scratchpad: activeScratchpad },
  });
  if (error || !data) {
    fail('patch chat context', response.status, error);
  }
  return data;
}

// --- Search ------------------------------------------------------------------

export interface SearchParams {
  query: string;
  /** Comma-separated resource types, as the endpoint takes them. Omitted searches all five. */
  types?: string;
  limitPerType?: number;
}

/** Cross-resource command-palette search over chats, personalities, rituals, memories, and gallery images. */
export async function search(client: ApiClient, params: SearchParams): Promise<SearchResponse> {
  const { data, error, response } = await client.GET('/search', {
    params: { query: { query: params.query, types: params.types, limit_per_type: params.limitPerType } },
  });
  if (error || !data) {
    fail('search', response.status, error);
  }
  return data;
}

// --- Jobs --------------------------------------------------------------------

/** Fetches a background job by ID (e.g. the job a chat message spawns to generate its reply). */
export async function getJob(client: ApiClient, id: string): Promise<Job> {
  const { data, error, response } = await client.GET('/job/{id}', { params: { path: { id } } });
  if (error || !data) {
    fail('get job', response.status, error);
  }
  return data;
}

// --- Memories --------------------------------------------------------------

/** Creates a single memory. */
export async function createMemory(client: ApiClient, memory: MemoryCreateRequest): Promise<Memory> {
  const { data, error, response } = await client.POST('/memory', { body: memory });
  if (error || !data) {
    fail('create memory', response.status, error);
  }
  return data;
}

/** Creates several memories in one call. */
export async function createMemoriesBatch(
  client: ApiClient,
  items: MemoryCreateRequest[],
  allOrNone = false,
): Promise<MemoryBatchCreateResponse> {
  const { data, error, response } = await client.POST('/memory/batch', {
    body: { items, all_or_none: allOrNone },
  });
  if (error || !data) {
    fail('create memories batch', response.status, error);
  }
  return data;
}

/** Deletes a memory by ID. */
export async function deleteMemory(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/memory/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete memory', response.status, error);
  }
}

export interface ListMemoriesParams {
  page?: number;
  limit?: number;
  chatId?: string;
  level?: components['schemas']['Memory']['level'];
  starred?: boolean;
  query?: string;
}

/** Lists the authenticated user's memories, with optional filtering. */
export async function listMemories(client: ApiClient, params: ListMemoriesParams = {}): Promise<Memory[]> {
  const { data, error, response } = await client.GET('/memory', {
    params: {
      query: {
        page: params.page,
        limit: params.limit,
        chat_id: params.chatId,
        level: params.level,
        starred: params.starred,
        query: params.query,
      },
    },
  });
  if (error || !data) {
    fail('list memories', response.status, error);
  }
  return data.results ?? [];
}

// --- Rituals (skills) ------------------------------------------------------

export interface CreateRitualInput {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personalityId?: string;
}

/** Creates a user ritual. */
export async function createRitual(client: ApiClient, input: CreateRitualInput): Promise<Ritual> {
  const { data, error, response } = await client.POST('/ritual', {
    body: {
      name: input.name,
      description: input.description,
      content: input.content,
      hotkeys: input.hotkeys,
      personality_id: input.personalityId,
    },
  });
  if (error || !data) {
    fail('create ritual', response.status, error);
  }
  return data;
}

/** Lists the authenticated user's rituals. */
export async function listRituals(client: ApiClient): Promise<Ritual[]> {
  const { data, error, response } = await client.GET('/ritual', {});
  if (error || !data) {
    fail('list rituals', response.status, error);
  }
  return data.results ?? [];
}

/** Lists the built-in system rituals (not user-owned, not deletable). */
export async function listSystemRituals(client: ApiClient): Promise<Ritual[]> {
  const { data, error, response } = await client.GET('/ritual/system', {});
  if (error || !data) {
    fail('list system rituals', response.status, error);
  }
  return data;
}

/** Deletes a ritual by ID. */
export async function deleteRitual(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/ritual/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete ritual', response.status, error);
  }
}

// --- Webhook tokens --------------------------------------------------------

/** Creates a webhook token; the raw `api_token` is only returned here, once. */
export async function createWebhookToken(client: ApiClient, name: string): Promise<CreateWebhookTokenResponse> {
  const { data, error, response } = await client.POST('/webhook-tokens', { body: { name } });
  if (error || !data) {
    fail('create webhook token', response.status, error);
  }
  return data;
}

/** Lists the authenticated user's webhook tokens. */
export async function listWebhookTokens(client: ApiClient): Promise<WebhookToken[]> {
  const { data, error, response } = await client.GET('/webhook-tokens', {});
  if (error || !data) {
    fail('list webhook tokens', response.status, error);
  }
  return data;
}

/** Revokes (deletes) a webhook token by ID. */
export async function revokeWebhookToken(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/webhook-tokens/{id}', { params: { path: { id } } });
  if (error) {
    fail('revoke webhook token', response.status, error);
  }
}

// --- Agent jobs ------------------------------------------------------------
//
// NOTE: there is no `POST /agent-job` in openapi.yaml — agent jobs are created
// as a side effect of scheduling from a chat, so there's no create helper here.

/** Lists the authenticated user's agent jobs. */
export async function listAgentJobs(client: ApiClient): Promise<AgentJob[]> {
  const { data, error, response } = await client.GET('/agent-job', {});
  if (error || !data) {
    fail('list agent jobs', response.status, error);
  }
  return data.results ?? [];
}

/** Previews how the backend interprets a natural-language schedule string. */
export async function parseSchedule(client: ApiClient, scheduleInput: string, timezone?: string): Promise<AgentJobSchedulePreview> {
  const { data, error, response } = await client.POST('/agent-job/schedule/parse', {
    body: { schedule_input: scheduleInput, timezone },
  });
  if (error || !data) {
    fail('parse schedule', response.status, error);
  }
  return data;
}

/** Deletes an agent job by ID. */
export async function deleteAgentJob(client: ApiClient, id: string): Promise<void> {
  const { error, response } = await client.DELETE('/agent-job/{id}', { params: { path: { id } } });
  if (error) {
    fail('delete agent job', response.status, error);
  }
}

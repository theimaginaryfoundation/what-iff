import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpResponse } from '@angular/common/http';
import { Observable, BehaviorSubject, throwError, of, forkJoin } from 'rxjs';
import { catchError, tap, map, concatMap, mergeMap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { clearRecentOpenedThreadIds } from '../../features/chat/helpers/thread-list.helpers';
import {
  Chat,
  ChatContext,
  ChatFilters,
  CreateChatRequest,
  PatchChatContextRequest,
  PatchChatRequest,
  UpdateChatRequest
} from '../models/chat.model';
import { ChatMessageResponse } from '../models/message.model';
import { PaginatedResponse } from '../models/common.model';
import { Job } from '../models/job.model';
import { apiErrorMessage } from '../utils/api-error.helpers';

interface MarkChatReadResponse {
  updated_count: number;
}

/** Hard caps for {@link ChatService.listAllChats} to bound memory and HTTP fan-out on large accounts. */
export const LIST_ALL_CHATS_MAX_TOTAL = 1000;
export const LIST_ALL_CHATS_MAX_PAGES = 25;

export interface ListAllChatsResult {
  chats: Chat[];
  /** True when results were cut off by {@link LIST_ALL_CHATS_MAX_TOTAL} or {@link LIST_ALL_CHATS_MAX_PAGES}. */
  truncated: boolean;
}

/** Max pages merged when {@link ChatService.listChats} loads page 1 for the sidebar (single burst). */
const LIST_CHATS_SIDEBAR_MAX_PAGES = 500;

@Injectable({
  providedIn: 'root'
})
export class ChatService {
  private http: HttpClient = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/chat`;

  // Current active chat
  private activeChatSubject = new BehaviorSubject<Chat | null>(null);
  public activeChat$ = this.activeChatSubject.asObservable();

  // Chat list for sidebar
  private chatsSubject = new BehaviorSubject<Chat[]>([]);
  public chats$ = this.chatsSubject.asObservable();

  createChat(chatData: CreateChatRequest): Observable<Chat> {
    return this.http.post<Chat>(this.apiUrl, chatData).pipe(
      tap(chat => this.cacheCreatedChat(chat)),
      catchError(err => this.handleError(err)),
    );
  }

  /**
   * Single logical load for the sidebar when page === 1: fetches remaining HTTP pages (capped) so the
   * list matches server totals. For page > 1, returns that page only (infinite-scroll style callers).
   */
  listChats(page: number = 1, limit: number = 50, filters?: ChatFilters): Observable<PaginatedResponse<Chat>> {
    return this.fetchChatsPage(page, limit, filters).pipe(
      mergeMap(first => {
        if (page !== 1) {
          return of(first);
        }
        const total = first.total_count ?? 0;
        const firstRows = first.results ?? [];
        if (firstRows.length >= total || total === 0 || limit < 1) {
          return of(first);
        }
        const totalPages = Math.min(Math.ceil(total / limit) || 1, LIST_CHATS_SIDEBAR_MAX_PAGES);
        if (totalPages <= 1) {
          return of(first);
        }
        const baseParams = this.chatListParams(1, limit, filters);
        const rest: Observable<PaginatedResponse<Chat>>[] = [];
        for (let p = 2; p <= totalPages; p++) {
          const paged: Record<string, string> = { ...baseParams, page: p.toString() };
          rest.push(this.http.get<PaginatedResponse<Chat>>(this.apiUrl, { params: paged }));
        }
        return forkJoin(rest).pipe(
          map(pages => {
            const merged = [...firstRows];
            for (const r of pages) {
              merged.push(...(r.results ?? []));
            }
            return {
              ...first,
              results: merged,
              page: 1
            };
          })
        );
      }),
      tap(response => {
        if (page === 1) {
          this.chatsSubject.next(response.results);
        } else {
          const currentChats = this.chatsSubject.getValue();
          this.chatsSubject.next([...currentChats, ...response.results]);
        }
      }),
      catchError(this.handleError)
    );
  }

  /**
   * Follows pagination until all chats are loaded (same filters each page), subject to
   * {@link LIST_ALL_CHATS_MAX_TOTAL} and {@link LIST_ALL_CHATS_MAX_PAGES}.
   * Updates chatsSubject with the merged list for sidebar / layout consumers.
   */
  listAllChats(limitPerPage = 50, filters?: ChatFilters): Observable<ListAllChatsResult> {
    const gather = (page: number, accumulated: Chat[]): Observable<ListAllChatsResult> =>
      this.fetchChatsPage(page, limitPerPage, filters).pipe(
        concatMap(response => {
          let merged = [...accumulated, ...response.results];
          let truncated = false;
          if (merged.length > LIST_ALL_CHATS_MAX_TOTAL) {
            merged = merged.slice(0, LIST_ALL_CHATS_MAX_TOTAL);
            truncated = true;
          }
          const totalPages = Math.max(1, Math.ceil(response.total_count / limitPerPage));
          const naturalDone = page >= totalPages || response.results.length === 0;
          const pageCapHit = page >= LIST_ALL_CHATS_MAX_PAGES && !naturalDone;
          if (pageCapHit) {
            truncated = true;
          }
          const stop =
            naturalDone || truncated || merged.length >= LIST_ALL_CHATS_MAX_TOTAL || page >= LIST_ALL_CHATS_MAX_PAGES;
          if (!stop) {
            return gather(page + 1, merged);
          }
          return of({ chats: merged, truncated });
        }),
        catchError(this.handleError)
      );
    return gather(1, []).pipe(tap(({ chats }) => this.chatsSubject.next(chats)));
  }

  private chatListParams(page: number, limit: number, filters?: ChatFilters): Record<string, string> {
    const params: Record<string, string> = {
      page: page.toString(),
      limit: limit.toString(),
    };
    if (filters) {
      if (filters.name) params['name'] = filters.name;
      if (filters.search) params['search'] = filters.search;
      if (filters.tag) params['tag'] = filters.tag;
      if (filters.personality_id) params['personality_id'] = filters.personality_id;
      if (filters.is_favorite !== undefined) params['is_favorite'] = String(filters.is_favorite);
      if (filters.archived !== undefined) params['archived'] = String(filters.archived);
      if (filters.min_date) params['min_date'] = filters.min_date;
      if (filters.max_date) params['max_date'] = filters.max_date;
      if (filters.source) params['source'] = filters.source;
      if (filters.ids) params['ids'] = filters.ids;
    }
    return params;
  }

  private fetchChatsPage(page: number, limit: number, filters?: ChatFilters): Observable<PaginatedResponse<Chat>> {
    return this.http.get<PaginatedResponse<Chat>>(this.apiUrl, { params: this.chatListParams(page, limit, filters) });
  }

  /**
   * Fetch a single page of chats WITHOUT mutating the sidebar cache (`chatsSubject`). Used by the
   * post-import picker, which lists threads created in the current import run and must not
   * disturb the active list.
   */
  listChatsPage(page = 1, limit = 30, filters?: ChatFilters): Observable<PaginatedResponse<Chat>> {
    return this.fetchChatsPage(page, limit, filters).pipe(catchError(this.handleError));
  }

  getChat(chatId: string): Observable<Chat> {
    return this.http.get<Chat>(`${this.apiUrl}/${chatId}`)
      .pipe(
        tap(chat => {
          this.activeChatSubject.next(chat);
        }),
        catchError(this.handleError)
      );
  }

  getChatContext(chatId: string): Observable<ChatContext> {
    return this.http.get<ChatContext>(`${this.apiUrl}/${chatId}/context`)
      .pipe(catchError(this.handleError));
  }

  patchChatContext(chatId: string, payload: PatchChatContextRequest): Observable<ChatContext> {
    return this.http.patch<ChatContext>(`${this.apiUrl}/${chatId}/context`, payload)
      .pipe(catchError(this.handleError));
  }

  patchChat(chatId: string, chatData: PatchChatRequest): Observable<Chat> {
    return this.http.patch<Chat>(`${this.apiUrl}/${chatId}`, chatData)
      .pipe(
        tap(updatedChat => {
          const currentChats = this.chatsSubject.getValue();
          let nextChats: Chat[];
          if (updatedChat.archived === true) {
            nextChats = currentChats.filter(chat => chat.id !== chatId);
          } else {
            const exists = currentChats.some(c => c.id === chatId);
            if (exists) {
              nextChats = currentChats.map(chat => (chat.id === chatId ? updatedChat : chat));
            } else {
              nextChats = [updatedChat, ...currentChats];
            }
          }
          this.chatsSubject.next(nextChats);

          // Update active chat if it's the current one
          const activeChat = this.activeChatSubject.getValue();
          if (activeChat && activeChat.id === chatId) {
            this.activeChatSubject.next(updatedChat);
          }
        }),
        catchError(this.handleError)
      );
  }

  updateChat(chatId: string, chatData: UpdateChatRequest): Observable<Chat> {
    return this.http.put<Chat>(`${this.apiUrl}/${chatId}`, chatData)
      .pipe(
        tap(updatedChat => {
          // Update in local chat list
          const currentChats = this.chatsSubject.getValue();
          const updatedChats = currentChats.map(chat =>
            chat.id === chatId ? updatedChat : chat
          );
          this.chatsSubject.next(updatedChats);

          // Update active chat if it's the current one
          const activeChat = this.activeChatSubject.getValue();
          if (activeChat && activeChat.id === chatId) {
            this.activeChatSubject.next(updatedChat);
          }
        }),
        catchError(this.handleError)
      );
  }

  deleteChat(chatId: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${chatId}`)
      .pipe(
        tap(() => {
          // Remove from local chat list
          const currentChats = this.chatsSubject.getValue();
          const filteredChats = currentChats.filter(chat => chat.id !== chatId);
          this.chatsSubject.next(filteredChats);

          // Clear active chat if it was deleted
          const activeChat = this.activeChatSubject.getValue();
          if (activeChat && activeChat.id === chatId) {
            this.activeChatSubject.next(null);
          }
        }),
        catchError(this.handleError)
      );
  }

  markChatRead(chatId: string): Observable<MarkChatReadResponse> {
    return this.http.post<MarkChatReadResponse>(`${this.apiUrl}/${chatId}/mark-read`, {})
      .pipe(
        tap(() => {
          const currentChats = this.chatsSubject.getValue();
          const updatedChats = currentChats.map(chat =>
            chat.id === chatId ? { ...chat, unread_count: 0 } : chat
          );
          this.chatsSubject.next(updatedChats);

          const activeChat = this.activeChatSubject.getValue();
          if (activeChat && activeChat.id === chatId) {
            this.activeChatSubject.next({ ...activeChat, unread_count: 0 });
          }
        }),
        catchError(this.handleError)
      );
  }

  createWelcomeMessage(chatId: string): Observable<ChatMessageResponse | null> {
    return this.http
      .post<ChatMessageResponse>(`${this.apiUrl}/${chatId}/welcome-message`, {}, { observe: 'response' })
      .pipe(
        map((response: HttpResponse<ChatMessageResponse>) => (response.status === 204 ? null : response.body)),
        catchError(this.handleError)
      );
  }

  exportChat(chatId: string): Observable<void> {
    return this.http.get(`${this.apiUrl}/${chatId}/export`, {
      responseType: 'blob',
      observe: 'response'
    }).pipe(
      tap(response => {
        // Extract filename from Content-Disposition header if available
        const contentDisposition = response.headers.get('Content-Disposition');
        let filename = 'chat-export.zip';

        if (contentDisposition) {
          const filenameMatch = contentDisposition.match(/filename="(.+)"/);
          if (filenameMatch && filenameMatch[1]) {
            filename = filenameMatch[1];
          }
        }

        // Create a download link and trigger it
        const blob = response.body;
        if (blob) {
          const url = window.URL.createObjectURL(blob);
          const link = document.createElement('a');
          link.href = url;
          link.download = filename;
          document.body.appendChild(link);
          link.click();
          document.body.removeChild(link);
          window.URL.revokeObjectURL(url);
        }
      }),
      // Map to void since we're handling the download in the tap
      map(() => void 0),
      catchError(this.handleError)
    );
  }

  /**
   * Upload an OpenAI/Anthropic conversations.json export. The server stages the file and runs the
   * import as a background job, responding 202 with the Job; poll it (via JobService) for progress.
   * The caller is responsible for extracting conversations.json from a .zip client-side beforehand.
   */
  importConversations(file: Blob, filename = 'conversations.json'): Observable<Job> {
    const form = new FormData();
    form.append('file', file, filename);
    return this.http.post<Job>(`${this.apiUrl}/import`, form).pipe(
      catchError(this.handleError),
    );
  }

  setActiveChat(chat: Chat | null): void {
    this.activeChatSubject.next(chat);
  }

  getActiveChat(): Chat | null {
    return this.activeChatSubject.getValue();
  }

  // Last chat ID management for session persistence
  setLastChatId(chatId: string): void {
    localStorage.setItem('lastChatId', chatId);
  }

  getLastChatId(): string | null {
    return localStorage.getItem('lastChatId');
  }

  clearLastChatId(): void {
    localStorage.removeItem('lastChatId');
  }

  /**
   * Clear all cached chat data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.activeChatSubject.next(null);
    this.chatsSubject.next([]);
    this.clearLastChatId();
    clearRecentOpenedThreadIds();
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An error occurred');

    console.error('Chat Service Error:', errorMessage);
    return throwError(() => new Error(errorMessage));
  }

  private cacheCreatedChat(chat: Chat): void {
    // Add to local chat list.
    const currentChats = this.chatsSubject.getValue();
    this.chatsSubject.next([chat, ...currentChats]);
    // Set as active chat.
    this.activeChatSubject.next(chat);
  }

}

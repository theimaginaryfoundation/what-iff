import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, BehaviorSubject, throwError } from 'rxjs';
import { catchError, map, tap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import {
  ChatMessage,
  ChatMessageFilters,
  CreateChatMessageRequest,
  ChatMessageResponse,
  MessageBookmark,
  MessageReadStatus
} from '../models/message.model';
import { PaginatedResponse } from '../models/common.model';

@Injectable({
  providedIn: 'root'
})
export class MessageService {
  private http: HttpClient = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/chat`;

  // Messages for current active chat
  private messagesSubject = new BehaviorSubject<ChatMessage[]>([]);
  public messages$ = this.messagesSubject.asObservable();

  // Typing state for assistant
  private isAssistantTypingSubject = new BehaviorSubject<boolean>(false);
  public isAssistantTyping$ = this.isAssistantTypingSubject.asObservable();

  // Current active chat ID for message filtering
  private currentChatId: string | null = null;

  // The active optimistic bookmark request for each loaded message. A newer request or
  // reconciliation supersedes its token so an older response cannot overwrite newer state.
  private bookmarkOperations = new Map<string, symbol>();

  setCurrentChatId(chatId: string | null): void {
    this.currentChatId = chatId;
  }

  getCurrentChatId(): string | null {
    return this.currentChatId;
  }

  listMessages(chatId: string, page: number = 1, limit: number = 50, filters?: ChatMessageFilters): Observable<PaginatedResponse<ChatMessage>> {
    let params: any = { page: page.toString(), limit: limit.toString() };

    if (filters) {
      if (filters.origin) params.origin = filters.origin;
      if (filters.search) params.search = filters.search;
      if (filters.min_date) params.min_date = filters.min_date;
      if (filters.max_date) params.max_date = filters.max_date;
    }

    return this.http.get<PaginatedResponse<ChatMessage>>(`${this.apiUrl}/${chatId}/chat-message`, { params })
      .pipe(
        tap(response => {
          if (page === 1) {
            // Replace message list on first page
            this.messagesSubject.next(response.results.reverse()); // Reverse to show newest at bottom
          } else {
            // Prepend older messages for infinite scroll up. Dedup by id: offset-based pages
            // can overlap what we already have once newer messages arrive, and duplicate ids
            // would break `track message.id` rendering.
            const currentMessages = this.messagesSubject.getValue();
            const known = new Set(currentMessages.map(m => m.id));
            const older = response.results.reverse().filter(m => !known.has(m.id));
            if (older.length > 0) {
              this.messagesSubject.next([...older, ...currentMessages]);
            }
          }
        }),
        catchError(this.handleError)
      );
  }

  /**
   * Non-destructively reconcile the newest page into the loaded list: update messages already
   * present (streaming→final, read status, bookmarks, context breakdown) and append any that
   * arrived while away — without dropping older loaded pages or moving scroll. Returns how many
   * new messages were appended, the server total, and whether a gap was detected (more than a
   * page arrived while away, so the loaded tail no longer overlaps the newest page — the caller
   * decides whether a full reload is worthwhile).
   */
  reconcileLatestPage(chatId: string, limit: number = 50): Observable<{ appended: number; total: number; gap: boolean }> {
    const params = { page: '1', limit: limit.toString() };
    return this.http
      .get<PaginatedResponse<ChatMessage>>(`${this.apiUrl}/${chatId}/chat-message`, { params })
      .pipe(
        map(response => {
          const fresh = [...response.results].reverse(); // oldest-first
          const total = response.total_count ?? fresh.length;
          const current = this.messagesSubject.getValue();
          if (current.length === 0) {
            this.messagesSubject.next(fresh);
            return { appended: fresh.length, total, gap: false };
          }
          const known = new Set(current.map(m => m.id));
          const hasOverlap = fresh.some(m => known.has(m.id));
          if (!hasOverlap) {
            // The loaded tail is older than the entire newest page — merging would leave a hole.
            return { appended: 0, total, gap: true };
          }
          const freshById = new Map(fresh.map(m => [m.id, m]));
          // A server reconciliation is newer than a pending optimistic bookmark update.
          fresh.forEach(message => this.bookmarkOperations.delete(message.id));
          const merged = current.map(m => freshById.get(m.id) ?? m);
          const appended = fresh.filter(m => !known.has(m.id));
          this.messagesSubject.next(appended.length > 0 ? [...merged, ...appended] : merged);
          return { appended: appended.length, total, gap: false };
        }),
        catchError(this.handleError),
      );
  }

  sendMessage(chatId: string, messageData: CreateChatMessageRequest): Observable<ChatMessageResponse> {
    let tz: string | undefined;
    try {
      tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    } catch {
      tz = undefined;
    }

    const payload: CreateChatMessageRequest = {
      ...messageData,
      client_timezone: messageData.client_timezone ?? tz
    };

    const optimisticId = `temp-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
    const optimisticMessage: ChatMessage = {
      id: optimisticId,
      chat_id: chatId,
      message: messageData.message,
      origin: messageData.origin,
      read_status: 'read',
      response_id: messageData.response_id,
      sent_at: new Date().toISOString(),
      attachments: messageData.attachments,
      rituals: messageData.rituals
    };
    this.addMessageToList(optimisticMessage);
    if (messageData.origin === 'User') {
      this.setAssistantTyping(true);
    }

    return this.http.post<ChatMessageResponse>(`${this.apiUrl}/${chatId}/chat-message`, payload)
      .pipe(
        tap(response => {
          const userMessage: ChatMessage = {
            id: response.id,
            chat_id: chatId,
            message: messageData.message,
            origin: messageData.origin,
            read_status: 'read',
            response_id: messageData.response_id,
            sent_at: optimisticMessage.sent_at,
            attachments: messageData.attachments,
            rituals: messageData.rituals
          };
          this.replaceMessageById(optimisticId, userMessage);
        }),
        catchError(error => {
          this.removeMessageById(optimisticId);
          this.setAssistantTyping(false);
          console.error('Error sending message:', error);
          return this.handleError(error);
        })
      );
  }

  getMessage(messageId: string): Observable<ChatMessage> {
    return this.http.get<ChatMessage>(`${this.apiUrl}/chat-message/${messageId}`)
      .pipe(catchError(this.handleError));
  }

  /** List all bookmarked messages for a chat (complete, regardless of pagination). */
  listBookmarks(chatId: string): Observable<MessageBookmark[]> {
    return this.http.get<MessageBookmark[]>(`${this.apiUrl}/${chatId}/bookmarks`)
      .pipe(catchError(this.handleError));
  }

  /**
   * Toggle a message bookmark. Updates the in-memory list optimistically and reconciles with
   * the server response (reverting on error).
   */
  setBookmark(chatId: string, messageId: string, bookmarked: boolean): Observable<ChatMessage> {
    const previousMessage = this.messagesSubject.getValue().find(message => message.id === messageId);
    const previousBookmarked = previousMessage?.bookmarked;
    const operation = Symbol(messageId);
    this.bookmarkOperations.set(messageId, operation);
    this.patchMessageInList(messageId, { bookmarked });
    return this.http
      .patch<ChatMessage>(`${this.apiUrl}/${chatId}/chat-message/${messageId}/bookmark`, { bookmarked })
      .pipe(
        tap(updated => {
          if (this.bookmarkOperations.get(messageId) === operation) {
            this.bookmarkOperations.delete(messageId);
            this.patchMessageInList(messageId, { bookmarked: updated.bookmarked });
          }
        }),
        catchError(error => {
          if (this.bookmarkOperations.get(messageId) === operation) {
            this.bookmarkOperations.delete(messageId);
            const currentMessage = this.messagesSubject.getValue().find(message => message.id === messageId);
            if (previousMessage && currentMessage) {
              this.patchMessageInList(messageId, { bookmarked: previousBookmarked });
            }
          }
          return this.handleError(error);
        }),
      );
  }

  /** Shallow-merge a patch onto a message already in the list (no-op if absent). */
  private patchMessageInList(messageId: string, patch: Partial<ChatMessage>): void {
    const current = this.messagesSubject.getValue();
    const idx = current.findIndex(m => m.id === messageId);
    if (idx < 0) return;
    const next = [...current];
    next[idx] = { ...next[idx], ...patch };
    this.messagesSubject.next(next);
  }

  /** Re-run async generation for an existing user message (same turn). */
  retryUserMessage(chatId: string, messageId: string): Observable<ChatMessageResponse> {
    return this.http
      .post<ChatMessageResponse>(`${this.apiUrl}/${chatId}/chat-message/${messageId}/retry`, {})
      .pipe(
        tap(() => {
          const cur = this.messagesSubject.getValue();
          const idx = cur.findIndex(m => m.id === messageId);
          if (idx >= 0) {
            const next = [...cur];
            next[idx] = { ...next[idx], last_error_message: null };
            this.messagesSubject.next(next);
          }
        }),
        catchError(this.handleError),
      );
  }

  addMessageToList(message: ChatMessage): void {
    // Only add message if it belongs to the currently active chat
    if (this.currentChatId && message.chat_id !== this.currentChatId) {
      return;
    }

    const currentMessages = this.messagesSubject.getValue();
    const idx = currentMessages.findIndex(m => m.id === message.id);
    if (idx >= 0) {
      const next = [...currentMessages];
      next[idx] = message;
      this.messagesSubject.next(next);
      return;
    }
    this.messagesSubject.next([...currentMessages, message]);
  }

  private replaceMessageById(fromId: string, replacement: ChatMessage): void {
    const currentMessages = this.messagesSubject.getValue();
    const idx = currentMessages.findIndex(m => m.id === fromId);
    if (idx < 0) {
      this.addMessageToList(replacement);
      return;
    }
    const next = [...currentMessages];
    next[idx] = replacement;
    this.messagesSubject.next(next);
  }

  private removeMessageById(id: string): void {
    const currentMessages = this.messagesSubject.getValue();
    const next = currentMessages.filter(message => message.id !== id);
    if (next.length === currentMessages.length) return;
    this.messagesSubject.next(next);
  }

  addAssistantMessage(message: ChatMessage): void {
    // Only process assistant message if it belongs to the currently active chat
    if (this.currentChatId && message.chat_id !== this.currentChatId) {
      return;
    }

    this.setAssistantTyping(false);
    this.addMessageToList(message);
  }

  addErrorMessage(chatId: string, error: string): void {
    // Only add error message if it belongs to the currently active chat
    if (this.currentChatId && chatId !== this.currentChatId) {
      return;
    }

    this.setAssistantTyping(false);
    const errorMessage: ChatMessage = {
      id: `error-${Date.now()}`,
      chat_id: chatId,
      message: `Error: ${error}`,
      origin: 'Assistant',
      read_status: 'read',
      sent_at: new Date().toISOString()
    };
    this.addMessageToList(errorMessage);
  }

  markAssistantMessagesRead(chatId: string): void {
    const currentMessages = this.messagesSubject.getValue();
    const updatedMessages = currentMessages.map((message) => {
      if (message.chat_id !== chatId) {
        return message;
      }
      if (message.origin !== 'Assistant') {
        return message;
      }
      if (message.read_status !== 'unread') {
        return message;
      }
      return {
        ...message,
        read_status: 'read' as MessageReadStatus
      };
    });
    this.messagesSubject.next(updatedMessages);
  }

  setAssistantTyping(isTyping: boolean): void {
    this.isAssistantTypingSubject.next(isTyping);
  }

  clearMessages(): void {
    this.messagesSubject.next([]);
    this.setAssistantTyping(false);
  }

  /**
   * Clear all cached message data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.messagesSubject.next([]);
    this.setAssistantTyping(false);
    this.currentChatId = null;
  }

  getMessages(): ChatMessage[] {
    return this.messagesSubject.getValue();
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    // Preserve HttpErrorResponse so callers can inspect status/body (e.g. quota_exceeded).
    console.error('Message Service Error:', error);
    return throwError(() => error);
  }
}

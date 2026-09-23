import { Injectable, OnDestroy, Signal, WritableSignal, computed, effect, inject, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { firstValueFrom, Subscription, finalize } from 'rxjs';
import { take } from 'rxjs/operators';

import { ChatService } from '../../core/services/chat.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { ChatStreamingService } from '../../core/services/chat-streaming.service';
import { DraftMessageService } from '../../core/services/draft-message.service';
import { JobService } from '../../core/services/job.service';
import { MessageService } from '../../core/services/message.service';
import { Chat } from '../../core/models/chat.model';
import { FileAttachment, PendingFileAttachment } from '../../core/models/file-attachment.model';
import { ChatMessage } from '../../core/models/message.model';
import { Model } from '../../core/models/model.model';
import { Ritual } from '../../core/models/ritual.model';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import {
  AUTOSAVE_DEBOUNCE_MS,
  CODE_BLOCK_CHUNK_SIZE,
  MESSAGE_JUMP_PAGE_SIZE,
  MESSAGE_LIST_PAGE_SIZE,
  STREAMING_INTERVAL_MS,
  STREAMING_SCROLL_CHECK_INTERVAL,
} from './chat.constants';
import { isHttpErrorResponse } from './helpers/chat-send.helpers';
import { ChatSendMessageResult } from './chat-send-result';
import { ChatSendGate } from './services/chat-send-gate';
import { AssistantTurn } from './assistant-turn';

@Injectable()
export class ChatSessionService implements OnDestroy {
  private readonly chatService = inject(ChatService);
  private readonly threadList = inject(ThreadListService);
  private readonly messageService = inject(MessageService);
  private readonly streamingService = inject(ChatStreamingService);
  private readonly draftService = inject(DraftMessageService);
  private readonly jobService = inject(JobService);
  private readonly sendGate = inject(ChatSendGate);

  private readonly _thread = signal<Chat | null>(null);
  private readonly _model = signal<Model | null>(null);
  private readonly _loading = signal(false);
  private readonly _error = signal<string | null>(null);

  private activeThreadId: string | null = null;
  private messagesTotalCount = 0;
  /**
   * Keyset token for the batch of messages immediately older than the oldest loaded one. Set from
   * each response's `next_cursor`; null when the oldest message in the thread is loaded. Older
   * loads (scroll-back and jump-to-bookmark) walk this cursor instead of doing page-offset math,
   * so the batch size can vary freely without gaps.
   */
  private olderCursor: string | null = null;
  /** Baseline for detecting checkpoints that land *after* the initial thread load. */
  private lastCheckpointAt = '';
  private checkpointBaselineInitialized = false;
  private autosaveTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly subscriptions = new Subscription();
  /**
   * The active thread's in-flight reply: job polling, pending draft, streaming, cancel and
   * resume-on-entry. New behaviour in those areas belongs in AssistantTurn, not here; this
   * service only decides when a turn starts and resets it on thread switches.
   */
  private readonly turn = new AssistantTurn({
    jobService: this.jobService,
    messageService: this.messageService,
    streamingService: this.streamingService,
    sendGate: this.sendGate,
    activeThreadId: () => this.activeThreadId,
    reportError: message => this._error.set(message),
  });
  /**
   * A tab-return sync that arrived while this tab's reply was still landing. Replayed once that
   * settles; otherwise a turn sent from another tab in the meantime would stay invisible until
   * the next focus event. Cleared on every thread switch.
   */
  private deferredSync: { threadId: string; nearBottom: boolean } | null = null;

  readonly thread: Signal<Chat | null> = this._thread.asReadonly();
  readonly messages: Signal<ChatMessage[]> = toSignal(this.messageService.messages$, { initialValue: [] });
  readonly streamingMessageId: Signal<string | null> = this.turn.streamingMessageId;
  readonly isStreaming = this.turn.isStreaming;
  readonly loading = this._loading.asReadonly();
  readonly error = this._error.asReadonly();
  readonly pendingAssistantDraftText = this.turn.pendingAssistantDraftText;
  readonly draft: WritableSignal<string> = signal('');
  readonly model: Signal<Model | null> = this._model.asReadonly();
  readonly personalityId = computed(() => this._thread()?.personality_id ?? null);
  /** True while the assistant is generating (core inference pending and/or UI streaming). */
  readonly isGenerating = this.turn.isGenerating;
  /** Keep composer disabled while generation is in progress. */
  readonly composerBusy = computed(() => this.isGenerating());
  /** True while a chat_message job is in flight (after send) but not yet finished. */
  readonly assistantJobPending = this.turn.jobPending;
  readonly hasMoreOlderMessages = signal(false);
  readonly loadingOlderMessages = signal(false);
  private readonly _contextCheckpointToken = signal(0);
  /**
   * Increments each time a post-turn checkpoint (scratchpad + summary) completes
   * for the active thread *after* its initial load. Views that cache thread context
   * (header summary, sidebar scratchpad) watch this to refresh in place.
   */
  readonly contextCheckpointToken = this._contextCheckpointToken.asReadonly();
  /**
   * Newest post-turn checkpoint timestamp across loaded messages. Bumps when a
   * background checkpoint (scratchpad + summary) completes, so views that cache
   * that context can refresh without a full reload. Empty when none present.
   */
  readonly latestCheckpointCompletedAt = computed(() => {
    let latest = '';
    for (const message of this.messages()) {
      const checkpointAt = message.checkpoint_completed_at?.trim();
      if (checkpointAt && checkpointAt > latest) {
        latest = checkpointAt;
      }
    }
    return latest;
  });

  constructor() {
    this.streamingService.configure({
      intervalMs: STREAMING_INTERVAL_MS,
      scrollCheckInterval: STREAMING_SCROLL_CHECK_INTERVAL,
      codeBlockChunkSize: CODE_BLOCK_CHUNK_SIZE,
    });

    this.subscriptions.add(
      this.messageService.messages$.subscribe(() => this.maybeBumpCheckpointToken()),
    );

    effect(() => {
      const chatId = this.activeThreadId;
      const draft = this.draft();
      if (!chatId) return;

      if (this.autosaveTimer) {
        clearTimeout(this.autosaveTimer);
      }
      this.autosaveTimer = setTimeout(() => {
        if (draft.trim()) {
          this.draftService.saveDraft(chatId, draft);
        } else {
          this.draftService.clearDraft(chatId);
        }
      }, AUTOSAVE_DEBOUNCE_MS);
    });

    effect(() => {
      if (this.turn.replyLanding()) return;
      untracked(() => this.flushDeferredSync());
    });
  }

  setActive(threadId: string): void {
    if (threadId === this.activeThreadId) return;

    const requestedThreadId = threadId;
    this.turn.reset();
    this.deferredSync = null;
    this.activeThreadId = threadId;
    this.messagesTotalCount = 0;
    this.olderCursor = null;
    this.lastCheckpointAt = '';
    this.checkpointBaselineInitialized = false;
    this.hasMoreOlderMessages.set(false);
    this.loadingOlderMessages.set(false);
    this._loading.set(true);
    this._error.set(null);
    // Clear the optimistic per-thread model selection so the picker reflects the
    // newly-loaded thread's own model_id instead of carrying the previous thread's pick.
    this._model.set(null);
    this.messageService.setCurrentChatId(threadId);
    this.messageService.clearMessages();

    const draft = this.draftService.getDraft(threadId);
    this.draft.set(draft?.message ?? '');

    this.subscriptions.add(
      this.chatService.getChat(threadId).subscribe({
        next: chat => {
          if (!this.isActiveThread(requestedThreadId)) return;
          this._thread.set(chat);
          this.chatService.setLastChatId(chat.id);
        },
        error: error => {
          if (!this.isActiveThread(requestedThreadId)) return;
          this._error.set(error?.message ?? 'Failed to load chat');
          this._thread.set(null);
        },
      }),
    );

    this.subscriptions.add(
      this.messageService.listMessages(threadId, 1, MESSAGE_LIST_PAGE_SIZE).subscribe({
        next: response => {
          if (!this.isActiveThread(requestedThreadId)) return;
          this.messagesTotalCount = response.total_count ?? response.results.length;
          this.olderCursor = response.next_cursor ?? null;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
          this._loading.set(false);
          // Adopt the loaded thread's newest checkpoint as the baseline so only
          // checkpoints that complete afterward trigger an in-place context refresh.
          this.lastCheckpointAt = this.latestCheckpointCompletedAt();
          this.checkpointBaselineInitialized = true;
          this.turn.resumeIfRunning(threadId);
          this.subscriptions.add(
            this.chatService.markChatRead(threadId).subscribe({
              next: () => {
                if (!this.isActiveThread(requestedThreadId)) return;
                this.messageService.markAssistantMessagesRead(threadId);
                this.threadList.clearUnreadForThread(threadId);
              },
              error: () => {
                // Best-effort: thread still works if mark-read fails.
              },
            }),
          );
        },
        error: error => {
          if (!this.isActiveThread(requestedThreadId)) return;
          this._loading.set(false);
          this._error.set(error?.message ?? 'Failed to load messages');
        },
      }),
    );
  }

  /**
   * Refresh the active thread after a tab-return/focus without destroying the user's scrollback.
   * Thread metadata (name/summary) always refreshes. Messages are reconciled non-destructively:
   * messages already loaded are updated in place and anything new is appended — older loaded
   * pages and the scroll position are preserved. A full reload only happens when more than a
   * page arrived while away (a gap) AND the user is following at the bottom; a user reading
   * history is never yanked back to the present.
   *
   * While this tab is generating, the sync is deferred rather than dropped: the job poller owns
   * the list until the reply lands, and the sync runs once it settles. Post-inference phases
   * (expression, summarization) do not defer it — they can run for minutes, and the composer is
   * already unlocked for them.
   */
  syncActiveThread(nearBottom: boolean): void {
    const threadId = this.activeThreadId;
    if (!threadId) return;
    if (this.turn.replyLanding()) {
      this.deferredSync = { threadId, nearBottom };
      return;
    }
    this.deferredSync = null;

    this.subscriptions.add(
      this.chatService.getChat(threadId).subscribe({
        next: chat => {
          if (!this.isActiveThread(threadId)) return;
          this._thread.set(chat);
        },
        error: () => {
          // Best-effort: keep showing current thread if the refetch fails.
        },
      }),
    );

    this.subscriptions.add(
      this.messageService.reconcileLatestPage(threadId, MESSAGE_LIST_PAGE_SIZE).subscribe({
        next: result => {
          if (!this.isActiveThread(threadId)) return;
          if (result.gap) {
            // More than a page arrived while away. Rebuild only if the user is following the
            // conversation; if they're reading history, leave their view exactly as it is.
            if (nearBottom) this.reloadFromLatest(threadId);
            return;
          }
          this.messagesTotalCount = result.total;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
          this.turn.resumeIfRunning(threadId);
          this.markActiveThreadRead(threadId);
        },
        error: () => {
          // Best-effort refresh: leave the current list intact on failure.
        },
      }),
    );
  }

  private flushDeferredSync(): void {
    const pending = this.deferredSync;
    if (!pending) return;
    this.deferredSync = null;
    if (this.isActiveThread(pending.threadId)) this.syncActiveThread(pending.nearBottom);
  }

  /** Destructive reload to the newest page (used only when a gap makes a merge unsafe). */
  private reloadFromLatest(threadId: string): void {
    this.subscriptions.add(
      this.messageService.listMessages(threadId, 1, MESSAGE_LIST_PAGE_SIZE).subscribe({
        next: response => {
          if (!this.isActiveThread(threadId)) return;
          this.messagesTotalCount = response.total_count ?? response.results.length;
          this.olderCursor = response.next_cursor ?? null;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
          this.turn.resumeIfRunning(threadId);
          this.markActiveThreadRead(threadId);
        },
        error: () => {
          // Best-effort: leave the current list intact on failure.
        },
      }),
    );
  }

  private markActiveThreadRead(threadId: string): void {
    this.subscriptions.add(
      this.chatService.markChatRead(threadId).subscribe({
        next: () => {
          if (!this.isActiveThread(threadId)) return;
          this.messageService.markAssistantMessagesRead(threadId);
          this.threadList.clearUnreadForThread(threadId);
        },
        error: () => {
          // Best-effort: thread still works if mark-read fails.
        },
      }),
    );
  }

  loadOlderMessages(): void {
    const threadId = this.activeThreadId;
    if (!threadId || this.loadingOlderMessages() || !this.hasMoreOlderMessages() || !this.olderCursor) {
      return;
    }
    this.loadingOlderMessages.set(true);
    this.messageService
      .listMessages(threadId, 1, MESSAGE_LIST_PAGE_SIZE, undefined, this.olderCursor)
      .pipe(finalize(() => this.loadingOlderMessages.set(false)))
      .subscribe({
        next: response => {
          if (!this.isActiveThread(threadId)) {
            return;
          }
          this.olderCursor = response.next_cursor ?? null;
          this.messagesTotalCount = response.total_count ?? this.messagesTotalCount;
          this.hasMoreOlderMessages.set(!!this.olderCursor && this.messages().length < this.messagesTotalCount);
        },
        error: () => {
          if (!this.isActiveThread(threadId)) {
            return;
          }
        },
      });
  }

  /**
   * Load successive older batches until `messageId` is present in the list, or there is nothing
   * older left / a batch cap is hit. Resolves to whether the message is now loaded. Powers jumping
   * to a bookmark that lives on an older, not-yet-loaded batch.
   *
   * Uses the larger jump batch and keyset cursor so a far-back target resolves in a handful of
   * roundtrips instead of dozens of small pages. The cap is on *batches*, not messages, so with a
   * 200-message batch it still reaches thousands of messages back.
   */
  loadOlderMessagesUntil(messageId: string, maxBatches = 40): Promise<boolean> {
    const threadId = this.activeThreadId;
    const isLoaded = () => this.messages().some(m => m.id === messageId);
    if (!threadId || isLoaded()) {
      return Promise.resolve(isLoaded());
    }
    return new Promise<boolean>(resolve => {
      let remaining = maxBatches;
      const step = (): void => {
        if (isLoaded()) {
          resolve(true);
          return;
        }
        if (!this.hasMoreOlderMessages() || !this.olderCursor || remaining-- <= 0) {
          resolve(isLoaded());
          return;
        }
        this.loadingOlderMessages.set(true);
        this.messageService
          .listMessages(threadId, 1, MESSAGE_JUMP_PAGE_SIZE, undefined, this.olderCursor)
          .pipe(finalize(() => this.loadingOlderMessages.set(false)))
          .subscribe({
            next: response => {
              if (!this.isActiveThread(threadId)) {
                resolve(false);
                return;
              }
              this.olderCursor = response.next_cursor ?? null;
              this.messagesTotalCount = response.total_count ?? this.messagesTotalCount;
              this.hasMoreOlderMessages.set(!!this.olderCursor && this.messages().length < this.messagesTotalCount);
              step();
            },
            error: () => resolve(isLoaded()),
          });
      };
      step();
    });
  }

  clearActive(): void {
    this.turn.reset();
    this.deferredSync = null;
    this.activeThreadId = null;
    this.messagesTotalCount = 0;
    this.olderCursor = null;
    this.hasMoreOlderMessages.set(false);
    this.loadingOlderMessages.set(false);
    this._thread.set(null);
    this._model.set(null);
    this.messageService.setCurrentChatId(null);
    this.messageService.clearMessages();
    this.draft.set('');
  }

  async createThread(opts: { personalityId?: string; modelId?: string } = {}): Promise<Chat> {
    const chat = await firstValueFrom(this.chatService.createChat({
      name: 'New Chat',
      personality_id: opts.personalityId,
      model_id: opts.modelId,
    }));
    this.chatService.setLastChatId(chat.id);
    return chat;
  }

  async sendMessage(
    text: string,
    attachments: readonly PendingFileAttachment[] = [],
    rituals: readonly Ritual[] = [],
  ): Promise<ChatSendMessageResult> {
    const chat = this._thread();
    const message = text.trim();
    // `_thread` still holds the previous thread until the new one's getChat resolves; never
    // let a send in that window post to the thread the user just navigated away from.
    if (!chat || !this.isActiveThread(chat.id) || !message || this.isGenerating()) {
      return { status: 'skipped' };
    }

    this.turn.beginSend();
    this.draft.set('');
    this.draftService.clearDraft(chat.id);

    const ritualPayload = rituals.length ? [...rituals] : undefined;

    let response;
    try {
      response = await firstValueFrom(this.messageService.sendMessage(chat.id, {
        message,
        origin: 'User',
        attachments: toUploadedAttachments(attachments),
        rituals: ritualPayload,
      }));
    } catch (error) {
      this.draftService.saveDraft(chat.id, message);
      if (!isHttpErrorResponse(error)) {
        console.warn('[chat.sendMessage] send failed with non-HTTP error', error);
      }
      if (!this.isActiveThread(chat.id)) {
        // The user switched threads while the POST was in flight; the unsent text is kept as
        // that thread's saved draft and must not surface in the now-active thread's composer.
        return { status: 'failed', error };
      }
      this.turn.sendFailed();
      this.draft.set(message);
      this._error.set(apiErrorMessage(error, 'Failed to send message'));
      return { status: 'failed', error };
    }

    if (!this.isActiveThread(chat.id)) {
      // Switched threads mid-POST: the turn belongs to the previous thread, whose job is picked
      // up by the turn's resume-on-entry when the user returns. Leave the active thread alone.
      return { status: 'sent' };
    }
    this._error.set(null);
    this.turn.sendAccepted(chat.id, response.id, response.job_id);
    return { status: 'sent' };
  }

  async retryUserMessage(message: ChatMessage): Promise<void> {
    const chat = this._thread();
    if (!chat || !this.isActiveThread(chat.id) || message.origin !== 'User' || this.isGenerating()) return;

    this.turn.beginRetry(message.id);
    this._error.set(null);
    try {
      const response = await firstValueFrom(this.messageService.retryUserMessage(chat.id, message.id));
      if (response.job_id) {
        this.turn.startPolling(response.job_id, chat.id);
      }
    } catch (error) {
      if (!this.isActiveThread(chat.id)) return;
      this.turn.retryFailed();
      this._error.set(apiErrorMessage(error, 'Retry failed'));
    }
  }

  startAssistantJobPolling(jobId: string, chatId: string): void {
    this.turn.startPolling(jobId, chatId);
  }

  cancelStreaming(): void {
    this.turn.cancelStreaming();
  }

  cancelGeneration(): void {
    this.turn.cancelGeneration();
  }

  setModel(model: Model): void {
    const chat = this._thread();
    if (!chat) {
      this._model.set(model);
      return;
    }
    const previousModel = this._model();
    const previousThread = { ...chat };
    const requestChatId = chat.id;
    const requestModelId = model.id;
    this._model.set(model);
    this._thread.set({
      ...chat,
      model_id: model.id,
      model_name: model.name,
    });
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, { model_id: model.id }).pipe(take(1)).subscribe({
        next: updated => {
          if (this._thread()?.id !== requestChatId || this._model()?.id !== requestModelId) {
            return;
          }
          this._thread.set({
            ...updated,
            model_id: updated.model_id ?? model.id,
            model_name: updated.model_name ?? model.name,
          });
          this._model.set(model);
        },
        error: (error: unknown) => {
          if (this._thread()?.id !== requestChatId || this._model()?.id !== requestModelId) {
            return;
          }
          this._thread.set(previousThread);
          this._model.set(previousModel);
          this._error.set('Failed to update model. Please try again.');
        },
      }),
    );
  }

  setPersonality(id: string | null): void {
    const chat = this._thread();
    if (!chat || !this.isActiveThread(chat.id)) return;
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, { personality_id: id ?? undefined }).subscribe({
        next: updated => {
          if (!this.isActiveThread(chat.id)) return;
          this._thread.set(updated);
        },
        error: err => {
          if (!this.isActiveThread(chat.id)) return;
          this._error.set(apiErrorMessage(err, 'Failed to update personality'));
        },
      }),
    );
  }

  /** Pins a generation mode on the thread, or clears to Auto when moodId is null. */
  setActiveMood(moodId: string | null): void {
    const chat = this._thread();
    if (!chat || !this.isActiveThread(chat.id)) return;
    const patch =
      moodId === null
        ? { clear_active_mood: true }
        : { active_mood_id: moodId, is_auto_mood: false };
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, patch).subscribe({
        next: updated => {
          if (!this.isActiveThread(chat.id)) return;
          this._error.set(null);
          this._thread.set(updated);
        },
        error: err => {
          if (!this.isActiveThread(chat.id)) return;
          this._error.set(apiErrorMessage(err, 'Failed to update mode'));
        },
      }),
    );
  }

  setThreadName(name: string): void {
    const chat = this._thread();
    const trimmedName = name.trim();
    if (!chat || !this.isActiveThread(chat.id) || !trimmedName || trimmedName === chat.name) return;
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, { name: trimmedName }).subscribe({
        next: updated => {
          if (!this.isActiveThread(chat.id)) return;
          this._thread.set(updated);
        },
      }),
    );
  }

  getDisplayMessage(message: ChatMessage): string {
    return this.streamingService.getDisplayMessage(message);
  }

  getDisplayRevision(): number {
    return this.streamingService.getDisplayRevision();
  }

  ngOnDestroy(): void {
    if (this.autosaveTimer) {
      clearTimeout(this.autosaveTimer);
    }
    this.turn.dispose();
    this.subscriptions.unsubscribe();
    this.streamingService.destroy();
  }

  /**
   * Bumps the checkpoint token when a newer post-turn checkpoint appears after the
   * thread's initial load — i.e. a background scratchpad/summary update just landed.
   */
  private maybeBumpCheckpointToken(): void {
    if (!this.checkpointBaselineInitialized) return;
    const latest = this.latestCheckpointCompletedAt();
    if (latest && latest > this.lastCheckpointAt) {
      this.lastCheckpointAt = latest;
      this._contextCheckpointToken.update(n => n + 1);
    }
  }

  private isActiveThread(threadId: string): boolean {
    return this.activeThreadId === threadId;
  }
}

function toUploadedAttachments(attachments: readonly PendingFileAttachment[]): FileAttachment[] | undefined {
  const uploaded = attachments
    .map(item => item.attachment)
    .filter((attachment): attachment is FileAttachment => attachment !== undefined);
  return uploaded.length ? uploaded : undefined;
}

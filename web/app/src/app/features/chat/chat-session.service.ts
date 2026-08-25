import { Injectable, OnDestroy, Signal, WritableSignal, computed, effect, inject, signal } from '@angular/core';
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
import { Job } from '../../core/models/job.model';
import { Model } from '../../core/models/model.model';
import { Ritual } from '../../core/models/ritual.model';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import {
  AUTOSAVE_DEBOUNCE_MS,
  CHAT_PENDING_ASSISTANT_MESSAGE_ID,
  CODE_BLOCK_CHUNK_SIZE,
  MESSAGE_LIST_PAGE_SIZE,
  STREAMING_INTERVAL_MS,
  STREAMING_SCROLL_CHECK_INTERVAL,
} from './chat.constants';
import { isHttpErrorResponse } from './helpers/chat-send.helpers';
import { lastUserTurnWithGenerationError } from './helpers/message-grouping.helpers';
import { ChatSendMessageResult } from './chat-send-result';
import { ChatSendGate } from './services/chat-send-gate';

@Injectable()
export class ChatSessionService implements OnDestroy {
  private static readonly PENDING_SEND_JOB_ID = 'pending-send';
  private readonly chatService = inject(ChatService);
  private readonly threadList = inject(ThreadListService);
  private readonly messageService = inject(MessageService);
  private readonly streamingService = inject(ChatStreamingService);
  private readonly draftService = inject(DraftMessageService);
  private readonly jobService = inject(JobService);
  private readonly sendGate = inject(ChatSendGate);

  private readonly _thread = signal<Chat | null>(null);
  private readonly _model = signal<Model | null>(null);
  private readonly _streamingMessageId = signal<string | null>(null);
  private readonly _loading = signal(false);
  private readonly _error = signal<string | null>(null);
  private readonly _pendingAssistantDraftText = signal('');

  private activeThreadId: string | null = null;
  private messagesPage = 1;
  private messagesTotalCount = 0;
  /** Baseline for detecting checkpoints that land *after* the initial thread load. */
  private lastCheckpointAt = '';
  private checkpointBaselineInitialized = false;
  private expectingAssistantResponse = false;
  private expectedAssistantAfterUserMessageId: string | null = null;
  private readonly jobRenderedDeltaIndex = new Map<string, number>();
  private autosaveTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly subscriptions = new Subscription();

  readonly thread: Signal<Chat | null> = this._thread.asReadonly();
  readonly messages: Signal<ChatMessage[]> = toSignal(this.messageService.messages$, { initialValue: [] });
  readonly streamingMessageId: Signal<string | null> = this._streamingMessageId.asReadonly();
  readonly isStreaming = computed(() => this._streamingMessageId() !== null);
  readonly loading = this._loading.asReadonly();
  readonly error = this._error.asReadonly();
  readonly pendingAssistantDraftText = this._pendingAssistantDraftText.asReadonly();
  readonly draft: WritableSignal<string> = signal('');
  readonly model: Signal<Model | null> = this._model.asReadonly();
  readonly personalityId = computed(() => this._thread()?.personality_id ?? null);
  /** Set while an async chat job is being polled (typing placeholder; send is still allowed). */
  private readonly _activeChatJobId = signal<string | null>(null);
  /** Tracks explicit user-initiated cancel intent for an in-flight job id. */
  private readonly _cancelRequestedJobId = signal<string | null>(null);
  /** Latest observed status of the in-flight chat job (null until the first poll snapshot). */
  private readonly _activeJobPhase = signal<Job['status'] | null>(null);
  /**
   * True only while *core inference* is still running. Once the job reaches
   * inference_complete, the assistant reply is fully available and the post-inference
   * phases (expression pick, conversation summarization) continue in the background —
   * they must not keep the stop button up or the composer locked.
   */
  private readonly inferenceGenerating = computed(
    () => this.assistantJobPending() && !isPostInferencePhase(this._activeJobPhase()),
  );
  /** True while the assistant is generating (core inference pending and/or UI streaming). */
  readonly isGenerating = computed(() => (this.inferenceGenerating() && !this.isCancellationPending()) || this.isStreaming());
  /** Keep composer disabled while generation is in progress. */
  readonly composerBusy = computed(() => this.isGenerating());
  /** True while a chat_message job is in flight (after send) but not yet finished. */
  readonly assistantJobPending = computed(() => this._activeChatJobId() !== null);
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
    this.streamingService.setCompletionCallback(messageId => {
      if (this._streamingMessageId() === messageId) {
        this._streamingMessageId.set(null);
      }
    });

    this.subscriptions.add(
      this.messageService.messages$.subscribe(messages => this.maybeStreamLatestAssistant(messages)),
    );

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
  }

  setActive(threadId: string): void {
    if (threadId === this.activeThreadId) return;

    const requestedThreadId = threadId;
    this.activeThreadId = threadId;
    this.messagesPage = 1;
    this.messagesTotalCount = 0;
    this.lastCheckpointAt = '';
    this.checkpointBaselineInitialized = false;
    this.hasMoreOlderMessages.set(false);
    this.loadingOlderMessages.set(false);
    this._loading.set(true);
    this._error.set(null);
    this._pendingAssistantDraftText.set('');
    this.streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    this.expectedAssistantAfterUserMessageId = null;
    this.jobRenderedDeltaIndex.clear();
    this._activeChatJobId.set(null);
    this._activeJobPhase.set(null);
    this._cancelRequestedJobId.set(null);
    this.expectingAssistantResponse = false;
    this._streamingMessageId.set(null);
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
          this.messagesPage = 1;
          this.messagesTotalCount = response.total_count ?? response.results.length;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
          this._loading.set(false);
          // Adopt the loaded thread's newest checkpoint as the baseline so only
          // checkpoints that complete afterward trigger an in-place context refresh.
          this.lastCheckpointAt = this.latestCheckpointCompletedAt();
          this.checkpointBaselineInitialized = true;
          this.resumePendingJobIfNeeded(threadId);
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
   * Re-pull the active thread's latest messages + metadata in place — no list
   * clear, no loading flash. Used when the tab regains focus/visibility so
   * messages produced in another tab (or by a background job) appear without a
   * manual page refresh. The message-list replace flows through the checkpoint
   * subscription, so a background turn's new summary/scratchpad refreshes too.
   *
   * No-op while this tab has its own in-flight job or streaming: the job poller
   * already keeps it current, and we must not disturb that state.
   */
  reloadActiveThread(): void {
    const threadId = this.activeThreadId;
    if (!threadId || this.assistantJobPending() || this.isStreaming()) {
      return;
    }

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
      this.messageService.listMessages(threadId, 1, MESSAGE_LIST_PAGE_SIZE).subscribe({
        next: response => {
          if (!this.isActiveThread(threadId)) return;
          this.messagesPage = 1;
          this.messagesTotalCount = response.total_count ?? response.results.length;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
          this.resumePendingJobIfNeeded(threadId);
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
        },
        error: () => {
          // Best-effort refresh: leave the current list intact on failure.
        },
      }),
    );
  }

  loadOlderMessages(): void {
    const threadId = this.activeThreadId;
    if (!threadId || this.loadingOlderMessages() || !this.hasMoreOlderMessages()) {
      return;
    }
    const nextPage = this.messagesPage + 1;
    this.loadingOlderMessages.set(true);
    this.messageService
      .listMessages(threadId, nextPage, MESSAGE_LIST_PAGE_SIZE)
      .pipe(finalize(() => this.loadingOlderMessages.set(false)))
      .subscribe({
        next: response => {
          if (!this.isActiveThread(threadId)) {
            return;
          }
          this.messagesPage = nextPage;
          this.messagesTotalCount = response.total_count ?? this.messagesTotalCount;
          this.hasMoreOlderMessages.set(this.messages().length < this.messagesTotalCount);
        },
        error: () => {
          if (!this.isActiveThread(threadId)) {
            return;
          }
        },
      });
  }

  clearActive(): void {
    this.activeThreadId = null;
    this.messagesPage = 1;
    this.messagesTotalCount = 0;
    this.hasMoreOlderMessages.set(false);
    this.loadingOlderMessages.set(false);
    this._thread.set(null);
    this._model.set(null);
    this._streamingMessageId.set(null);
    this._activeChatJobId.set(null);
    this._activeJobPhase.set(null);
    this._cancelRequestedJobId.set(null);
    this._pendingAssistantDraftText.set('');
    this.streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    this.expectedAssistantAfterUserMessageId = null;
    this.jobRenderedDeltaIndex.clear();
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
    if (!chat || !message || this.isGenerating()) {
      return { status: 'skipped' };
    }

    this.expectingAssistantResponse = true;
    this._cancelRequestedJobId.set(null);
    this._activeChatJobId.set(ChatSessionService.PENDING_SEND_JOB_ID);
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
      this.expectingAssistantResponse = false;
      this._activeChatJobId.set(null);
      this.expectedAssistantAfterUserMessageId = null;
      this.draft.set(message);
      this._error.set(apiErrorMessage(error, 'Failed to send message'));
      this.draftService.saveDraft(chat.id, message);
      if (!isHttpErrorResponse(error)) {
        console.warn('[chat.sendMessage] send failed with non-HTTP error', error);
      }
      return { status: 'failed', error };
    }

    this._error.set(null);
    this.expectedAssistantAfterUserMessageId = response.id;
    if (response.job_id) {
      this.startAssistantJobPolling(response.job_id, chat.id);
      if (this._cancelRequestedJobId() === ChatSessionService.PENDING_SEND_JOB_ID) {
        this.requestCancelForJob(response.job_id);
      }
    } else {
      this._activeChatJobId.set(null);
      this._cancelRequestedJobId.set(null);
    }
    return { status: 'sent' };
  }

  async retryUserMessage(message: ChatMessage): Promise<void> {
    const chat = this._thread();
    if (!chat || message.origin !== 'User' || this.isGenerating()) return;

    this.expectingAssistantResponse = true;
    this._error.set(null);
    this.expectedAssistantAfterUserMessageId = message.id;
    try {
      const response = await firstValueFrom(this.messageService.retryUserMessage(chat.id, message.id));
      if (response.job_id) {
        this.startAssistantJobPolling(response.job_id, chat.id);
      }
    } catch (error) {
      this.expectingAssistantResponse = false;
      this._error.set(apiErrorMessage(error, 'Retry failed'));
    }
  }

  startAssistantJobPolling(jobId: string, chatId: string): void {
    if (!jobId || !this.isActiveThread(chatId) || this.jobService.isJobBeingPolled(jobId)) {
      return;
    }
    this.expectingAssistantResponse = true;
    this._activeChatJobId.set(jobId);
    this._activeJobPhase.set(null);
    if (this._cancelRequestedJobId() !== jobId) {
      this._pendingAssistantDraftText.set('');
      this.streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
    this.jobRenderedDeltaIndex.set(jobId, 0);
    this.subscriptions.add(
      this.jobService
        .pollJob(jobId, chatId)
        .pipe(
          finalize(() => {
            this.finishPendingDraftStream(this._cancelRequestedJobId() !== jobId);
            this.jobRenderedDeltaIndex.delete(jobId);
          }),
          finalize(() => {
            if (this._activeChatJobId() === jobId) {
              this._activeChatJobId.set(null);
              this._activeJobPhase.set(null);
            }
            if (this._cancelRequestedJobId() === jobId) {
              this._cancelRequestedJobId.set(null);
            }
            this.expectingAssistantResponse = false;
            this.expectedAssistantAfterUserMessageId = null;
            this.sendGate.refresh();
          }),
        )
        .subscribe({
          next: job => this.handleJobProgressSnapshot(job),
          error: err => {
            this._error.set(apiErrorMessage(err, 'Failed to process message'));
          },
        }),
    );
  }

  cancelStreaming(): void {
    this.cancelStreamingWithOptions({ clearPendingDraft: true });
  }

  cancelGeneration(): void {
    const activeJobId = this._activeChatJobId();
    if (!activeJobId) {
      // Rare but possible during UI transitions: streaming may still be active
      // after job bookkeeping has already been cleared.
      this.cancelStreaming();
      return;
    }
    this._cancelRequestedJobId.set(activeJobId);
    if (activeJobId !== ChatSessionService.PENDING_SEND_JOB_ID) {
      this.requestCancelForJob(activeJobId);
    }
    // Stop local typing animation immediately, but preserve visible partial draft
    // until terminal cancel reconciliation arrives from polling.
    this.cancelStreamingWithOptions({ clearPendingDraft: false });
  }

  private cancelStreamingWithOptions(opts: { clearPendingDraft: boolean }): void {
    const messageId = this._streamingMessageId();
    if (messageId) {
      const fullText = messageId === CHAT_PENDING_ASSISTANT_MESSAGE_ID ? this._pendingAssistantDraftText() : undefined;
      this.streamingService.stopStreaming(messageId, fullText, false);
      this._streamingMessageId.set(null);
    }
    if (opts.clearPendingDraft && messageId === CHAT_PENDING_ASSISTANT_MESSAGE_ID) {
      this._pendingAssistantDraftText.set('');
    }
    this.expectingAssistantResponse = false;
    this.expectedAssistantAfterUserMessageId = null;
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
    if (!chat) return;
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, { personality_id: id ?? undefined }).subscribe({
        next: updated => this._thread.set(updated),
        error: err => this._error.set(apiErrorMessage(err, 'Failed to update personality')),
      }),
    );
  }

  /** Pins a generation mode on the thread, or clears to Auto when moodId is null. */
  setActiveMood(moodId: string | null): void {
    const chat = this._thread();
    if (!chat) return;
    const patch =
      moodId === null
        ? { clear_active_mood: true }
        : { active_mood_id: moodId, is_auto_mood: false };
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, patch).subscribe({
        next: updated => {
          this._error.set(null);
          this._thread.set(updated);
        },
        error: err => this._error.set(apiErrorMessage(err, 'Failed to update mode')),
      }),
    );
  }

  setThreadName(name: string): void {
    const chat = this._thread();
    const trimmedName = name.trim();
    if (!chat || !trimmedName || trimmedName === chat.name) return;
    this.subscriptions.add(
      this.chatService.patchChat(chat.id, { name: trimmedName }).subscribe({
        next: updated => this._thread.set(updated),
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
    this.subscriptions.unsubscribe();
    this.streamingService.destroy();
  }

  private maybeStreamLatestAssistant(messages: readonly ChatMessage[]): void {
    const latest = messages[messages.length - 1];
    if (!latest || latest.origin !== 'Assistant' || !latest.message) return;
    if (this._pendingAssistantDraftText()) {
      this.finishPendingDraftStream();
      return;
    }
    if (!this.expectingAssistantResponse) return;
    if (!this.isAssistantForExpectedUserTurn(messages, latest.id)) return;
    this.expectingAssistantResponse = false;
    this.expectedAssistantAfterUserMessageId = null;
    this._streamingMessageId.set(latest.id);
    this.streamingService.startStreaming(latest);
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

  private resumePendingJobIfNeeded(threadId: string): void {
    if (!this.isActiveThread(threadId)) return;
    // Snapshot from the same stream the UI uses (avoids relying on getMessages(),
    // which may be absent on UX-V2's slimmer MessageService after merges).
    this.subscriptions.add(
      this.messageService.messages$.pipe(take(1)).subscribe(messages => {
        if (!this.isActiveThread(threadId)) return;
        const lastUser = this.latestUserTurn(messages);
        if (!lastUser) return;
        if (this.hasAssistantReplyForUserTurn(messages, lastUser)) return;
        if (lastUserTurnWithGenerationError(messages) !== null) {
          return;
        }

        this.subscriptions.add(
          this.jobService.getActiveChatMessageJob(threadId, lastUser.id).subscribe({
            next: active => {
              if (!this.isActiveThread(threadId) || !active?.job_id) return;
              if (isTerminalJobStatus(active.status)) return;
              this.startAssistantJobPolling(active.job_id, threadId);
            },
          }),
        );
      }),
    );
  }

  private handleJobProgressSnapshot(job: Job): void {
    if (!job) return;
    // Only the active job drives the generating/phase state. A prior job may still
    // be polling its post-inference phases (expression/summarization) after the
    // composer unlocked, and its late snapshots must not overwrite the new job's phase.
    if (this._activeChatJobId() === job.id) {
      this._activeJobPhase.set(job.status);
    }
    const cancelPendingForJob = this._cancelRequestedJobId() === job.id;
    if (cancelPendingForJob && job.status === 'cancelled') {
      this._cancelRequestedJobId.set(null);
    }
    const draftDeltas = job.draft_deltas ?? [];
    if (draftDeltas.length === 0) return;
    const jobId = job.id;
    const renderedIdx = this.jobRenderedDeltaIndex.get(jobId) ?? 0;
    if (renderedIdx >= draftDeltas.length) return;
    if (cancelPendingForJob) {
      // Cancellation requested: advance cursor but do not keep animating more draft chunks.
      this.jobRenderedDeltaIndex.set(jobId, draftDeltas.length);
      return;
    }
    const newChunks = draftDeltas.slice(renderedIdx);
    this.jobRenderedDeltaIndex.set(jobId, draftDeltas.length);
    const nextDraftText = `${this._pendingAssistantDraftText()}${newChunks.join('')}`;
    this._pendingAssistantDraftText.set(nextDraftText);
    this.expectingAssistantResponse = false;
    this._streamingMessageId.set(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    this.streamingService.appendServerChunks(CHAT_PENDING_ASSISTANT_MESSAGE_ID, newChunks);
  }

  private finishPendingDraftStream(clearPendingDraft: boolean = true): void {
    const pendingText = this._pendingAssistantDraftText();
    if (pendingText) {
      this.streamingService.completeStreaming(CHAT_PENDING_ASSISTANT_MESSAGE_ID, pendingText, true);
    } else {
      this.streamingService.stopStreaming(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
    if (this._streamingMessageId() === CHAT_PENDING_ASSISTANT_MESSAGE_ID) {
      this._streamingMessageId.set(null);
    }
    if (clearPendingDraft) {
      this._pendingAssistantDraftText.set('');
      this.streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
  }

  private requestCancelForJob(jobId: string): void {
    this.subscriptions.add(
      this.jobService.cancelJob(jobId).subscribe({
        error: err => {
          if (this._cancelRequestedJobId() === jobId) {
            this._cancelRequestedJobId.set(null);
          }
          this._error.set(apiErrorMessage(err, 'Failed to stop response'));
        },
      }),
    );
  }

  private isCancellationPending(): boolean {
    const activeJobId = this._activeChatJobId();
    const cancelJobId = this._cancelRequestedJobId();
    return !!activeJobId && !!cancelJobId && activeJobId === cancelJobId;
  }

  private isAssistantForExpectedUserTurn(messages: readonly ChatMessage[], assistantMessageId: string): boolean {
    const expectedUserId = this.expectedAssistantAfterUserMessageId;
    if (!expectedUserId) {
      return true;
    }
    const userIdx = messages.findIndex(message => message.id === expectedUserId);
    if (userIdx < 0) {
      return false;
    }
    const assistantIdx = messages.findIndex(message => message.id === assistantMessageId);
    return assistantIdx > userIdx;
  }

  private hasAssistantReplyForUserTurn(messages: readonly ChatMessage[], userMessage: ChatMessage): boolean {
    if (userMessage.origin !== 'User') return false;
    if (userMessage.response_id && messages.some(message => message.origin === 'Assistant' && message.id === userMessage.response_id)) {
      return true;
    }
    if (messages.some(message => message.origin === 'Assistant' && message.response_id === userMessage.id)) {
      return true;
    }
    const userSentAtMs = Date.parse(userMessage.sent_at);
    if (Number.isNaN(userSentAtMs)) {
      return false;
    }
    return messages.some(message => {
      if (message.origin !== 'Assistant') return false;
      const assistantSentAtMs = Date.parse(message.sent_at);
      return !Number.isNaN(assistantSentAtMs) && assistantSentAtMs >= userSentAtMs;
    });
  }

  private latestUserTurn(messages: readonly ChatMessage[]): ChatMessage | null {
    let best: ChatMessage | null = null;
    let bestSentAtMs = Number.NEGATIVE_INFINITY;
    for (const message of messages) {
      if (message.origin !== 'User') continue;
      const sentAtMs = Date.parse(message.sent_at);
      if (Number.isNaN(sentAtMs)) {
        if (!best) best = message;
        continue;
      }
      if (sentAtMs >= bestSentAtMs) {
        best = message;
        bestSentAtMs = sentAtMs;
      }
    }
    return best;
  }
}

function toUploadedAttachments(attachments: readonly PendingFileAttachment[]): FileAttachment[] | undefined {
  const uploaded = attachments
    .map(item => item.attachment)
    .filter((attachment): attachment is FileAttachment => attachment !== undefined);
  return uploaded.length ? uploaded : undefined;
}

function isTerminalJobStatus(status: Job['status']): boolean {
  return status === 'complete' || status === 'cancelled' || status === 'failed';
}

/**
 * Reports whether the job has progressed past core inference. At and beyond
 * inference_complete the assistant text is finalized; remaining phases (expression
 * classification, conversation summarization) are background post-processing.
 */
function isPostInferencePhase(status: Job['status'] | null): boolean {
  return (
    status === 'inference_complete' ||
    status === 'expression_complete' ||
    status === 'compaction_complete' ||
    status === 'complete' ||
    status === 'cancelled' ||
    status === 'failed'
  );
}

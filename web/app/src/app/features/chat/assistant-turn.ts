import { Signal, computed, signal } from '@angular/core';
import { Subscription, finalize } from 'rxjs';
import { take } from 'rxjs/operators';

import { ChatStreamingService } from '../../core/services/chat-streaming.service';
import { JobService } from '../../core/services/job.service';
import { MessageService } from '../../core/services/message.service';
import { ChatMessage } from '../../core/models/message.model';
import { ChatTurnProgress, ChatTurnToolCall, Job } from '../../core/models/job.model';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import { CHAT_JOB_POLL_INTERVAL_MS, CHAT_PENDING_ASSISTANT_MESSAGE_ID } from './chat.constants';
import { ChatSendGate } from './services/chat-send-gate';

export interface AssistantTurnDeps {
  jobService: JobService;
  messageService: MessageService;
  streamingService: ChatStreamingService;
  sendGate: ChatSendGate;
  /** The thread the session is showing; every write is scoped to the turn's own thread. */
  activeThreadId: () => string | null;
  reportError: (message: string) => void;
}

/**
 * The active thread's in-flight assistant turn: the chat job being polled, its phase and cancel
 * intent, the pending-assistant draft streamed from its deltas, and the local typing animation.
 *
 * Owned by ChatSessionService, which decides *when* a turn starts (send, retry, resume on thread
 * entry) and resets it on every thread switch. The pending-assistant slot, streaming id and
 * expectation flags here are shared across threads, so every async write is guarded by the
 * thread the job belongs to (#144).
 */
export class AssistantTurn {
  /** Stand-in job id between a send's POST and its job id coming back. */
  private static readonly PENDING_SEND_JOB_ID = 'pending-send';

  /** Set while an async chat job is being polled (typing placeholder; send is still allowed). */
  private readonly _activeChatJobId = signal<string | null>(null);
  /** Tracks explicit user-initiated cancel intent for an in-flight job id. */
  private readonly _cancelRequestedJobId = signal<string | null>(null);
  /** Latest observed status of the in-flight chat job (null until the first poll snapshot). */
  private readonly _activeJobPhase = signal<Job['status'] | null>(null);
  private readonly _streamingMessageId = signal<string | null>(null);
  private readonly _pendingAssistantDraftText = signal('');
  private readonly _liveToolCalls = signal<readonly ChatTurnToolCall[]>([]);
  /** Live model reasoning for the pending reply; replaced wholesale on each job snapshot. */
  private readonly _pendingAssistantDraftReasoning = signal('');

  private expectingAssistantResponse = false;
  private expectedAssistantAfterUserMessageId: string | null = null;
  private readonly jobRenderedDeltaIndex = new Map<string, number>();
  /**
   * Resume lookups, user-turn backfills and cancel requests; lives as long as the owning
   * session. These are one-shot HTTP calls: RxJS removes a child from this container when it
   * completes, so repeated resumes don't accumulate. Only requests still in flight are held.
   */
  private readonly subscriptions = new Subscription();
  /**
   * Job polls owned by the active thread. Torn down on every thread switch: a slow turn for
   * the previous thread (e.g. an imported thread stalled behind the rehydration gate) must not
   * keep writing its draft deltas / completion into the single pending-assistant slot, which
   * the newly active thread renders as its own reply (#144). Returning to the thread resumes
   * polling via resumeIfRunning.
   */
  private jobSubscriptions = new Subscription();

  readonly streamingMessageId: Signal<string | null> = this._streamingMessageId.asReadonly();
  readonly isStreaming = computed(() => this._streamingMessageId() !== null);
  readonly pendingAssistantDraftText = this._pendingAssistantDraftText.asReadonly();
  /** The active job's tool calls so far (live timeline from Job.progress); empty when idle. */
  readonly liveToolCalls = this._liveToolCalls.asReadonly();
  readonly pendingAssistantDraftReasoning = this._pendingAssistantDraftReasoning.asReadonly();
  /** True while a chat_message job is in flight (after send) but not yet finished. */
  readonly jobPending = computed(() => this._activeChatJobId() !== null);
  /**
   * True only while *core inference* is still running. Once the job reaches
   * inference_complete, the assistant reply is fully available and the post-inference
   * phases (expression pick, conversation summarization) continue in the background —
   * they must not keep the stop button up or the composer locked.
   */
  private readonly inferenceGenerating = computed(
    () => this.jobPending() && !isPostInferencePhase(this._activeJobPhase()),
  );
  private readonly cancellationPending = computed(() => {
    const activeJobId = this._activeChatJobId();
    return !!activeJobId && activeJobId === this._cancelRequestedJobId();
  });
  /** True while the assistant is generating (core inference pending and/or UI streaming). */
  readonly isGenerating = computed(() => (this.inferenceGenerating() && !this.cancellationPending()) || this.isStreaming());
  /**
   * True while this turn's reply is still landing: core inference running (including while a
   * cancel winds down) or the reply animating in. Post-inference phases don't count. The session
   * holds tab-return syncs and resume lookups while this is true; it is not a generic busy flag.
   */
  readonly replyLanding = computed(() => this.inferenceGenerating() || this.isStreaming());

  constructor(private readonly deps: AssistantTurnDeps) {
    deps.streamingService.setCompletionCallback(messageId => {
      if (this._streamingMessageId() === messageId) {
        this._streamingMessageId.set(null);
      }
    });
    this.subscriptions.add(deps.messageService.messages$.subscribe(messages => this.maybeStreamLatestAssistant(messages)));
  }

  /** Forget the turn on a thread switch. Its server-side job keeps running. */
  reset(): void {
    this.stopPolling();
    this._pendingAssistantDraftText.set('');
    this._pendingAssistantDraftReasoning.set('');
    this.deps.streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    this.expectedAssistantAfterUserMessageId = null;
    this.expectingAssistantResponse = false;
    this.jobRenderedDeltaIndex.clear();
    this._activeChatJobId.set(null);
    this._activeJobPhase.set(null);
    this._cancelRequestedJobId.set(null);
    this._streamingMessageId.set(null);
    this._liveToolCalls.set([]);
  }

  dispose(): void {
    this.jobSubscriptions.unsubscribe();
    this.subscriptions.unsubscribe();
  }

  /** A send's POST is in flight: show the typing placeholder before the job id is known. */
  beginSend(): void {
    this.expectingAssistantResponse = true;
    this._cancelRequestedJobId.set(null);
    this._activeChatJobId.set(AssistantTurn.PENDING_SEND_JOB_ID);
    // The placeholder shows from here; it must not carry the previous turn's tool rows.
    this._liveToolCalls.set([]);
  }

  sendFailed(): void {
    this.expectingAssistantResponse = false;
    this._activeChatJobId.set(null);
    this.expectedAssistantAfterUserMessageId = null;
  }

  /** The POST landed on the active thread: follow its job, honouring a stop pressed mid-POST. */
  sendAccepted(chatId: string, userMessageId: string, jobId: string | undefined): void {
    this.expectedAssistantAfterUserMessageId = userMessageId;
    if (jobId) {
      this.startPolling(jobId, chatId);
      if (this._cancelRequestedJobId() === AssistantTurn.PENDING_SEND_JOB_ID) {
        this.requestCancelForJob(jobId);
      }
    } else {
      this._activeChatJobId.set(null);
      this._cancelRequestedJobId.set(null);
    }
  }

  beginRetry(userMessageId: string): void {
    this._liveToolCalls.set([]);
    this.expectingAssistantResponse = true;
    this.expectedAssistantAfterUserMessageId = userMessageId;
  }

  retryFailed(): void {
    this.expectingAssistantResponse = false;
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
    if (activeJobId !== AssistantTurn.PENDING_SEND_JOB_ID) {
      this.requestCancelForJob(activeJobId);
    }
    // Stop local typing animation immediately, but preserve visible partial draft
    // until terminal cancel reconciliation arrives from polling.
    this.cancelStreamingWithOptions({ clearPendingDraft: false });
  }

  startPolling(jobId: string, chatId: string): void {
    const { jobService, streamingService, sendGate } = this.deps;
    if (!jobId || !this.isActiveThread(chatId) || jobService.isJobBeingPolled(jobId)) {
      return;
    }
    this.expectingAssistantResponse = true;
    this._activeChatJobId.set(jobId);
    this._activeJobPhase.set(null);
    this._liveToolCalls.set([]);
    if (this._cancelRequestedJobId() !== jobId) {
      this._pendingAssistantDraftText.set('');
      this._pendingAssistantDraftReasoning.set('');
      streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
    this.jobRenderedDeltaIndex.set(jobId, 0);
    // Every write below is scoped to the thread this job belongs to: the pending-assistant
    // slot, streaming id and expectation flags are shared across threads.
    this.jobSubscriptions.add(
      jobService
        .pollJob(jobId, chatId, CHAT_JOB_POLL_INTERVAL_MS)
        .pipe(
          finalize(() => {
            this.jobRenderedDeltaIndex.delete(jobId);
            if (!this.isActiveThread(chatId)) return;
            // A job superseded while in its post-inference phases (a newer turn was sent or
            // resumed) no longer owns the pending-assistant slot; leave the newer draft alone.
            if (this._activeChatJobId() !== jobId) return;
            this.finishPendingDraftStream(this._cancelRequestedJobId() !== jobId);
          }),
          finalize(() => {
            sendGate.refresh();
            if (!this.isActiveThread(chatId)) return;
            if (this._activeChatJobId() === jobId) {
              this._liveToolCalls.set([]);
              this._activeChatJobId.set(null);
              this._activeJobPhase.set(null);
            }
            if (this._cancelRequestedJobId() === jobId) {
              this._cancelRequestedJobId.set(null);
            }
            this.expectingAssistantResponse = false;
            this.expectedAssistantAfterUserMessageId = null;
          }),
        )
        .subscribe({
          next: job => {
            if (!this.isActiveThread(chatId)) return;
            this.handleJobProgressSnapshot(job);
          },
          error: err => {
            if (!this.isActiveThread(chatId)) return;
            this.deps.reportError(apiErrorMessage(err, 'Failed to process message'));
          },
        }),
    );
  }

  /**
   * Picks a running turn back up when the user (re)enters a thread: switching back from another
   * thread, a refresh, or a tab-focus sync. Job polls are scoped to the active thread (#144), so
   * this is the only way a turn that kept running while the user was elsewhere gets its
   * typing placeholder and streaming back. The server is asked for the thread's active job
   * directly rather than inferring an "unanswered" user turn from the loaded page.
   */
  resumeIfRunning(threadId: string): void {
    // A job still in its post-inference phases doesn't block picking up a newer turn (e.g. one
    // sent from another tab); startPolling skips a job that is already being polled.
    if (!this.isActiveThread(threadId) || this.replyLanding()) return;
    this.subscriptions.add(
      this.deps.jobService.getActiveChatJob(threadId).subscribe({
        next: active => {
          if (!this.isActiveThread(threadId) || !active?.job_id) return;
          if (isTerminalJobStatus(active.status)) return;
          if (active.message_id) this.ensureUserTurnLoaded(threadId, active.message_id);
          this.startPolling(active.job_id, threadId);
        },
        error: () => {
          // Best-effort: the thread still renders; a later sync can resume.
        },
      }),
    );
  }

  /**
   * The running turn's user message can be missing from the loaded page (e.g. it was sent from
   * another tab after this one loaded); fetch it so the typing placeholder has a turn to answer.
   */
  private ensureUserTurnLoaded(threadId: string, messageId: string): void {
    const { messageService } = this.deps;
    // Snapshot from the same stream the UI renders.
    this.subscriptions.add(
      messageService.messages$.pipe(take(1)).subscribe(messages => {
        if (messages.some(message => message.id === messageId)) return;
        this.subscriptions.add(
          messageService.getMessage(messageId).subscribe({
            next: message => {
              if (!this.isActiveThread(threadId)) return;
              messageService.addMessageToList(message);
            },
            error: () => {
              // Best-effort: polling still delivers the reply.
            },
          }),
        );
      }),
    );
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
    this.deps.streamingService.startStreaming(latest);
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

  private handleJobProgressSnapshot(job: Job): void {
    if (!job) return;
    // Only the active job drives the generating/phase state. A prior job may still
    // be polling its post-inference phases (expression/summarization) after the
    // composer unlocked, and its late snapshots must not overwrite the new job's phase.
    if (this._activeChatJobId() === job.id) {
      this._activeJobPhase.set(job.status);
      const toolCalls = parseChatTurnToolCalls(job.progress);
      if (toolCalls) this._liveToolCalls.set(toolCalls);
    }
    const cancelPendingForJob = this._cancelRequestedJobId() === job.id;
    if (cancelPendingForJob && job.status === 'cancelled') {
      this._cancelRequestedJobId.set(null);
    }
    // Reasoning is rendered from the whole array each snapshot (not a cursor like the
    // text below): the server resets it when a truncated call is retried. Only the
    // active job drives it, and a pending cancel freezes it like the text draft. Past
    // `processing` the server has already cleared the draft; keep showing the last
    // snapshot until the saved message (which carries model_reasoning) replaces it.
    const inferenceRunning = job.status === 'pending' || job.status === 'processing';
    if (this._activeChatJobId() === job.id && !cancelPendingForJob && inferenceRunning) {
      this._pendingAssistantDraftReasoning.set((job.draft_reasoning ?? []).join(''));
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
    this.deps.streamingService.appendServerChunks(CHAT_PENDING_ASSISTANT_MESSAGE_ID, newChunks);
  }

  private finishPendingDraftStream(clearPendingDraft: boolean = true): void {
    const { streamingService } = this.deps;
    const pendingText = this._pendingAssistantDraftText();
    if (pendingText) {
      streamingService.completeStreaming(CHAT_PENDING_ASSISTANT_MESSAGE_ID, pendingText, true);
    } else {
      streamingService.stopStreaming(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
    if (this._streamingMessageId() === CHAT_PENDING_ASSISTANT_MESSAGE_ID) {
      this._streamingMessageId.set(null);
    }
    if (clearPendingDraft) {
      this._pendingAssistantDraftText.set('');
      this._pendingAssistantDraftReasoning.set('');
      streamingService.clearMessageState(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    }
  }

  private cancelStreamingWithOptions(opts: { clearPendingDraft: boolean }): void {
    const messageId = this._streamingMessageId();
    if (messageId) {
      const fullText = messageId === CHAT_PENDING_ASSISTANT_MESSAGE_ID ? this._pendingAssistantDraftText() : undefined;
      this.deps.streamingService.stopStreaming(messageId, fullText, false);
      this._streamingMessageId.set(null);
    }
    if (opts.clearPendingDraft && messageId === CHAT_PENDING_ASSISTANT_MESSAGE_ID) {
      this._pendingAssistantDraftText.set('');
      this._pendingAssistantDraftReasoning.set('');
    }
    this.expectingAssistantResponse = false;
    this.expectedAssistantAfterUserMessageId = null;
  }

  private requestCancelForJob(jobId: string): void {
    const threadId = this.deps.activeThreadId();
    this.subscriptions.add(
      this.deps.jobService.cancelJob(jobId).subscribe({
        error: err => {
          if (threadId === null || !this.isActiveThread(threadId)) return;
          if (this._cancelRequestedJobId() === jobId) {
            this._cancelRequestedJobId.set(null);
          }
          this.deps.reportError(apiErrorMessage(err, 'Failed to stop response'));
        },
      }),
    );
  }

  /** Ends the current thread's job polls (the server-side jobs keep running). */
  private stopPolling(): void {
    this.jobSubscriptions.unsubscribe();
    this.jobSubscriptions = new Subscription();
  }

  private isActiveThread(threadId: string): boolean {
    return this.deps.activeThreadId() === threadId;
  }
}

/** The tool timeline from a chat_message job's progress payload; undefined when absent or unreadable. */
export function parseChatTurnToolCalls(progress: string | undefined): ChatTurnToolCall[] | undefined {
  if (!progress) return undefined;
  try {
    const parsed = JSON.parse(progress) as Partial<ChatTurnProgress>;
    return Array.isArray(parsed.tool_calls) ? parsed.tool_calls : undefined;
  } catch {
    return undefined;
  }
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

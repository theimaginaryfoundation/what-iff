import { DestroyRef, Injectable, computed, effect, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';

import { ChatBranchSummary, ChatLineage } from '../../../core/models/chat.model';
import { ChatMessage } from '../../../core/models/message.model';
import { ChatService } from '../../../core/services/chat.service';
import { DraftMessageService } from '../../../core/services/draft-message.service';
import { ThreadListService } from '../../../core/services/thread-list.service';
import { ChatSessionService } from '../chat-session.service';
import { branchBannerText, branchPlanFor, groupBranchesByMessage } from '../helpers/branch.helpers';

/** Query param that asks the chat page to scroll to (and flash) a message once it has loaded. */
export const FOCUS_MESSAGE_QUERY_PARAM = 'focus';

/**
 * "What if…" branching for the active thread: loads lineage (where it branched from, what
 * branched off it), creates branches from a message, and navigates between them.
 * Provided per chat page alongside ChatSessionService.
 */
@Injectable()
export class ChatBranchingService {
  private readonly session = inject(ChatSessionService);
  private readonly chatService = inject(ChatService);
  private readonly drafts = inject(DraftMessageService);
  private readonly threadList = inject(ThreadListService);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

  readonly lineage = signal<ChatLineage | null>(null);
  readonly creating = signal(false);
  readonly error = signal<string | null>(null);

  readonly branchesByMessage = computed(() => groupBranchesByMessage(this.lineage()?.branches));
  readonly bannerText = computed(() => branchBannerText(this.lineage()));
  readonly parent = computed(() => this.lineage()?.parent ?? null);

  /** Branching needs a settled, writable thread. */
  readonly enabled = computed(() => {
    const thread = this.session.thread();
    return !!thread && thread.archived !== true && !this.session.isGenerating() && !this.creating();
  });

  /** True while a branch's earlier history is being summarized server-side. */
  readonly catchingUp = computed(() => {
    const state = this.session.thread()?.rehydration_state;
    return !!this.lineage()?.parent && (state === 'pending' || state === 'processing');
  });

  private lastThreadId: string | null = null;
  private readonly loadLineageOnThread = effect(() => {
    const threadId = this.session.thread()?.id ?? null;
    if (threadId === this.lastThreadId) return;
    this.lastThreadId = threadId;
    this.lineage.set(null);
    this.error.set(null);
    if (threadId) this.refresh(threadId);
  });

  refresh(threadId: string): void {
    this.chatService
      .getChatLineage(threadId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: lineage => {
          if (this.session.thread()?.id === threadId) this.lineage.set(lineage);
        },
        // Lineage is decorative; a failure just hides branch markers.
        error: () => {},
      });
  }

  /** Create a branch from a message and open it (user messages come back as an editable draft). */
  branchFrom(message: ChatMessage): void {
    const thread = this.session.thread();
    if (!thread || !this.enabled()) return;
    const plan = branchPlanFor(message);
    this.creating.set(true);
    this.error.set(null);
    this.chatService
      .forkChat(thread.id, plan.request)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: res => {
          this.creating.set(false);
          if (plan.draft) this.drafts.saveDraft(res.chat.id, plan.draft);
          void this.threadList.refresh();
          void this.router.navigate(['/chat', res.chat.id]);
        },
        error: err => {
          this.creating.set(false);
          this.error.set(err?.message ?? 'Could not create the branch.');
        },
      });
  }

  openBranch(branch: ChatBranchSummary): void {
    void this.router.navigate(['/chat', branch.id]);
  }

  /** Jump to the parent thread at the message this branch diverged from. */
  openParent(): void {
    const parent = this.parent();
    if (!parent || parent.deleted) return;
    const queryParams = parent.message_id ? { [FOCUS_MESSAGE_QUERY_PARAM]: parent.message_id } : {};
    void this.router.navigate(['/chat', parent.id], { queryParams });
  }
}

import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Chat, isSandboxed } from '../../../core/models/chat.model';
import { AnnouncerService } from '../../../core/services/announcer.service';
import { ChatService } from '../../../core/services/chat.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ThreadListService } from '../../../core/services/thread-list.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { ContextPanelService } from './context-panel.service';

export type LeaveSandboxResult = { status: 'left' } | { status: 'cancelled' } | { status: 'failed'; message: string };

/**
 * Takes a thread out of its sandbox, after the owner confirms. One place for the header chip and the
 * Memories panel, so both show the same confirmation. A thread can leave a sandbox but never enter
 * one (docs/ARCHITECTURE_SUMMARY.md), so there is no opposite call. The saved chat is published so the
 * header, the sidebar and the panel update together.
 */
@Injectable({ providedIn: 'root' })
export class ThreadSandboxService {
  private readonly chatService = inject(ChatService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly announcer = inject(AnnouncerService);
  private readonly context = inject(ContextPanelService);
  private readonly threadList = inject(ThreadListService);

  async leave(chat: Chat | null | undefined): Promise<LeaveSandboxResult> {
    if (!chat || !isSandboxed(chat)) return { status: 'cancelled' };
    const confirmed = await this.confirmation.confirm({
      title: 'Remove from sandbox?',
      message:
        "This thread will be able to read your memories, other conversations, scratchpad and account-wide files, jobs, skills and personalities, and will use the full tool set. Everything said in it so far, by anyone who could talk to it, becomes part of what your personality learns from. This can't be undone: a thread can only be sandboxed when it is created.",
      confirmText: 'Remove from sandbox',
      type: 'warning',
    });
    if (!confirmed) return { status: 'cancelled' };
    try {
      const updated = await firstValueFrom(this.chatService.patchChat(chat.id, { context_scope: 'account' }));
      this.context.publishThreadUpdate(updated);
      // The sidebar and Thread Manager read their own list; without this the row keeps its shield.
      this.threadList.applyChatUpdate(updated);
      this.announcer.announce('Removed from the sandbox. This thread can now use your memories, conversations and files.');
      return { status: 'left' };
    } catch (error) {
      return { status: 'failed', message: apiErrorMessage(error, 'Failed to remove the thread from the sandbox') };
    }
  }
}

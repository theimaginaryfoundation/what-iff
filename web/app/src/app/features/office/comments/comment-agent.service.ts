import { Injectable, Signal, computed, effect, inject, signal, untracked } from '@angular/core';

import { CommentMessage, CommentRef } from '../../../core/models/message.model';
import { FileWorkspaceService } from '../../file-workspace/file-workspace.service';
import { CommentPerson, CommentThread } from './comment-threads';

/** The agent a person can mention in a comment: the agent of the chat the file is open in. */
export function agentPersonId(personalityId: string | null): string {
  return `agent:${personalityId ?? 'chat'}`;
}

/**
 * The open chat, as comments see it (ADR 0x046): its agent, and how to send it a comment. The
 * chat page registers itself while it is shown; the file workspace sits outside it, in the layout.
 */
export interface CommentAgentHost {
  readonly chatId: Signal<string | null>;
  readonly agent: Signal<CommentPerson | null>;
  /** Sends the comment to the agent as a message of the chat; false when it was not sent. */
  sendComment(text: string, comment: CommentMessage, ref: CommentRef): Promise<boolean>;
}

/**
 * What a comments host does about the chat's agent (ADR 0x046): who else a comment can mention,
 * and what to do once a comment is posted (send it to the agent when it mentions it).
 */
export interface CommentAgentHooks {
  people(people: CommentPerson[]): CommentPerson[];
  /** `quote` is what the thread is on as the panel shows it (empty for the whole file). */
  posted(thread: CommentThread<unknown>, commentId: string, quote: string): void;
}

/**
 * Comments that mention the agent (ADR 0x046). A comment on a file opened in a chat can mention
 * the chat's agent; posting it sends the agent a message whose text is the comment's, with what
 * the thread is on and the thread so far as context the chat does not show.
 */
@Injectable({ providedIn: 'root' })
export class CommentAgentService {
  private readonly workspace = inject(FileWorkspaceService);
  private readonly host = signal<CommentAgentHost | null>(null);

  /** The agent that can be mentioned on the file open now, or null outside a chat. */
  readonly agent = computed<CommentPerson | null>(() => {
    const host = this.host();
    const chatId = this.workspace.chatId();
    return host && chatId && host.chatId() === chatId ? host.agent() : null;
  });

  /** The files the agent's last reply commented on, and a count that changes with every reply. */
  private readonly agentComments = signal<{ fileIds: ReadonlySet<string>; n: number }>({ fileIds: new Set(), n: 0 });

  /** The agent's reply commented on these files: an open one shows its comments again. */
  agentCommented(fileIds: readonly string[]): void {
    if (fileIds.length) this.agentComments.update(c => ({ fileIds: new Set(fileIds), n: c.n + 1 }));
  }

  /**
   * Calls reload whenever the agent comments on the file (call in an injection context). The
   * comments panel of a file open while the agent replies shows what it wrote once the reply lands.
   */
  watchAgentComments(file: () => { id: string } | null | undefined, reload: () => void): void {
    effect(() => {
      const { fileIds, n } = this.agentComments();
      const id = untracked(file)?.id;
      if (n && id && fileIds.has(id)) untracked(reload);
    });
  }

  /** Registers the chat shown now; the returned function unregisters it. */
  register(host: CommentAgentHost): () => void {
    this.host.set(host);
    return () => {
      if (this.host() === host) this.host.set(null);
    };
  }

  /** The hooks for the comments of a file (read when a comment is posted). */
  hooks(file: () => { id: string; name: string }): CommentAgentHooks {
    return {
      people: people => this.withAgent(people),
      posted: (thread, commentId, quote) => void this.posted(file(), thread, commentId, quote),
    };
  }

  /** The people a comment can mention: those given, and the chat's agent after the first. */
  withAgent(people: CommentPerson[]): CommentPerson[] {
    const agent = this.agent();
    if (!agent || people.some(p => p.id === agent.id)) return people;
    return [...people.slice(0, 1), agent, ...people.slice(1)];
  }

  /**
   * Called once a comment is posted: when it mentions the agent, sends it. `quote` is what the
   * thread is on as the panel shows it (empty for the whole file).
   */
  async posted(file: { id: string; name: string }, thread: CommentThread<unknown>, commentId: string, quote: string): Promise<boolean> {
    const agent = this.agent();
    const host = this.host();
    const at = thread.comments.findIndex(c => c.id === commentId);
    const comment = thread.comments[at];
    if (!agent || !host || !comment?.mentions?.some(m => m.id === agent.id)) return false;
    const message: CommentMessage = {
      file_id: file.id,
      thread_id: thread.id,
      comment_id: comment.id,
      quote,
      earlier: thread.comments.slice(0, at).map(c => ({ author_name: c.authorName, text: c.text })),
    };
    const ref: CommentRef = { file_id: file.id, file_name: file.name, thread_id: thread.id, comment_id: comment.id, quote };
    return host.sendComment(comment.text, message, ref);
  }
}

import { DatePipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChildren,
} from '@angular/core';

import {
  AvatarComponent,
  CheckIconComponent,
  EditIconComponent,
  IconButtonComponent,
  TooltipDirective,
  TrashIconComponent,
  XIconComponent,
} from '../../../shared/ui';
import { CommentDraft, CommentPerson, CommentThread, DocComment } from './comment-threads';
import { MentionInputComponent } from './mention-input.component';

/** A piece of a comment's text: plain, or a mention to show as one. */
interface TextPart {
  text: string;
  mention: boolean;
}

export interface ReplyEvent {
  threadId: string;
  draft: CommentDraft;
}

export interface EditEvent extends ReplyEvent {
  commentId: string;
}

/**
 * The comments beside a document or a deck (ADR 0x045): every thread in the order the editor gives
 * them, the one being added, replies, edits, resolving and deleting. It holds no comments itself;
 * the editor owns the threads and acts on what this emits. Read-only shows the threads without
 * the boxes and buttons that would change them.
 */
@Component({
  selector: 'app-comments-panel',
  standalone: true,
  imports: [
    AvatarComponent,
    CheckIconComponent,
    DatePipe,
    EditIconComponent,
    IconButtonComponent,
    MentionInputComponent,
    TooltipDirective,
    TrashIconComponent,
    XIconComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './comments-panel.component.html',
  styleUrl: './comments-panel.component.scss',
})
export class CommentsPanelComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly threads = input.required<readonly CommentThread<unknown>[]>();
  readonly activeId = input<string | null>(null);
  /** The thread being started: what it is about, before its first comment is posted. */
  readonly draftQuote = input<string | null | undefined>(undefined);
  readonly me = input.required<CommentPerson>();
  readonly people = input<readonly CommentPerson[]>([]);
  readonly readOnly = input(false);
  /** Said when there are no threads, about how to start one. */
  readonly emptyHint = input('Select something and choose Comment to start a thread.');

  readonly select = output<string>();
  readonly create = output<CommentDraft>();
  readonly cancelDraft = output<void>();
  readonly reply = output<ReplyEvent>();
  readonly edit = output<EditEvent>();
  readonly remove = output<{ threadId: string; commentId: string }>();
  readonly resolve = output<{ threadId: string; resolved: boolean }>();
  readonly closed = output<void>();

  protected readonly showResolved = signal(false);
  /** The comment being edited, by id. */
  protected readonly editing = signal<string | null>(null);
  /** The thread whose reply box is open. */
  protected readonly replying = signal<string | null>(null);

  protected readonly drafting = computed(() => this.draftQuote() !== undefined);
  protected readonly resolvedCount = computed(() => this.threads().filter(t => t.resolved).length);
  protected readonly shown = computed(() => {
    const active = this.activeId();
    return this.threads().filter(t => !t.resolved || this.showResolved() || t.id === active);
  });

  private readonly cards = viewChildren<ElementRef<HTMLElement>>('card');

  constructor() {
    // The active thread is brought into view when the editor selects it.
    effect(() => {
      const active = this.activeId();
      this.cards();
      if (!active) return;
      untracked(() => {
        const card = this.host.nativeElement.querySelector<HTMLElement>(`[data-thread-id="${CSS.escape(active)}"]`);
        card?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
      });
    });
  }

  protected isMine(comment: DocComment): boolean {
    return comment.authorId === this.me().id;
  }

  /** The text with each mention's `@Name` as its own part. */
  protected parts(comment: DocComment): TextPart[] {
    const names = (comment.mentions ?? []).map(m => `@${m.name}`).sort((a, b) => b.length - a.length);
    if (!names.length) return [{ text: comment.text, mention: false }];
    const out: TextPart[] = [];
    let rest = comment.text;
    while (rest) {
      let at = -1;
      let hit = '';
      for (const name of names) {
        const i = rest.indexOf(name);
        if (i >= 0 && (at < 0 || i < at)) {
          at = i;
          hit = name;
        }
      }
      if (at < 0) {
        out.push({ text: rest, mention: false });
        break;
      }
      if (at > 0) out.push({ text: rest.slice(0, at), mention: false });
      out.push({ text: hit, mention: true });
      rest = rest.slice(at + hit.length);
    }
    return out;
  }

  protected draftOf(comment: DocComment): CommentDraft {
    return { text: comment.text, mentions: comment.mentions ?? [] };
  }

  protected onSelect(thread: CommentThread<unknown>): void {
    if (thread.id !== this.activeId()) this.select.emit(thread.id);
  }

  protected startReply(threadId: string): void {
    this.replying.set(threadId);
    this.select.emit(threadId);
  }

  protected postReply(threadId: string, draft: CommentDraft): void {
    this.reply.emit({ threadId, draft });
    this.replying.set(null);
  }

  protected saveEdit(threadId: string, commentId: string, draft: CommentDraft): void {
    this.edit.emit({ threadId, commentId, draft });
    this.editing.set(null);
  }

  protected toggleResolved(): void {
    this.showResolved.update(v => !v);
  }
}

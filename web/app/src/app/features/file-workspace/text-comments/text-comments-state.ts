import { Signal, computed, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { CommentSaveStatus } from '../../office/comments/comment-autosave';
import {
  CommentDraft,
  CommentPerson,
  addReply,
  commentAuthors,
  editComment,
  newComment,
  removeComment,
  setResolved,
} from '../../office/comments/comment-threads';
import { CommentAgentHooks } from '../../office/comments/comment-agent.service';
import { EditEvent, ReplyEvent } from '../../office/comments/comments-panel.component';
import { TextCommentAnchor, TextCommentThread, TextComments, TextCommentsService } from './text-comments.service';

type Range = { start: number; end: number };

/**
 * Where a file's threads are shown (ADR 0x045): the Monaco editor's decorations, which follow the
 * text as it is typed, or the viewer's highlights. Ranges are UTF-16 offsets into the text.
 */
export interface TextCommentSurface {
  /** Marks a range as a thread's (or clears the thread's mark with null). */
  place(threadId: string, range: Range | null): void;
  /** Where each marked thread's range is now; null for one whose text is all gone. */
  ranges(): Map<string, Range | null>;
  /** Marks the active thread's text more strongly and leaves hidden (resolved) threads unmarked. */
  paint(state: { activeId: string | null; hidden: ReadonlySet<string> }): void;
  /** Shows a range. */
  reveal(range: Range): void;
  text(range: Range): string;
}

/** A thread's text, as the panel quotes it: on one line, and short. */
function quoteOf(text: string): string {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length > 160 ? `${flat.slice(0, 157)}…` : flat;
}

/**
 * Three-way merge of a file's threads: `mine` and `theirs` both changed from `base`. Threads and
 * comments added on either side are kept, those removed on either side go, and a comment or
 * resolved state changed here wins over the other side's. Anchors are theirs (the server has
 * carried them onto the file as it is), except for threads only this side has.
 */
export function mergeThreads(
  base: readonly TextCommentThread[],
  mine: readonly TextCommentThread[],
  theirs: readonly TextCommentThread[],
): TextCommentThread[] {
  const baseById = new Map(base.map(t => [t.id, t]));
  const mineById = new Map(mine.map(t => [t.id, t]));
  const out: TextCommentThread[] = [];
  for (const t of theirs) {
    const was = baseById.get(t.id);
    const now = mineById.get(t.id);
    if (was && !now) continue; // removed here
    if (!was || !now) {
      out.push(t);
      continue;
    }
    const wasComments = new Map(was.comments.map(c => [c.id, c]));
    const nowComments = new Map(now.comments.map(c => [c.id, c]));
    const comments = t.comments
      .filter(c => !(wasComments.has(c.id) && !nowComments.has(c.id)))
      .map(c => {
        const mineC = nowComments.get(c.id);
        const baseC = wasComments.get(c.id);
        return mineC && baseC && mineC.text !== baseC.text ? mineC : c;
      });
    const theirIds = new Set(t.comments.map(c => c.id));
    for (const c of now.comments) if (!wasComments.has(c.id) && !theirIds.has(c.id)) comments.push(c);
    const resolved = !!now.resolved !== !!was.resolved ? now.resolved : t.resolved;
    const merged: TextCommentThread = { ...t, comments };
    if (resolved) merged.resolved = true;
    else delete merged.resolved;
    if (comments.length) out.push(merged);
  }
  const theirIds = new Set(theirs.map(t => t.id));
  for (const t of mine) if (!baseById.has(t.id) && !theirIds.has(t.id)) out.push(t);
  return out;
}

let nextId = 0;
function newThreadId(): string {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${(nextId++).toString(36)}`;
  return random;
}

/**
 * The comment threads of a text or code file (ADR 0x045), for the editor and the viewer: threads
 * on ranges of the text (or on the whole file once all of their text is gone), the active one and
 * the one being started, and saving them beside the file.
 */
export class TextCommentsState {
  readonly threads = signal<TextCommentThread[]>([]);
  readonly activeId = signal<string | null>(null);
  /** The thread being started: on a range of the text, or (range null) on the whole file. */
  readonly draft = signal<{ range: Range | null; quote: string } | null>(null);
  readonly status = signal<CommentSaveStatus>('idle');
  /** Whether the file's comments loaded: the server keeps none for a file that is not text (or too large). */
  readonly available = signal(false);
  /** Where each thread is, as last read from the surface. */
  readonly positions = signal<ReadonlyMap<string, Range | null>>(new Map());
  /** The content the server has the anchors placed on, and the revision a write sends back. */
  private contentVersion = 0;
  private revision = 0;
  private surface: TextCommentSurface | null = null;
  private fileId: string | null = null;
  /** The threads as the server last had them, which a merge after a conflict starts from. */
  private synced: TextCommentThread[] = [];

  constructor(
    private readonly api: TextCommentsService,
    private readonly me: Signal<CommentPerson>,
    /** Called after every change a save has to write. */
    private readonly changed: () => void,
    /** Called when a save finds the file's content changed elsewhere: the host shows it as it is now. */
    private readonly contentChanged?: () => void,
    private readonly agent: CommentAgentHooks | null = null,
  ) {}

  /**
   * The threads for the panel: those on the whole file first, then in text order. A thread's
   * quote is its text now; one on the whole file says what it was on.
   */
  readonly shown = computed(() => {
    const positions = this.positions();
    const rank = (t: TextCommentThread) => positions.get(t.id)?.start ?? -1;
    return this.threads()
      .map((t, i) => ({ t, i }))
      .sort((a, b) => rank(a.t) - rank(b.t) || a.i - b.i)
      .map(({ t }) => {
        const at = positions.get(t.id);
        const quote =
          at && this.surface
            ? quoteOf(this.surface.text(at))
            : t.quote
              ? `The whole file · was on “${quoteOf(t.quote)}”`
              : 'The whole file';
        return { ...t, quote };
      });
  });
  /** How a surface marks the threads: the active one more strongly, resolved ones (unless active) not at all. */
  readonly paintState = computed(() => {
    const activeId = this.activeId();
    const hidden: ReadonlySet<string> = new Set(
      this.threads()
        .filter(t => t.resolved && t.id !== activeId)
        .map(t => t.id),
    );
    return { activeId, hidden };
  });
  readonly openCount = computed(() => this.threads().filter(t => !t.resolved).length);
  readonly people = computed<CommentPerson[]>(() => {
    const me = this.me();
    const people = [me, ...commentAuthors(this.threads()).filter(p => p.id !== me.id)];
    return this.agent ? this.agent.people(people) : people;
  });

  /** Loads a file's threads; they are placed on the surface once it is attached. */
  async load(fileId: string): Promise<void> {
    this.fileId = fileId;
    this.available.set(false);
    const got = await firstValueFrom(this.api.get(fileId));
    if (this.fileId !== fileId) return;
    this.adopt(got);
    this.available.set(true);
  }

  /** The surface the threads are shown on (null while there is none). */
  attach(surface: TextCommentSurface | null): void {
    this.surface = surface;
    this.placeAll();
  }

  /** The content the anchors are placed on now: the server's, after a load or a save. */
  get placedOn(): number {
    return this.contentVersion;
  }

  private adopt(got: TextComments): void {
    // The comments shown are the server's now: those not saved are gone, and their mentions.
    this.agent?.discard();
    this.contentVersion = got.contentVersion;
    this.revision = got.revision;
    this.synced = got.threads;
    this.threads.set(got.threads);
    if (!got.threads.some(t => t.id === this.activeId())) this.activeId.set(null);
    this.placeAll();
  }

  private placeAll(): void {
    if (!this.surface) return;
    for (const t of this.threads()) this.surface.place(t.id, t.anchor);
    this.refresh();
  }

  /** Reads where each thread is from the surface (after the text changed). */
  refresh(): void {
    if (this.surface) this.positions.set(this.surface.ranges());
  }

  /** The threads with their anchors where they are now, for a save. */
  snapshot(): TextCommentThread[] {
    const ranges = this.surface?.ranges() ?? new Map<string, Range | null>();
    return this.threads().map(t => {
      if (!ranges.has(t.id)) return t;
      const at = ranges.get(t.id) ?? null;
      // A thread whose text is all gone is on the whole file, with the quote it had.
      return { ...t, anchor: at, quote: at && this.surface ? this.surface.text(at) : t.quote };
    });
  }

  /**
   * Writes the threads placed on contentVersion (what a save just stored, or what is shown). When
   * the comments changed elsewhere since they were read (an agent's reply), the two are merged and
   * written again. When the file's content changed, the comments are read again and the host shows
   * the file as it is now; the status says so.
   */
  async save(contentVersion: number, threads: TextCommentThread[] = this.snapshot(), mentions = this.mentionMark()): Promise<void> {
    const fileId = this.fileId;
    if (!fileId) return;
    this.status.set('saving');
    let sent = threads;
    for (let attempt = 0; ; attempt++) {
      try {
        const got = await firstValueFrom(this.api.put(fileId, sent, contentVersion, this.revision));
        this.contentVersion = got.contentVersion;
        this.revision = got.revision;
        if (sent !== threads) this.takeMerged(threads, got.threads);
        this.synced = got.threads;
        this.status.set('saved');
        // The comments that mention the agent are stored now: it is sent them (ADR 0x046).
        this.agent?.saved(mentions);
        return;
      } catch (error) {
        const status = (error as { status?: number }).status;
        if (status === 409 && attempt === 0) {
          const theirs = await this.reread(fileId);
          if (theirs) {
            sent = mergeThreads(this.synced, threads, theirs.threads);
            this.synced = theirs.threads;
            continue;
          }
        }
        if (status === 412) {
          await this.reread(fileId);
          this.contentChanged?.();
        }
        this.status.set(status === 409 || status === 412 ? 'conflict' : 'failed');
        throw error;
      }
    }
  }

  /** Where a save takes the comments from, for the mentions it holds (CommentAgentHooks.mark). */
  mentionMark(): number {
    return this.agent?.mark() ?? 0;
  }

  /** Whether there are changes here a save has not stored: comments, replies, resolved state (anchors aside, which move as the text is typed). */
  unsaved(): boolean {
    const shape = (threads: readonly TextCommentThread[]) =>
      JSON.stringify(threads.map(t => [t.id, !!t.resolved, t.comments.map(c => [c.id, c.text])]));
    return shape(this.threads()) !== shape(this.synced);
  }

  /** The server's comments now, taking their revision; null when they cannot be read. */
  private async reread(fileId: string): Promise<TextComments | null> {
    try {
      const got = await firstValueFrom(this.api.get(fileId));
      this.revision = got.revision;
      this.contentVersion = got.contentVersion;
      return got;
    } catch {
      return null;
    }
  }

  /** Shows what a merged save stored, keeping changes made here while it ran. */
  private takeMerged(sent: readonly TextCommentThread[], stored: readonly TextCommentThread[]): void {
    const before = new Set(this.threads().map(t => t.id));
    this.threads.set(mergeThreads(sent, this.threads(), stored));
    for (const t of this.threads()) if (!before.has(t.id)) this.surface?.place(t.id, t.anchor);
    this.refresh();
  }

  private saving = false;
  private pendingSave = false;

  /**
   * Saves the threads as they are now, placed on the content they were loaded with, after any
   * save already running (comments made while viewing are written as they are made). A save that
   * does not land stops the queue; its status says why.
   */
  autosave(): void {
    this.pendingSave = true;
    if (this.saving) return;
    this.saving = true;
    void (async () => {
      while (this.pendingSave) {
        this.pendingSave = false;
        try {
          await this.save(this.contentVersion);
        } catch {
          this.pendingSave = false;
        }
      }
      this.saving = false;
    })();
  }

  /** The thread on a text offset, the narrowest where ranges overlap. */
  threadAt(offset: number): string | null {
    let best: { id: string; size: number } | null = null;
    for (const [id, at] of this.positions()) {
      if (!at || offset < at.start || offset > at.end) continue;
      const size = at.end - at.start;
      if (!best || size < best.size) best = { id, size };
    }
    return best?.id ?? null;
  }

  /** The surface's selection moved: the thread there, if any, becomes active. */
  offsetChosen(offset: number): void {
    const id = this.threadAt(offset);
    if (id && id !== this.activeId()) {
      this.draft.set(null);
      this.activeId.set(id);
    }
  }

  start(range: Range): void {
    if (!this.surface || range.end <= range.start) return;
    this.draft.set({ range, quote: quoteOf(this.surface.text(range)) });
    this.activeId.set(null);
  }

  /** Starts a thread on the whole file (an image's, which has no text to place one on). */
  startOnFile(): void {
    this.draft.set({ range: null, quote: '' });
    this.activeId.set(null);
  }

  cancelDraft(): void {
    this.draft.set(null);
  }

  create(draft: CommentDraft): void {
    const at = this.draft();
    if (!at || (at.range && !this.surface)) return;
    const id = newThreadId();
    const anchor: TextCommentAnchor = at.range ? { ...at.range } : null;
    const quote = at.range && this.surface ? this.surface.text(at.range) : '';
    this.threads.update(threads => [...threads, { id, anchor, quote, comments: [newComment(id, this.me(), draft)] }]);
    this.surface?.place(id, anchor);
    this.draft.set(null);
    this.activeId.set(id);
    this.refresh();
    this.changed();
    this.notifyAgent(id, id);
  }

  /** Hands a posted comment to the agent hooks, with what its thread is on now. */
  private notifyAgent(threadId: string, commentId: string): void {
    const thread = this.threads().find(t => t.id === threadId);
    if (!this.agent || !thread) return;
    const at = this.positions().get(threadId);
    this.agent.posted(thread, commentId, at && this.surface ? quoteOf(this.surface.text(at)) : '');
  }

  select(threadId: string): void {
    this.draft.set(null);
    this.activeId.set(threadId);
    const at = this.positions().get(threadId);
    if (at) this.surface?.reveal(at);
  }

  reply(event: ReplyEvent): void {
    const comment = newComment(newThreadId(), this.me(), event.draft);
    this.threads.update(threads => addReply(threads, event.threadId, comment));
    this.changed();
    this.notifyAgent(event.threadId, comment.id);
  }

  edit(event: EditEvent): void {
    this.threads.update(threads => editComment(threads, event.threadId, event.commentId, event.draft));
    this.changed();
  }

  resolve(event: { threadId: string; resolved: boolean }): void {
    this.threads.update(threads => setResolved(threads, event.threadId, event.resolved));
    this.changed();
  }

  remove(event: { threadId: string; commentId: string }): void {
    if (event.threadId === event.commentId) {
      this.surface?.place(event.threadId, null);
      if (this.activeId() === event.threadId) this.activeId.set(null);
    }
    this.threads.update(threads => removeComment(threads, event.threadId, event.commentId));
    this.refresh();
    this.changed();
  }
}

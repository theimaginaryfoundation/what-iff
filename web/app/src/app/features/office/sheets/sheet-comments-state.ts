import { Signal, computed, signal } from '@angular/core';

import {
  CommentDraft,
  CommentPerson,
  addReply,
  commentAuthors,
  editComment,
  newComment,
  removeComment,
  setResolved,
} from '../comments/comment-threads';
import { CommentAgentHooks } from '../comments/comment-agent.service';
import { EditEvent, ReplyEvent } from '../comments/comments-panel.component';
import { newCommentGuid } from '../pptx/pptx-comments';
import { cellA1 } from '../shared/a1';
import { SheetCommentCells } from '../univer/univer-loader';
import { SheetCommentThread } from '../xlsx/sheet-comments';

type Cell = { sheetId: string; row: number; col: number };

/**
 * The comment threads of a workbook (ADR 0x045), as the spreadsheet editor shows them: the active
 * sheet's threads in the panel, in cell order, the active one, and the thread being started. Each
 * thread is on one cell, which carries its id (xlsx/sheet-comments.ts), so where a thread is comes
 * from the grid, and moves with it.
 */
export class SheetCommentsState {
  readonly threads = signal<SheetCommentThread[]>([]);
  readonly activeId = signal<string | null>(null);
  /** The cell a thread is being started on, until its first comment is posted. */
  readonly draft = signal<(Cell & { quote: string }) | null>(null);
  /** Where each thread's cell is, as last read from the grid. */
  readonly cells = signal<ReadonlyMap<string, Cell>>(new Map());
  readonly activeSheetId = signal<string | null>(null);
  private grid: SheetCommentCells | null = null;

  constructor(
    private readonly me: Signal<CommentPerson>,
    /** Called after every change a save has to write. */
    private readonly changed: () => void,
    private readonly agent: CommentAgentHooks | null = null,
  ) {}

  /** Threads with a cell; a thread whose cell was deleted goes with it. */
  private readonly placed = computed(() => {
    const cells = this.cells();
    return this.threads().filter(t => cells.has(t.id));
  });

  /** The active sheet's threads, in row then column order, each labelled by its cell. */
  readonly onSheet = computed(() => {
    const cells = this.cells();
    const sheet = this.activeSheetId();
    return this.placed()
      .filter(t => cells.get(t.id)!.sheetId === sheet)
      .map(t => ({ t, at: cells.get(t.id)! }))
      .sort((a, b) => a.at.row - b.at.row || a.at.col - b.at.col)
      .map(({ t, at }) => ({ ...t, quote: cellA1(at.row, at.col) }));
  });
  readonly openCount = computed(() => this.placed().filter(t => !t.resolved).length);
  readonly people = computed<CommentPerson[]>(() => {
    const me = this.me();
    const people = [me, ...commentAuthors(this.threads()).filter(p => p.id !== me.id)];
    return this.agent ? this.agent.people(people) : people;
  });

  load(threads: SheetCommentThread[]): void {
    this.threads.set(threads);
    this.activeId.set(null);
    this.draft.set(null);
  }

  attach(grid: SheetCommentCells | null): void {
    this.grid = grid;
    this.refresh();
  }

  /** Reads where every thread is from the grid (after a change that may have moved cells). */
  refresh(): void {
    if (!this.grid) return;
    this.cells.set(this.grid.locate());
    this.activeSheetId.set(this.grid.activeSheetId());
  }

  /** The grid's selection moved to a cell: its thread, if any, becomes active. */
  cellChosen(cell: Cell): void {
    this.activeSheetId.set(cell.sheetId);
    const id = this.grid?.at(cell.sheetId, cell.row, cell.col) ?? null;
    if (id && this.threads().some(t => t.id === id) && id !== this.activeId()) {
      this.draft.set(null);
      this.activeId.set(id);
    }
  }

  /** Starts a thread on the selected cell; a cell that has one already opens it instead, as Excel does. */
  start(): 'started' | 'existing' | 'none' {
    const cell = this.grid?.activeCell();
    if (!cell) return 'none';
    const existing = this.grid?.at(cell.sheetId, cell.row, cell.col);
    if (existing && this.threads().some(t => t.id === existing)) {
      this.select(existing);
      return 'existing';
    }
    this.draft.set({ ...cell, quote: cellA1(cell.row, cell.col) });
    this.activeId.set(null);
    return 'started';
  }

  cancelDraft(): void {
    this.draft.set(null);
  }

  async create(draft: CommentDraft): Promise<void> {
    const at = this.draft();
    if (!at || !this.grid) return;
    const id = newCommentGuid();
    if (!(await this.grid.tag(at.sheetId, at.row, at.col, id))) return;
    this.threads.update(threads => [...threads, { id, anchor: { sheetId: at.sheetId }, comments: [newComment(id, this.me(), draft)] }]);
    this.draft.set(null);
    this.activeId.set(id);
    this.refresh();
    // The mention is queued before the change starts a save (a viewer saves at once), so that save
    // holds it and sends it when it lands.
    this.notifyAgent(id, id);
    this.changed();
  }

  /** Hands a posted comment to the agent hooks, with the cell its thread is on. */
  private notifyAgent(threadId: string, commentId: string): void {
    const thread = this.threads().find(t => t.id === threadId);
    if (!this.agent || !thread) return;
    const at = this.cells().get(threadId);
    this.agent.posted(thread, commentId, at ? `Cell ${cellA1(at.row, at.col)}` : '');
  }

  /** Chooses a thread: it becomes active, and its cell is shown. */
  select(threadId: string): void {
    this.draft.set(null);
    this.activeId.set(threadId);
    const at = this.cells().get(threadId);
    if (at) this.grid?.reveal(at.sheetId, at.row, at.col);
  }

  reply(event: ReplyEvent): void {
    const comment = newComment(newCommentGuid(), this.me(), event.draft);
    this.threads.update(threads => addReply(threads, event.threadId, comment));
    this.notifyAgent(event.threadId, comment.id);
    this.changed();
  }

  edit(event: EditEvent): void {
    this.threads.update(threads => editComment(threads, event.threadId, event.commentId, event.draft));
    this.changed();
  }

  resolve(event: { threadId: string; resolved: boolean }): void {
    this.threads.update(threads => setResolved(threads, event.threadId, event.resolved));
    this.changed();
  }

  async remove(event: { threadId: string; commentId: string }): Promise<void> {
    if (event.threadId === event.commentId) {
      const at = this.cells().get(event.threadId);
      if (at) await this.grid?.tag(at.sheetId, at.row, at.col, null);
      if (this.activeId() === event.threadId) this.activeId.set(null);
    }
    this.threads.update(threads => removeComment(threads, event.threadId, event.commentId));
    this.refresh();
    this.changed();
  }

  /** The thread on the selected cell, for the right-click menu. */
  atSelection(): string | null {
    const cell = this.grid?.activeCell();
    const id = cell ? this.grid?.at(cell.sheetId, cell.row, cell.col) : null;
    return id && this.threads().some(t => t.id === id) ? id : null;
  }
}

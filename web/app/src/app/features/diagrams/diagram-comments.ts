import { computed, signal } from '@angular/core';

import { TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { BridgeCell, BridgeEvent, DrawioAction } from './drawio-frame';

export type CellMarks = Record<string, 'open' | 'active'>;

/**
 * Comments on a diagram's elements (ADR 0x049), between the comment threads (TextCommentsState,
 * as for text and images) and our plugin in the editor: what it reports (the page, the selection,
 * clicks, right-click items) starts and chooses threads, and the threads are shown back to it as
 * marks on their elements.
 */
export class DiagramComments {
  /** The page the editor shows (its id), once the plugin said. */
  readonly page = signal<string | null>(null);
  /** The element Comment starts a thread on: the one selected or last clicked. */
  readonly chosen = signal<BridgeCell | null>(null);
  /** Opens a thread's reply box in the panel (the right-click menu's "Reply to comment"). */
  readonly replyRequest = signal<{ threadId: string } | null>(null);

  constructor(
    readonly state: TextCommentsState,
    /** Sends an action to the editor. */
    private readonly send: (action: DrawioAction) => void,
    /** Opens the comments panel. */
    private readonly openPanel: () => void,
    /** Whether the user can start and change threads here. */
    private readonly canComment: () => boolean,
  ) {}

  /** The marks for the page shown: open threads' elements, and the active (or new) thread's more strongly. */
  readonly marks = computed<CellMarks>(() => {
    const page = this.page();
    const activeId = this.state.activeId();
    const draftCell = this.state.draft()?.cell;
    const marks: CellMarks = {};
    const here = (cellPage: string | undefined) => !cellPage || !page || cellPage === page;
    for (const t of this.state.threads()) {
      if (!t.cell || !here(t.cell.page)) continue;
      if (t.id === activeId) marks[t.cell.id] = 'active';
      else if (!t.resolved && marks[t.cell.id] !== 'active') marks[t.cell.id] = 'open';
    }
    if (draftCell && here(draftCell.page)) marks[draftCell.id] = 'active';
    return marks;
  });

  /** The action that shows the marks to the editor. */
  marksAction(): DrawioAction {
    return { action: 'whatiff-marks', page: this.page(), marks: this.marks() };
  }

  /** Handles what the plugin reported. */
  handle(event: BridgeEvent): void {
    switch (event.event) {
      case 'whatiff-page':
        this.page.set(event.page);
        this.chosen.set(null);
        break;
      case 'whatiff-selection':
        this.page.set(event.page);
        this.chosen.set(event.cells.length === 1 ? event.cells[0] : null);
        break;
      case 'whatiff-click': {
        this.chosen.set({ id: event.cell, label: event.label ?? '' });
        const threadId = this.state.threadOnCell({ id: event.cell, page: event.page ?? undefined });
        if (threadId) {
          this.state.select(threadId);
          this.openPanel();
        }
        break;
      }
      case 'whatiff-menu':
        this.menu(event);
        break;
    }
  }

  /** Starts a thread on the chosen element, or on the whole diagram when none is chosen. */
  startComment(): void {
    if (!this.canComment()) return;
    const cell = this.chosen();
    if (cell) this.startOn(cell);
    else this.state.startOnFile();
    this.openPanel();
  }

  /** Chooses a thread from the panel: its element is selected and shown in the editor. */
  selectThread(threadId: string): void {
    if (typeof threadId !== 'string') return; // a DOM select event (TextCommentsState.select)
    this.state.select(threadId);
    const cell = this.state.threads().find(t => t.id === threadId)?.cell;
    if (cell) this.send({ action: 'whatiff-reveal', cell: cell.id, ...(cell.page ? { page: cell.page } : {}) });
  }

  private menu(event: Extract<BridgeEvent, { event: 'whatiff-menu' }>): void {
    const cell = { id: event.cell, page: event.page ?? undefined };
    const threadId = this.state.threadOnCell(cell);
    switch (event.item) {
      case 'new':
        if (!this.canComment()) return;
        this.startOn({ id: event.cell, label: event.label ?? '' });
        this.openPanel();
        break;
      case 'reply':
        if (!threadId) return;
        this.state.select(threadId);
        this.openPanel();
        if (this.canComment()) this.replyRequest.set({ threadId });
        break;
      case 'delete':
        if (threadId && this.canComment()) this.state.remove({ threadId, commentId: threadId });
        break;
    }
  }

  private startOn(cell: BridgeCell): void {
    const page = this.page();
    this.state.startOnCell(page ? { id: cell.id, page } : { id: cell.id }, cell.label);
  }
}

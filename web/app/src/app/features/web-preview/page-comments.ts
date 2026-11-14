import { computed, signal } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { PageElement } from '../file-workspace/text-comments/text-comments.service';
import { PageBridgeAction, PageBridgeEvent, PageMark } from './page-bridge';
import { formatSourceRange, parseSourceRange } from './source-positions';

/** The mark id of the thread being started. */
const DRAFT = 'draft';

/**
 * Comments on a rendered page's elements (ADR 0x050 §5), between the page's comment threads
 * (TextCommentsState, kept with the HTML file as for any text file) and the bridge script in the
 * page: a pick starts a thread, and the threads are shown back as marks on their elements.
 *
 * A thread's mark carries its code anchor as a source range in this file, so an element the HTML
 * declares is found by its tag even after the file was edited (the server carries the range), and
 * its element anchor (selector and fingerprint) for an element a script made. A pick on an element
 * of this file starts a thread on its tag too; one on an element a script made, or from another
 * file, starts a thread on the element alone.
 */
export class PageComments {
  /** Whether the page is waiting for an element to be picked. */
  readonly picking = signal(false);
  /** Threads with an anchor on the page whose element the page did not find, as it last said. */
  readonly unplaced = signal<ReadonlySet<string>>(new Set());

  constructor(
    readonly state: TextCommentsState,
    /** Sends an action to the page's bridge. */
    private readonly send: (action: PageBridgeAction) => void,
    /** Opens the comments panel. */
    private readonly openPanel: () => void,
    /** Whether the user can start and change threads here. */
    private readonly canComment: () => boolean,
    /** The page's file. */
    private readonly page: () => FileAttachment,
  ) {}

  /** The marks for the page: open threads' elements, and the active (or new) thread's more strongly. */
  readonly marks = computed<PageMark[]>(() => {
    const page = this.page().id;
    const positions = this.state.positions();
    const activeId = this.state.activeId();
    const marks: PageMark[] = [];
    const mark = (
      id: string,
      state: PageMark['state'],
      at: { start: number; end: number } | null | undefined,
      element: PageElement | undefined,
    ) => {
      const onPage = element && element.page === page ? element : undefined;
      if (!at && !onPage) return;
      marks.push({
        id,
        state,
        ...(at ? { src: formatSourceRange({ file: page, ...at }) } : {}),
        ...(onPage ? { selector: onPage.selector, ...(onPage.fingerprint ? { fingerprint: onPage.fingerprint } : {}) } : {}),
      });
    };
    for (const t of this.state.threads()) {
      if (t.resolved && t.id !== activeId) continue;
      mark(t.id, t.id === activeId ? 'active' : 'open', positions.get(t.id), t.element);
    }
    const draft = this.state.draft();
    if (draft) mark(DRAFT, 'active', draft.range, draft.element);
    return marks;
  });

  /** The action that shows the marks to the page. */
  marksAction(): PageBridgeAction {
    return { type: 'marks', marks: this.marks() };
  }

  /** Enters the page's element picker, or leaves it. */
  togglePick(): void {
    if (this.picking()) {
      this.picking.set(false);
      this.send({ type: 'stop-pick' });
      return;
    }
    if (!this.canComment()) return;
    this.picking.set(true);
    this.send({ type: 'pick' });
  }

  /** Shows a thread chosen in the panel on the page. */
  reveal(threadId: string): void {
    this.send({ type: 'reveal', id: threadId });
  }

  /** Handles what the page's bridge reported. */
  handle(event: PageBridgeEvent): void {
    switch (event.type) {
      case 'ready':
        // A page that (re)loaded has no marks and is not picking.
        if (this.picking()) this.send({ type: 'pick' });
        this.send(this.marksAction());
        break;
      case 'picked': {
        this.picking.set(false);
        if (!this.canComment()) return;
        const page = this.page().id;
        const src = parseSourceRange(event.src);
        const element: PageElement = { page, selector: event.selector, fingerprint: event.fingerprint };
        // The element's tag is its code when the page's own file declares it.
        const range = src && src.file === page ? { start: src.start, end: src.end } : null;
        this.state.startOnElement(element, range, event.label);
        this.openPanel();
        break;
      }
      case 'pick-cancelled':
        this.picking.set(false);
        break;
      case 'select':
        if (event.id === DRAFT) return;
        this.state.select(event.id);
        this.openPanel();
        break;
      case 'unplaced':
        this.unplaced.set(new Set(event.ids.filter(id => id !== DRAFT)));
        break;
    }
  }
}

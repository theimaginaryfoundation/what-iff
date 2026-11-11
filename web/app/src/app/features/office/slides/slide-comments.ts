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
import { EditEvent, ReplyEvent } from '../comments/comments-panel.component';
import { Deck, Slide, SlideElement } from '../pptx/deck-model';
import { SlideCommentAnchor, SlideCommentThread, newCommentGuid } from '../pptx/pptx-comments';
import { CommentPin } from './slide-canvas.component';

/** Where a slide thread's pin goes when it is about the whole slide: along the top, clear of earlier ones. */
const SLIDE_PIN = { x: 12, y: 24, step: 26 };

/**
 * The comment threads of a deck (ADR 0x045), as the slides editor shows them: the current slide's
 * threads in the panel and as pins, the active one, and the thread being started. The threads are
 * kept beside the deck rather than in it, so undoing an edit to a slide never undoes a comment.
 */
export class SlideComments {
  readonly threads = signal<SlideCommentThread[]>([]);
  readonly activeId = signal<string | null>(null);
  /** The thread being started, until its first comment is posted. */
  readonly draft = signal<{ anchor: SlideCommentAnchor; quote: string } | null>(null);

  constructor(
    private readonly deck: Signal<Deck>,
    private readonly slide: Signal<Slide>,
    private readonly me: Signal<CommentPerson>,
    /** Called after every change a save has to write. */
    private readonly changed: () => void,
  ) {}

  /** Threads on slides the deck still has; a deleted slide takes its threads with it. */
  private readonly live = computed(() => {
    const slides = new Set(this.deck().slides.map(s => s.id));
    return this.threads().filter(t => slides.has(t.anchor.slideId));
  });

  /**
   * The current slide's threads, in the order they were started. A file does not say what a thread
   * is about in words, so one read from it is labelled by its shape, or by its slide.
   */
  readonly onSlide = computed(() => {
    const slide = this.slide();
    const number = this.deck().slides.indexOf(slide) + 1;
    return this.live()
      .filter(t => t.anchor.slideId === slide.id)
      .map(t => {
        if (t.quote) return t;
        const element = t.anchor.elementId ? slide.elements.find(e => e.id === t.anchor.elementId) : undefined;
        return { ...t, quote: element?.name || `Slide ${number}` };
      });
  });
  readonly openCount = computed(() => this.live().filter(t => !t.resolved).length);
  /** Open threads per slide id, for the rail. */
  readonly openBySlide = computed(() => {
    const out = new Map<string, number>();
    for (const t of this.live()) if (!t.resolved) out.set(t.anchor.slideId, (out.get(t.anchor.slideId) ?? 0) + 1);
    return out;
  });
  readonly people = computed<CommentPerson[]>(() => {
    const me = this.me();
    return [me, ...commentAuthors(this.threads()).filter(p => p.id !== me.id)];
  });
  /** Pins for the current slide: open threads, and a resolved one while it is active. */
  readonly pins = computed<CommentPin[]>(() => {
    const active = this.activeId();
    const draft = this.draft();
    const pins: CommentPin[] = this.onSlide()
      .filter(t => !t.resolved || t.id === active)
      .map(t => ({
        id: t.id,
        x: t.anchor.x,
        y: t.anchor.y,
        active: t.id === active,
        label: `Comment by ${t.comments[0]?.authorName ?? 'someone'}`,
      }));
    if (draft && draft.anchor.slideId === this.slide().id)
      pins.push({ id: 'draft', x: draft.anchor.x, y: draft.anchor.y, active: true, label: 'New comment' });
    return pins;
  });
  /** The element the active thread (or the one being started) is about. */
  readonly highlightId = computed(() => {
    const draft = this.draft();
    if (draft) return draft.anchor.elementId ?? null;
    return this.onSlide().find(t => t.id === this.activeId())?.anchor.elementId ?? null;
  });

  /** The deck the anchors were last placed against, so a moved shape takes its pins along. */
  private placedOn: Deck | null = null;

  load(threads: SlideCommentThread[], deck: Deck): void {
    this.placedOn = deck;
    this.threads.set(threads);
    this.activeId.set(null);
    this.draft.set(null);
  }

  /** Starts a thread on `element`, or on the slide (at `at`, when given) when there is none. */
  start(element: SlideElement | null, slideNumber: number, at?: { x: number; y: number }): void {
    const slideId = this.slide().id;
    let anchor: SlideCommentAnchor;
    if (element) anchor = { slideId, x: element.left + element.width, y: element.top, elementId: element.id };
    else if (at) anchor = { slideId, x: at.x, y: at.y };
    else {
      // Each slide thread's pin goes after the ones already along the top.
      const taken = this.onSlide().filter(t => !t.anchor.elementId && t.anchor.y === SLIDE_PIN.y).length;
      anchor = { slideId, x: SLIDE_PIN.x + taken * SLIDE_PIN.step, y: SLIDE_PIN.y };
    }
    this.draft.set({ anchor, quote: element ? element.name || 'Selected shape' : `Slide ${slideNumber}` });
    this.activeId.set(null);
  }

  cancelDraft(): void {
    this.draft.set(null);
  }

  create(draft: CommentDraft): void {
    const at = this.draft();
    if (!at) return;
    const id = newCommentGuid();
    this.threads.update(threads => [...threads, { id, anchor: at.anchor, quote: at.quote, comments: [newComment(id, this.me(), draft)] }]);
    this.draft.set(null);
    this.activeId.set(id);
    this.changed();
  }

  select(threadId: string | null): void {
    this.draft.set(null);
    this.activeId.set(threadId);
  }

  reply(event: ReplyEvent): void {
    this.threads.update(threads => addReply(threads, event.threadId, newComment(newCommentGuid(), this.me(), event.draft)));
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

  remove(event: { threadId: string; commentId: string }): void {
    if (event.threadId === event.commentId && this.activeId() === event.threadId) this.activeId.set(null);
    this.threads.update(threads => removeComment(threads, event.threadId, event.commentId));
    this.changed();
  }

  /**
   * Moves the pins of threads about shapes that moved since the deck was last seen (a drag, an
   * undo), so a pin stays where it was on its shape. Not a change of its own: the shape moved.
   */
  follow(deck: Deck): void {
    const before = this.placedOn;
    this.placedOn = deck;
    if (!before || before === deck) return;
    const where = (d: Deck, slideId: string, elementId: string) =>
      d.slides.find(sl => sl.id === slideId)?.elements.find(e => e.id === elementId);
    let moved = false;
    const next = this.threads().map(t => {
      if (!t.anchor.elementId) return t;
      const was = where(before, t.anchor.slideId, t.anchor.elementId);
      const now = where(deck, t.anchor.slideId, t.anchor.elementId);
      if (!was || !now || (was.left === now.left && was.top === now.top)) return t;
      moved = true;
      return { ...t, anchor: { ...t.anchor, x: t.anchor.x + now.left - was.left, y: t.anchor.y + now.top - was.top } };
    });
    if (moved) this.threads.set(next);
  }

  /** Leaving a slide leaves its thread being started. */
  slideChanged(): void {
    this.draft.set(null);
    if (!this.onSlide().some(t => t.id === this.activeId())) this.activeId.set(null);
  }
}

import { computed, signal } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { CommentDraft } from '../office/comments/comment-threads';
import { EditEvent, ReplyEvent } from '../office/comments/comments-panel.component';
import { TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { PageElement, TextCommentThread } from '../file-workspace/text-comments/text-comments.service';
import { ViewerCommentSurface } from '../file-workspace/text-comments/viewer-comment-surface';
import { PageBridgeAction, PageBridgeEvent, PageMark, PageProject } from './page-bridge';
import { anchorText, formatSourceRange, parseSourceRange } from './source-positions';

/** The mark id of the thread being started. */
const DRAFT = 'draft';
/** The project's scripts whose threads can be on the page's elements: JavaScript, and what is compiled to it (§4). */
const SCRIPT = /\.(m?js|jsx|tsx?|mts)$/i;

/** Another file of the project whose threads may be on the page's elements: a script. */
interface ScriptComments {
  file: FileAttachment;
  path: string;
  text: string;
  state: TextCommentsState;
}

/**
 * Comments on a rendered page's elements (ADR 0x050 §5), between the page's comment threads and the
 * bridge script in the page: a pick starts a thread, and the threads are shown back as marks.
 *
 * A thread lives with the file its code anchor is in. On an element the page's HTML declares, that
 * is the page's file, on the element's tag; on one a script made, the script's file, on the line
 * that made it (the call site the bridge recorded); on one with neither, the page's file, on the
 * element alone. So the page shows the threads of its own file and of the project's scripts that
 * name it, and the panel shows them together: this class routes each thread's actions to the file
 * it lives in (`stateFor` makes a script's state, which saves as soon as it changes).
 */
export class PageComments {
  /** Whether the page is waiting for an element to be picked. */
  readonly picking = signal(false);
  /** Threads with an anchor on the page whose element the page did not find, as it last said. */
  readonly unplaced = signal<ReadonlySet<string>>(new Set());
  /** The project's scripts whose comments are loaded, by file id. */
  private readonly scripts = signal<ReadonlyMap<string, ScriptComments>>(new Map());
  /** The state the thread being started is in. */
  private readonly draftIn = signal<TextCommentsState | null>(null);

  constructor(
    /** The page file's comments. */
    readonly state: TextCommentsState,
    /** Sends an action to the page's bridge. */
    private readonly send: (action: PageBridgeAction) => void,
    /** Opens the comments panel. */
    private readonly openPanel: () => void,
    /** Whether the user can start and change threads here. */
    private readonly canComment: () => boolean,
    /** The page's file. */
    private readonly page: () => FileAttachment,
    /** The project the page is served from (null for a page on its own). */
    private readonly project: () => PageProject | null = () => null,
    /** A comment state for another file of the project (a script). */
    private readonly stateFor: ((file: FileAttachment) => TextCommentsState) | null = null,
  ) {}

  /** The script threads on this page's elements, by script. */
  private readonly scriptThreads = computed(() => {
    const page = this.page().id;
    return [...this.scripts().values()].map(s => ({
      script: s,
      threads: s.state.threads().filter(t => t.element?.page === page),
    }));
  });

  /** The marks for the page: open threads' elements, and the active (or new) thread's more strongly. */
  readonly marks = computed<PageMark[]>(() => {
    const page = this.page().id;
    const activeId = this.activeId();
    const marks: PageMark[] = [];
    const mark = (
      id: string,
      state: PageMark['state'],
      at: { start: number; end: number } | null | undefined,
      element: PageElement | undefined,
      file = page,
    ) => {
      const onPage = element && element.page === page ? element : undefined;
      if (!at && !onPage) return;
      marks.push({
        id,
        state,
        ...(at ? { src: formatSourceRange({ file, ...at }) } : {}),
        ...(onPage ? { selector: onPage.selector, ...(onPage.fingerprint ? { fingerprint: onPage.fingerprint } : {}) } : {}),
      });
    };
    const positions = this.state.positions();
    for (const t of this.state.threads()) {
      if (t.resolved && t.id !== activeId) continue;
      mark(t.id, t.id === activeId ? 'active' : 'open', positions.get(t.id), t.element);
    }
    // A script's thread is found on the page by its code where the element carries it (JSX's, §4),
    // else by its element.
    for (const { script, threads } of this.scriptThreads()) {
      const at = script.state.positions();
      for (const t of threads) {
        if (t.resolved && t.id !== activeId) continue;
        mark(t.id, t.id === activeId ? 'active' : 'open', at.get(t.id), t.element, script.file.id);
      }
    }
    const drafting = this.drafting();
    const draft = drafting?.draft();
    if (drafting && draft) {
      const script = [...this.scripts().values()].find(s => s.state === drafting);
      mark(DRAFT, 'active', draft.range, draft.element, script ? script.file.id : page);
    }
    return marks;
  });

  // --- The panel: the page's threads and the scripts' threads on its elements, together ---------

  /** The threads for the panel: the page file's, then each script's, named after its file. */
  readonly shown = computed(() => [
    ...this.state.shown(),
    ...this.scriptThreads().flatMap(({ script, threads }) => {
      const ids = new Set(threads.map(t => t.id));
      return script.state
        .shown()
        .filter(t => ids.has(t.id))
        .map(t => ({ ...t, quote: `${script.path} · ${t.quote}` }));
    }),
  ]);
  readonly activeId = computed(
    () => this.state.activeId() ?? [...this.scripts().values()].map(s => s.state.activeId()).find(id => !!id) ?? null,
  );
  /** The state the thread being started is in: one a pick started, else the page's (a draft started on its text). */
  private readonly drafting = computed(() => this.draftIn() ?? (this.state.draft() ? this.state : null));
  readonly draftQuote = computed(() => this.drafting()?.draft()?.quote);
  readonly openCount = computed(() => this.shown().filter(t => !t.resolved).length);
  readonly people = computed(() => {
    const byId = new Map(this.state.people().map(p => [p.id, p]));
    for (const s of this.scripts().values()) for (const p of s.state.people()) if (!byId.has(p.id)) byId.set(p.id, p);
    return [...byId.values()];
  });
  readonly status = computed(() => this.state.status());

  /** Chooses a thread: its own file's state makes it active, the others none; the page shows it. */
  select(threadId: string): void {
    if (typeof threadId !== 'string') return; // a DOM select event (TextCommentsState.select)
    const owner = this.ownerOf(threadId);
    if (!owner) return;
    for (const s of this.states()) if (s !== owner) s.activeId.set(null);
    owner.select(threadId);
    this.draftIn.set(null);
    this.reveal(threadId);
  }

  create(draft: CommentDraft): void {
    const state = this.drafting();
    if (!state) return;
    this.draftIn.set(null);
    for (const s of this.states()) if (s !== state) s.activeId.set(null);
    state.create(draft);
  }

  cancelDraft(): void {
    this.drafting()?.cancelDraft();
    this.draftIn.set(null);
  }

  reply(event: ReplyEvent): void {
    this.ownerOf(event.threadId)?.reply(event);
  }

  edit(event: EditEvent): void {
    this.ownerOf(event.threadId)?.edit(event);
  }

  resolve(event: { threadId: string; resolved: boolean }): void {
    this.ownerOf(event.threadId)?.resolve(event);
  }

  remove(event: { threadId: string; commentId: string }): void {
    this.ownerOf(event.threadId)?.remove(event);
  }

  // --- The page --------------------------------------------------------------------------------

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
        // A page that (re)loaded has no marks and is not picking; its scripts may have changed.
        if (this.picking()) this.send({ type: 'pick' });
        this.send(this.marksAction());
        void this.loadScripts();
        break;
      case 'picked':
        this.picking.set(false);
        if (this.canComment()) void this.startOnPick(event);
        break;
      case 'pick-cancelled':
        this.picking.set(false);
        break;
      case 'select':
        if (event.id === DRAFT) return;
        this.select(event.id);
        this.openPanel();
        break;
      case 'menu':
        // The viewer shows the menu; nothing to do here.
        break;
      case 'unplaced':
        this.unplaced.set(new Set(event.ids.filter(id => id !== DRAFT)));
        break;
    }
  }

  /**
   * Starts a thread on a picked element, in the file its code is in: the page's, on the tag it
   * declares; a script's, on the JSX tag that made it (its source range, §4) or the line that
   * called the DOM (through the source map, for a compiled script); else the page's, on the
   * element alone.
   */
  private async startOnPick(event: Extract<PageBridgeEvent, { type: 'picked' }>): Promise<void> {
    const page = this.page().id;
    const element: PageElement = { page, selector: event.selector, fingerprint: event.fingerprint };
    const src = parseSourceRange(event.src);
    let state = this.state;
    let range: { start: number; end: number } | null = src && src.file === page ? { start: src.start, end: src.end } : null;
    if (!range && src) {
      const script = await this.scriptWithId(src.file);
      if (script && src.end <= script.text.length) {
        state = script.state;
        range = { start: src.start, end: src.end };
      }
    }
    if (!range && event.created) {
      const { path, line, column } = event.created;
      const script = await this.scriptAt(path);
      const at = script ? await this.project()!.sourceLine(path, line, column) : null;
      const lines = script && at ? lineRange(script.text, at) : null;
      if (script && lines) {
        state = script.state;
        range = lines;
      }
    }
    for (const s of this.states()) {
      if (s !== state) s.cancelDraft();
    }
    state.startOnElement(element, range, event.label);
    this.draftIn.set(state);
    this.openPanel();
  }

  /** Loads the comments of every script the page loaded that are not loaded yet. */
  private async loadScripts(): Promise<void> {
    const project = this.project();
    if (!project || !this.stateFor) return;
    for (const [path, file] of project.servedFiles()) {
      if (SCRIPT.test(path) && file.id !== this.page().id && !this.scripts().has(file.id)) await this.load(project, path, file);
    }
  }

  /** The script at a project path, its comments loaded. */
  private async scriptAt(path: string): Promise<ScriptComments | null> {
    const project = this.project();
    if (!project || !this.stateFor || !SCRIPT.test(path)) return null;
    const file = await project.fileAt(path);
    if (!file || file.id === this.page().id) return null;
    return this.scripts().get(file.id) ?? this.load(project, path, file);
  }

  /** The project script with this file id the page loaded, its comments loaded. */
  private async scriptWithId(id: string): Promise<ScriptComments | null> {
    const loaded = this.scripts().get(id);
    if (loaded) return loaded;
    for (const [path, file] of this.project()?.servedFiles() ?? []) if (file.id === id) return this.scriptAt(path);
    return null;
  }

  private async load(project: PageProject, path: string, file: FileAttachment): Promise<ScriptComments | null> {
    try {
      const text = anchorText(await project.text(file));
      const state = this.stateFor!(file);
      state.attach(new ViewerCommentSurface(text, () => null));
      await state.load(file.id);
      const script: ScriptComments = { file, path, text, state };
      // Loaded meanwhile by another request: keep the first.
      const kept = this.scripts().get(file.id);
      if (kept) return kept;
      this.scripts.update(all => new Map(all).set(file.id, script));
      return script;
    } catch {
      // A script whose comments cannot be read (too large, not text) has none here.
      return null;
    }
  }

  private states(): TextCommentsState[] {
    return [this.state, ...[...this.scripts().values()].map(s => s.state)];
  }

  private ownerOf(threadId: string): TextCommentsState | null {
    return this.states().find(s => s.threads().some((t: TextCommentThread) => t.id === threadId)) ?? null;
  }
}

/** A line of text as a range, without its indentation (1-based); null for a blank or missing line. */
export function lineRange(text: string, line: number): { start: number; end: number } | null {
  if (line < 1) return null;
  let start = 0;
  for (let n = 1; n < line; n++) {
    const next = text.indexOf('\n', start);
    if (next < 0) return null;
    start = next + 1;
  }
  let end = text.indexOf('\n', start);
  if (end < 0) end = text.length;
  if (text[end - 1] === '\r') end--;
  while (start < end && /\s/.test(text[start])) start++;
  while (end > start && /\s/.test(text[end - 1])) end--;
  return end > start ? { start, end } : null;
}

import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, signal } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { CommentPeopleService } from '../comments/comment-people.service';
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
import { CommentsPanelComponent, EditEvent, ReplyEvent } from '../comments/comments-panel.component';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { DocxCommentThread, newWordCommentId } from '../docx/docx-comments';
import { DocDocument } from '../docx/univer-doc-types';
import { DOCX_MIME, documentToDocx } from '../docx/docx-export';
import { docxToDocument } from '../docx/docx-import';
import { FidelityReport } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import { UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { DocCommentMarks, FUniver, UNIVER_DOCS_LOADER, UniverDocsKit } from '../univer/univer-loader';

const RENDER_ATTEMPTS = 40;
const RENDER_RETRY_MS = 100;
const SELECTION_OPERATION = 'doc.operation.set-selections';
const RICH_TEXT_MUTATION = 'doc.mutation.rich-text-editing';

interface Document extends UniverUnit<DocDocument> {
  getPermission?(): { setEditable(value?: boolean): Promise<void> | void };
}

/** A text range in one story of the document, as Univer reports a selection. */
interface TextSelection {
  segmentId: string;
  start: number;
  end: number;
}

interface SelectionEvent {
  id?: string;
  params?: { unitId?: string; segmentId?: string; ranges?: { startOffset: number; endOffset: number; collapsed?: boolean }[] };
}

/**
 * The document editor (ADR 0x043, office suite): Univer Docs over the document our docx
 * converter reads. Saving hands back a .docx written over the original package (patch mode), so
 * every part the editor does not model is kept as it was.
 *
 * Comments (ADR 0x045) are threads kept here and shown in the comments panel; Univer only marks
 * the text each is on, and moves the mark as the text is edited.
 */
@Component({
  selector: 'app-docs-editor',
  standalone: true,
  imports: [CommentsPanelComponent, FidelityNoticeComponent],
  templateUrl: './docs-editor.component.html',
  styleUrls: ['../univer/univer-editor.component.scss', './docs-editor.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DocsEditorComponent extends UniverEditorBase<DocDocument, Document> {
  private readonly load = inject(UNIVER_DOCS_LOADER);
  readonly noun = 'document';
  /** The bytes the open document came from; the exporter patches them rather than writing from scratch. */
  private original: ArrayBuffer | null = null;

  /** The margin corners and the lines of borderless table cells; off, the page looks as it prints. */
  readonly showGuides = signal(false);
  private docsKit: UniverDocsKit | null = null;
  private unitId: string | null = null;
  private timers: ReturnType<typeof setTimeout>[] = [];

  // --- comments ----------------------------------------------------------------------------------
  readonly me = inject(CommentPeopleService).me;
  readonly threads = signal<DocxCommentThread[]>([]);
  readonly activeThreadId = signal<string | null>(null);
  private readonly panel = inject(CommentsPanelState);
  /** The panel opens by itself on a document with open threads, until the user opens or closes it. */
  readonly commentsOpen = computed(() => this.panel.open() ?? this.openCount() > 0);
  /** The text a comment is being started on, until its first comment is posted. */
  readonly draft = signal<(TextSelection & { quote: string }) | null>(null);
  /** The text selected now, if any: what Comment would comment on. */
  readonly selection = signal<TextSelection | null>(null);
  /** Threads whose text is still in the document; deleting the text takes the comment with it. */
  private readonly marked = signal<ReadonlySet<string> | null>(null);
  private marks: DocCommentMarks | null = null;

  readonly shownThreads = computed(() => {
    const marked = this.marked();
    return marked ? this.threads().filter(t => marked.has(t.id)) : this.threads();
  });
  readonly openCount = computed(() => this.shownThreads().filter(t => !t.resolved).length);
  readonly people = computed<CommentPerson[]>(() => {
    const me = this.me();
    return [me, ...commentAuthors(this.threads()).filter(p => p.id !== me.id)];
  });
  readonly canComment = computed(() => {
    const selection = this.selection();
    return !this.readOnly() && !!selection && selection.end > selection.start;
  });

  constructor() {
    super();
    // Univer paints the active thread's text and leaves resolved threads unmarked.
    effect(() => {
      const activeId = this.activeThreadId();
      const hidden = new Set(
        this.threads()
          .filter(t => t.resolved && t.id !== activeId)
          .map(t => t.id),
      );
      this.marked();
      this.marks?.paint({ activeId, hidden });
    });
    // Captured, because Univer's own text input would take the keys first.
    inject<ElementRef<HTMLElement>>(ElementRef).nativeElement.addEventListener('keydown', event => this.onKeydown(event), {
      capture: true,
    });
  }

  protected async loadKit(): Promise<UniverKit> {
    const kit = await this.load(this.document);
    this.docsKit = kit;
    return kit;
  }

  toggleGuides(): void {
    this.showGuides.update(show => !show);
    this.applyGuides();
  }

  /** Univer renders the page shortly after the unit exists, so this tries until it can. */
  private applyGuides(attempt = 0): void {
    if (!this.univer || !this.unitId) return;
    const applied = this.docsKit?.setDocGuides?.(this.univer, this.unitId, this.showGuides() && !this.readOnly());
    if (!applied && attempt < RENDER_ATTEMPTS) this.timers.push(setTimeout(() => this.applyGuides(attempt + 1), RENDER_RETRY_MS));
  }

  /** Hooks the comment marks up once the page is rendered. */
  private attachMarks(attempt = 0): void {
    if (!this.univer || !this.unitId || this.marks) return;
    this.marks = this.docsKit?.commentMarks?.(this.univer, this.unitId) ?? null;
    if (!this.marks) {
      if (attempt < RENDER_ATTEMPTS) this.timers.push(setTimeout(() => this.attachMarks(attempt + 1), RENDER_RETRY_MS));
      return;
    }
    this.marked.set(new Set(this.marks.positions().keys()));
  }

  protected override onDispose(): void {
    this.timers.forEach(clearTimeout);
    this.timers = [];
    this.marks?.dispose();
    this.marks = null;
    this.marked.set(null);
    this.selection.set(null);
    this.unitId = null;
  }

  protected async parse(blob: Blob, file: FileAttachment): Promise<{ data: DocDocument; report: FidelityReport }> {
    const bytes = await blob.arrayBuffer();
    this.original = null;
    const result = await docxToDocument(bytes, { id: `doc-${file.id}`, name: file.name });
    this.original = bytes;
    this.threads.set(result.comments);
    this.draft.set(null);
    if (!result.comments.some(t => t.id === this.activeThreadId())) this.activeThreadId.set(null);
    return { data: result.document, report: result.report };
  }

  protected async createUnit(univerAPI: FUniver, data: DocDocument, readOnly: boolean): Promise<Document> {
    const api = univerAPI as FUniver & { createDocument(data: unknown): Document };
    // A disabled snapshot is read-only without the permission layer's "content is protected"
    // toasts on every click; the viewer stays quiet.
    const document = api.createDocument(readOnly ? { ...data, disabled: true } : data);
    this.unitId = data.id;
    this.applyGuides();
    this.attachMarks();
    univerAPI.addEvent(univerAPI.Event.CommandExecuted, (event: SelectionEvent) => {
      if (event.params?.unitId !== this.unitId) return;
      if (event.id === SELECTION_OPERATION) this.onSelection(event);
      else if (event.id === RICH_TEXT_MUTATION && this.marks) this.marked.set(new Set(this.marks.positions().keys()));
    });
    return document;
  }

  protected async serialize(data: DocDocument): Promise<Blob> {
    const { bytes, report } = await documentToDocx(data, this.original ?? undefined, { comments: this.threads() });
    this.exportReport.set(report);
    return new Blob([bytes as BlobPart], { type: DOCX_MIME });
  }

  // --- comments ----------------------------------------------------------------------------------

  private onSelection(event: SelectionEvent): void {
    const range = event.params?.ranges?.[0];
    if (!range) return;
    const segmentId = event.params?.segmentId ?? '';
    const selection = { segmentId, start: Math.min(range.startOffset, range.endOffset), end: Math.max(range.startOffset, range.endOffset) };
    this.selection.set(selection);
    // Clicking into commented text opens its thread.
    if (segmentId || !this.marks) return;
    const threadId = this.marks.at(selection.start);
    if (threadId && threadId !== this.activeThreadId()) {
      this.activeThreadId.set(threadId);
      this.panel.open.set(true);
    }
  }

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  /** Starts a thread on the selected text. */
  startComment(): void {
    const selection = this.selection();
    if (!this.canComment() || !selection || !this.marks) return;
    const quote = this.marks
      .text(selection.segmentId, selection.start, selection.end)
      .replace(/[\u0000-\u001f]/g, ' ')
      .replace(/\s+/g, ' ')
      .trim();
    this.draft.set({ ...selection, quote });
    this.activeThreadId.set(null);
    this.panel.open.set(true);
  }

  async createThread(draft: CommentDraft): Promise<void> {
    const at = this.draft();
    if (!at || !this.marks) return;
    const id = newWordCommentId();
    if (!(await this.marks.add(id, at.segmentId, at.start, at.end))) return;
    const thread: DocxCommentThread = {
      id,
      anchor: { segmentId: at.segmentId },
      quote: at.quote,
      comments: [newComment(id, this.me(), draft)],
    };
    this.threads.set(this.inTextOrder([...this.threads(), thread]));
    this.draft.set(null);
    this.activeThreadId.set(id);
    this.marked.set(new Set(this.marks.positions().keys()));
    this.contentChanged();
  }

  cancelDraft(): void {
    this.draft.set(null);
  }

  selectThread(threadId: string): void {
    this.draft.set(null);
    this.activeThreadId.set(threadId);
    this.marks?.reveal(threadId);
  }

  reply(event: ReplyEvent): void {
    this.threads.update(threads => addReply(threads, event.threadId, newComment(newWordCommentId(), this.me(), event.draft)));
    this.contentChanged();
  }

  edit(event: EditEvent): void {
    this.threads.update(threads => editComment(threads, event.threadId, event.commentId, event.draft));
    this.contentChanged();
  }

  resolve(event: { threadId: string; resolved: boolean }): void {
    this.threads.update(threads => setResolved(threads, event.threadId, event.resolved));
    this.contentChanged();
  }

  async remove(event: { threadId: string; commentId: string }): Promise<void> {
    const thread = this.threads().find(t => t.id === event.threadId);
    if (!thread) return;
    if (event.threadId === event.commentId) {
      await this.marks?.remove(thread.id, thread.anchor.segmentId);
      if (this.activeThreadId() === thread.id) this.activeThreadId.set(null);
    }
    this.threads.update(threads => removeComment(threads, event.threadId, event.commentId));
    this.contentChanged();
  }

  /** Threads in the order their text appears; ones with no mark (a header's) keep their place at the end. */
  private inTextOrder(threads: DocxCommentThread[]): DocxCommentThread[] {
    const positions = this.marks?.positions() ?? new Map<string, number>();
    const rank = (t: DocxCommentThread) =>
      t.anchor.segmentId ? Number.MAX_SAFE_INTEGER : (positions.get(t.id) ?? Number.MAX_SAFE_INTEGER);
    return threads
      .map((t, i) => ({ t, i }))
      .sort((a, b) => rank(a.t) - rank(b.t) || a.i - b.i)
      .map(x => x.t);
  }

  /** Ctrl+Alt+M comments on the selection, as in Word and Google Docs. */
  private onKeydown(event: KeyboardEvent): void {
    if ((event.ctrlKey || event.metaKey) && event.altKey && event.key.toLowerCase() === 'm') {
      event.preventDefault();
      event.stopPropagation();
      this.startComment();
    }
  }
}

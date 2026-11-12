import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterRenderEffect,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { renderLatexMath } from '../../../core/utils/latex-math.helpers';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';
import { CommentPeopleService } from '../../office/comments/comment-people.service';
import { CommentsPanelComponent } from '../../office/comments/comments-panel.component';
import { CommentsPanelState } from '../../office/comments/comments-panel-state.service';
import { COMMENT_SAVER } from '../editors/file-editor';
import { TextCommentsState } from '../text-comments/text-comments-state';
import { TextCommentsService } from '../text-comments/text-comments.service';
import { RenderedTextMap } from '../text-comments/rendered-text-map';
import { CommentTextMap, TextDomMap } from '../text-comments/text-dom-map';
import { ViewerCommentSurface } from '../text-comments/viewer-comment-surface';
import { MONACO_LOADER, monacoLanguageFor } from '../editors/monaco/monaco-loader';
import { appTheme, monacoThemeFor } from '../editors/monaco/monaco-themes';
import {
  csvDelimiterFor,
  DecodedText,
  decodeTextPreview,
  FilePreviewKind,
  MAX_MARKDOWN_RENDER_CHARS,
  parseCsv,
  previewKindFor,
} from '../../gallery/helpers/file-preview.helpers';

/** Code longer than this is shown plain: tokenizing it would stall the viewer. */
const MAX_HIGHLIGHT_CHARS = 200_000;

/**
 * Text, code, Markdown and CSV, read-only (ADR 0x043 §2): Markdown and CSV rendered with the raw
 * text a click away, everything else as text and never as markup. The Monaco editor replaces the
 * raw view in a later step.
 */
@Component({
  selector: 'app-text-viewer',
  standalone: true,
  imports: [MarkdownModule, CheckboxComponent, CommentsPanelComponent],
  templateUrl: './text-viewer.component.html',
  styleUrl: './text-viewer.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TextViewerComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly document = inject(DOCUMENT);
  private readonly loadMonaco = inject(MONACO_LOADER);

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  readonly kind = computed<FilePreviewKind>(() => previewKindFor(this.file()));
  readonly decoded = signal<DecodedText | null>(null);
  readonly failed = signal(false);
  /** Markdown and CSV: the rendered view (true) or the raw text. */
  readonly rendered = signal(true);
  readonly wrapLines = signal(true);

  readonly text = computed(() => this.decoded()?.text ?? '');
  /**
   * Code with a known language, highlighted by Monaco's tokenizer into static markup (ADR 0x043
   * §2): the phone's read-only view, and the raw view everywhere, without an editor instance.
   * Null until it is ready or when the language is unknown; the plain text shows meanwhile.
   */
  readonly highlighted = signal<string | null>(null);
  readonly markdownTooBig = computed(() => this.text().length > MAX_MARKDOWN_RENDER_CHARS);
  readonly csv = computed(() => (this.kind() === 'csv' && this.decoded() ? parseCsv(this.text(), csvDelimiterFor(this.file())) : null));
  readonly canRender = computed(() => this.kind() === 'csv' || (this.kind() === 'markdown' && !this.markdownTooBig()));
  readonly showRendered = computed(() => this.canRender() && this.rendered());

  private generation = 0;

  // --- comments (ADR 0x045): on the text, kept beside the file, written as they are made --------
  private readonly saver = inject(COMMENT_SAVER, { optional: true });
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new TextCommentsState(inject(TextCommentsService), this.me, () => this.comments.autosave());
  private readonly panel = inject(CommentsPanelState);
  /** Text and Markdown carry comments; a file shown only in part (over 1 MB) does not. */
  readonly hasComments = computed(() => {
    const data = this.decoded();
    return (this.kind() === 'text' || this.kind() === 'markdown') && !!data && !data.binary && !data.truncated;
  });
  /** Whether comments can be written here (the user may change the file), or only read. */
  readonly canComment = computed(() => this.hasComments() && !!this.saver?.canComment());
  readonly commentsOpen = computed(() => this.hasComments() && (this.panel.open() ?? this.comments.openCount() > 0));
  readonly replyRequest = signal<{ threadId: string } | null>(null);
  /** The right-click menu over the text: where it opened, and on what. */
  readonly contextMenu = signal<{
    left: number;
    top: number;
    threadId: string | null;
    range: { start: number; end: number } | null;
  } | null>(null);
  private surface: ViewerCommentSurface | null = null;
  private map: CommentTextMap | null = null;
  /** A thread chosen while the text was shown formatted, to show once the raw text is. */
  private revealAfterRender: string | null = null;

  constructor() {
    // The raw text's map is made whenever it is rendered; coloured text that does not line up
    // with the file falls back to the plain text, which always does.
    afterRenderEffect(() => {
      const html = this.highlighted();
      this.showRendered();
      this.text();
      this.commentsOpen();
      // Formatted Markdown is mapped once it has rendered (onMarkdownReady).
      if (this.showRendered()) return;
      const pre = this.host.nativeElement.querySelector<HTMLElement>('pre.text-viewer__text');
      this.map = pre && this.hasComments() ? TextDomMap.build(pre, this.text()) : null;
      if (pre && !this.map && html && this.hasComments()) {
        untracked(() => this.highlighted.set(null));
        return;
      }
      this.surface?.repaint();
      const pending = this.revealAfterRender;
      if (pending && this.map) {
        this.revealAfterRender = null;
        untracked(() => this.comments.select(pending));
      }
    });
    effect(() => {
      const state = this.comments.paintState();
      untracked(() => this.surface?.paint(state));
    });
    inject(DestroyRef).onDestroy(() => this.surface?.dispose());
    effect(() => {
      const blob = this.blob();
      const generation = ++this.generation;
      untracked(() => {
        this.decoded.set(null);
        this.failed.set(false);
        this.rendered.set(true);
      });
      void this.decode(blob, generation);
    });
  }

  onMarkdownReady(): void {
    void renderLatexMath(this.host.nativeElement);
    // Comment anchors over the formatted text, found by their words.
    const markdown = this.host.nativeElement.querySelector<HTMLElement>('.text-viewer__markdown');
    if (markdown && this.hasComments() && this.showRendered()) {
      this.map = new RenderedTextMap(markdown, this.text());
      this.surface?.repaint();
    }
  }

  private async decode(blob: Blob, generation: number): Promise<void> {
    try {
      const decoded = decodeTextPreview(new Uint8Array(await blob.arrayBuffer()));
      if (generation !== this.generation) return;
      this.decoded.set(decoded);
      this.highlighted.set(null);
      void this.loadComments(decoded.text, generation);
      if (this.kind() === 'text' && !decoded.binary && decoded.text.length > 0 && decoded.text.length <= MAX_HIGHLIGHT_CHARS) {
        void this.highlight(decoded.text, generation);
      }
    } catch {
      if (generation === this.generation) this.failed.set(true);
    }
  }

  private async loadComments(text: string, generation: number): Promise<void> {
    this.surface?.dispose();
    this.surface = null;
    if (!this.hasComments()) return;
    this.surface = new ViewerCommentSurface(text, () => this.map);
    this.comments.attach(this.surface);
    try {
      await this.comments.load(this.file().id);
    } catch {
      // A file the server cannot keep comments for (not text, too large) has none.
    }
    if (generation !== this.generation) return;
  }

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  /** The text selected in the raw view, or the word at the caret, as a range of the file's text. */
  private selectedRange(): { start: number; end: number } | null {
    const selection = this.document.getSelection();
    const map = this.map;
    if (!selection?.rangeCount || !map) return null;
    const range = selection.getRangeAt(0);
    const shown = this.host.nativeElement.querySelector(this.showRendered() ? '.text-viewer__markdown' : 'pre.text-viewer__text');
    if (!shown?.contains(range.commonAncestorContainer)) return null;
    if (!range.collapsed) return map.sourceRange(range);
    const at = map.offsetOf(range.startContainer, range.startOffset);
    return at === null ? null : this.wordAt(at);
  }

  private wordAt(offset: number): { start: number; end: number } | null {
    const text = this.text();
    const word = /[\p{L}\p{N}_]/u;
    let at = word.test(text[offset] ?? '') ? offset : word.test(text[offset - 1] ?? '') ? offset - 1 : -1;
    if (at < 0) return null;
    let start = at;
    while (start > 0 && word.test(text[start - 1])) start--;
    while (at < text.length && word.test(text[at])) at++;
    return { start, end: at };
  }

  /**
   * Starts a thread on the selection. In formatted Markdown the selection's words are found in the
   * source; when they cannot be, the raw text is shown to select in instead.
   */
  startComment(range = this.selectedRange()): void {
    if (!this.canComment()) return;
    this.panel.open.set(true);
    if (range) {
      this.comments.start(range);
      return;
    }
    if (this.showRendered()) this.rendered.set(false);
  }

  /** A thread chosen in the panel: shown where its words are, or in the raw text when the formatting hides them. */
  selectThread(threadId: string): void {
    const at = this.comments.positions().get(threadId);
    if (this.showRendered() && at && !this.map?.range(at.start, at.end)) {
      this.revealAfterRender = threadId;
      this.rendered.set(false);
      return;
    }
    this.comments.select(threadId);
  }

  /** A click in the raw text: on commented text, its thread opens. */
  onTextClick(event: MouseEvent): void {
    const offset = this.offsetAt(event);
    if (offset !== null) this.comments.offsetChosen(offset);
  }

  onTextContextMenu(event: MouseEvent): void {
    if (!this.canComment() || !this.map) return;
    event.preventDefault();
    const box = this.host.nativeElement.getBoundingClientRect();
    const selected = this.selectedRange();
    const offset = this.offsetAt(event);
    const threadId = offset !== null && !selected ? this.comments.threadAt(offset) : null;
    const range = selected ?? (offset !== null ? this.wordAt(offset) : null);
    this.contextMenu.set({ left: event.clientX - box.left, top: event.clientY - box.top, threadId, range });
  }

  menuNewComment(): void {
    const menu = this.contextMenu();
    this.contextMenu.set(null);
    if (menu?.range) this.startComment(menu.range);
  }

  menuReply(): void {
    const threadId = this.contextMenu()?.threadId;
    this.contextMenu.set(null);
    if (!threadId) return;
    this.comments.select(threadId);
    this.panel.open.set(true);
    this.replyRequest.set({ threadId });
  }

  menuDelete(): void {
    const threadId = this.contextMenu()?.threadId;
    this.contextMenu.set(null);
    if (threadId) this.comments.remove({ threadId, commentId: threadId });
  }

  private offsetAt(event: MouseEvent): number | null {
    const doc = this.document as Document & { caretRangeFromPoint?(x: number, y: number): Range | null };
    const caret = doc.caretRangeFromPoint?.(event.clientX, event.clientY);
    return caret && this.map ? this.map.offsetOf(caret.startContainer, caret.startOffset) : null;
  }

  private async highlight(text: string, generation: number): Promise<void> {
    try {
      const monaco = await this.loadMonaco(this.document);
      const language = monacoLanguageFor(monaco, this.file().name);
      if (language === 'plaintext' || generation !== this.generation) return;
      // colorize paints with the active theme; follow the app's.
      monaco.editor.setTheme(monacoThemeFor(appTheme(this.document)));
      const html = await monaco.editor.colorize(text, language, { tabSize: 4 });
      // Monaco escapes the text and emits only spans with token classes; nothing from the file
      // reaches the DOM as markup.
      // Monaco's colorize output is spans with classes, which Angular's sanitizer keeps as they are,
      // so it goes through the sanitizer rather than around it.
      if (generation === this.generation) this.highlighted.set(html);
    } catch {
      // Highlighting is a nicety; the plain text is already shown.
    }
  }
}

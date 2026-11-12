import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { renderLatexMath } from '../../../core/utils/latex-math.helpers';
import { previewKindFor } from '../../gallery/helpers/file-preview.helpers';
import { FileEditor } from './file-editor';
import { CommentPeopleService } from '../../office/comments/comment-people.service';
import { CommentsPanelComponent } from '../../office/comments/comments-panel.component';
import { CommentsPanelState } from '../../office/comments/comments-panel-state.service';
import { MonacoCommentSurface } from '../text-comments/monaco-comment-surface';
import { PreviewCommentHighlights } from '../text-comments/preview-comment-highlights';
import { TextCommentsState } from '../text-comments/text-comments-state';
import { TextCommentThread, TextCommentsService } from '../text-comments/text-comments.service';
import { MONACO_LOADER, type Monaco, monacoLanguageFor, vfsUriFor } from './monaco/monaco-loader';
import { appTheme, followAppTheme, monacoThemeFor } from './monaco/monaco-themes';

const PREVIEW_DEBOUNCE_MS = 250;

/**
 * The code and Markdown editor (ADR 0x043 §2): Monaco over the file's text, with the language
 * picked from the file name and a rendered preview beside Markdown. The host owns saving; this
 * reports dirtiness, hands over the content, and asks to save on Ctrl+S.
 */
@Component({
  selector: 'app-code-editor',
  standalone: true,
  imports: [MarkdownModule, CommentsPanelComponent],
  templateUrl: './code-editor.component.html',
  styleUrl: './code-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CodeEditorComponent implements FileEditor {
  private readonly document = inject(DOCUMENT);
  private readonly load = inject(MONACO_LOADER);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly editorHost = viewChild.required<ElementRef<HTMLElement>>('editor');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(false);
  /** Comments can be started where the text can be changed and the server keeps comments for the file. */
  readonly canStartComment = computed(() => !this.readOnly() && this.comments.available());

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly language = signal('plaintext');
  readonly isMarkdown = computed(() => previewKindFor(this.file()) === 'markdown');
  readonly showPreview = signal(true);

  // --- comments (ADR 0x045): kept beside the file, written once a save has stored the text ---------
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new TextCommentsState(inject(TextCommentsService), this.me, () => this.commentsChanged());
  private readonly panel = inject(CommentsPanelState);
  /** The panel opens by itself on a file with open threads, until the user opens or closes it. */
  readonly commentsOpen = computed(() => this.panel.open() ?? this.comments.openCount() > 0);
  /** Opens a thread's reply box in the panel (the right-click menu's "Reply to comment"). */
  readonly replyRequest = signal<{ threadId: string } | null>(null);
  /** Comments changed since they were last written. */
  private commentsDirty = false;
  private surface: MonacoCommentSurface | null = null;
  /** The threads shown over the Markdown preview, by their words. */
  private readonly previewComments = new PreviewCommentHighlights();
  /** The threads as they were when each blob handed to the host was made, anchored on its text. */
  private readonly handedOutComments = new WeakMap<Blob, TextCommentThread[]>();
  private refreshTimer: ReturnType<typeof setTimeout> | null = null;
  private onCommentKey: import('monaco-editor').editor.IContextKey<boolean> | null = null;
  readonly previewText = signal('');

  private monaco: Monaco | null = null;
  private editor: import('monaco-editor').editor.IStandaloneCodeEditor | null = null;
  private model: import('monaco-editor').editor.ITextModel | null = null;
  private baseline = '';
  /** The text behind each blob handed to the host, and the latest one. */
  private readonly handedOut = new WeakMap<Blob, string>();
  private lastContent: Blob | null = null;
  private previewTimer: ReturnType<typeof setTimeout> | null = null;
  private rendered = false;
  private generation = 0;

  constructor() {
    afterNextRender(() => {
      this.rendered = true;
      void this.mount();
    });
    effect(() => {
      // Only new bytes reload the text. A rename or move changes file() and must not touch edits.
      const blob = this.blob();
      if (!this.rendered) return;
      // After a save the host hands back this editor's own bytes; reloading them would discard
      // what was typed since, and the undo history and cursor with it.
      if (blob === this.lastContent) return;
      untracked(() => void this.loadText(blob));
    });
    // The editor and the preview mark open threads' text, the active one more strongly.
    effect(() => {
      const state = this.comments.paintState();
      this.comments.positions();
      this.showPreview();
      untracked(() => {
        this.surface?.paint(state);
        this.paintPreview();
      });
    });
    const stopTheme = followAppTheme(this.document, theme => this.monaco?.editor.setTheme(theme));
    inject(DestroyRef).onDestroy(stopTheme);
    effect(() => {
      const readOnly = this.readOnly();
      untracked(() => this.editor?.updateOptions({ readOnly }));
    });
    inject(DestroyRef).onDestroy(() => this.teardown());
  }

  async content(): Promise<Blob> {
    const text = this.model?.getValue() ?? this.baseline;
    const blob = new Blob([text], { type: this.file().file_type || 'text/plain' });
    this.handedOut.set(blob, text);
    this.handedOutComments.set(blob, this.comments.snapshot());
    this.lastContent = blob;
    return blob;
  }

  markSaved(saved?: Blob): void {
    const current = this.model?.getValue() ?? this.baseline;
    // What was saved, which is not the current text if the user kept typing during the save.
    this.baseline = (saved && this.handedOut.get(saved)) ?? current;
    this.setDirty(current !== this.baseline || this.commentsDirty);
  }

  /** Writes the comments placed on what the save stored (FileEditor.saved). */
  async saved(file: FileAttachment, content: Blob): Promise<void> {
    const threads = this.handedOutComments.get(content) ?? this.comments.snapshot();
    // A file that never had comments has nothing to write.
    if (!threads.length && !this.hadComments) return;
    await this.comments.save(file.content_version ?? this.comments.placedOn, threads);
    this.hadComments = threads.length > 0;
    // Comments changed while the save ran (a reply posted meanwhile) are still unsaved.
    this.commentsDirty = this.comments.unsaved();
    this.setDirty((this.model?.getValue() ?? this.baseline) !== this.baseline || this.commentsDirty);
  }

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  /** Starts a thread on the selection (in the editor or the preview), or on the word at the caret. */
  startComment(): void {
    if (!this.canStartComment()) return;
    const range = this.previewSelection() ?? this.surface?.selectionOrWord();
    if (!range) return;
    this.comments.start(range);
    this.panel.open.set(true);
  }

  selectThread(threadId: string): void {
    this.comments.select(threadId);
    const at = this.comments.positions().get(threadId);
    if (at) this.previewComments.reveal(at);
  }

  /** A click in the preview on a thread's text makes it active, and shows it in the editor. */
  onPreviewClick(event: MouseEvent): void {
    const doc = this.document as Document & { caretRangeFromPoint?(x: number, y: number): Range | null };
    const caret = doc.caretRangeFromPoint?.(event.clientX, event.clientY);
    const offset = caret ? this.previewComments.offsetOf(caret.startContainer, caret.startOffset) : null;
    const threadId = offset !== null ? this.comments.threadAt(offset) : null;
    if (threadId) this.comments.select(threadId);
  }

  /** What is selected in the preview, as a range of the text. */
  private previewSelection(): { start: number; end: number } | null {
    const selection = this.document.getSelection();
    const preview = this.previewRoot();
    if (!selection?.rangeCount || selection.isCollapsed || !preview) return null;
    const range = selection.getRangeAt(0);
    if (!preview.contains(range.commonAncestorContainer)) return null;
    return this.previewComments.sourceRange(range);
  }

  private previewRoot(): HTMLElement | null {
    return this.isMarkdown() && this.showPreview() ? this.host.nativeElement.querySelector<HTMLElement>('.code-editor__markdown') : null;
  }

  private paintPreview(): void {
    this.previewComments.paint(this.previewRoot(), this.model?.getValue() ?? '', this.comments.positions(), this.comments.paintState());
  }

  private replyAtCaret(): void {
    const threadId = this.threadAtCaret();
    if (!threadId) return;
    this.comments.select(threadId);
    this.panel.open.set(true);
    this.replyRequest.set({ threadId });
  }

  private deleteAtCaret(): void {
    const threadId = this.threadAtCaret();
    if (threadId) this.comments.remove({ threadId, commentId: threadId });
  }

  private threadAtCaret(): string | null {
    const position = this.editor?.getPosition();
    if (!position || !this.model) return null;
    return this.comments.threadAt(this.model.getOffsetAt(position));
  }

  private commentsChanged(): void {
    this.commentsDirty = true;
    this.setDirty(true);
  }

  /** Whether the file has threads stored, so a save that removed the last one still writes. */
  private hadComments = false;

  /** Hooks comments up to the editor: marks, the caret, the right-click menu and Ctrl+Alt+M. */
  private attachComments(monaco: Monaco): void {
    const editor = this.editor;
    const model = this.model;
    if (!editor || !model) return;
    this.surface?.dispose();
    this.surface = new MonacoCommentSurface(monaco, editor, model);
    this.comments.attach(this.surface);
    if (this.onCommentKey) return;
    this.onCommentKey = editor.createContextKey('whatiffOnComment', false);
    editor.onDidChangeCursorSelection(event => {
      const offset = model.getOffsetAt(event.selection.getPosition());
      this.onCommentKey?.set(!!this.comments.threadAt(offset));
      // Only the user moving the caret picks a thread; edits and reveals move it too.
      if (event.source === 'mouse' || event.source === 'keyboard') this.comments.offsetChosen(offset);
    });
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyMod.Alt | monaco.KeyCode.KeyM, () => this.startComment());
    const menu = (id: string, label: string, order: number, precondition: string, run: () => void) =>
      editor.addAction({ id, label, contextMenuGroupId: '0_whatiff_comments', contextMenuOrder: order, precondition, run });
    menu('whatiff.text.comment.new', 'New comment', 1, '!whatiffOnComment', () => this.startComment());
    menu('whatiff.text.comment.reply', 'Reply to comment', 1, 'whatiffOnComment', () => this.replyAtCaret());
    menu('whatiff.text.comment.delete', 'Delete comment', 2, 'whatiffOnComment', () => this.deleteAtCaret());
  }

  togglePreview(): void {
    this.showPreview.update(v => !v);
  }

  onMarkdownReady(): void {
    void renderLatexMath(this.host.nativeElement);
    this.paintPreview();
  }

  private async mount(): Promise<void> {
    try {
      this.monaco = await this.load(this.document);
    } catch {
      this.state.set('error');
      return;
    }
    await this.loadText(this.blob());
  }

  private async loadText(blob: Blob): Promise<void> {
    const generation = ++this.generation;
    const monaco = this.monaco;
    if (!monaco) return;
    let text: string;
    try {
      text = await blob.text();
    } catch {
      if (generation === this.generation) this.state.set('error');
      return;
    }
    if (generation !== this.generation) return;
    const file = this.file();
    const language = monacoLanguageFor(monaco, file.name);
    this.language.set(language);
    const uri = vfsUriFor(monaco, file);
    if (!this.model || this.model.uri.toString() !== uri.toString()) {
      this.model?.dispose();
      this.model = monaco.editor.getModel(uri) ?? monaco.editor.createModel(text, language, uri);
      this.model.setValue(text);
      this.model.onDidChangeContent(() => this.onContentChanged());
      if (!this.editor) {
        this.editor = monaco.editor.create(this.editorHost().nativeElement, {
          model: this.model,
          theme: monacoThemeFor(appTheme(this.document)),
          automaticLayout: true,
          readOnly: this.readOnly(),
          minimap: { enabled: false },
          wordWrap: language === 'markdown' || language === 'plaintext' ? 'on' : 'off',
          scrollBeyondLastLine: false,
        });
        this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => this.saveRequested.emit());
      } else {
        this.editor.setModel(this.model);
      }
    } else {
      this.model.setValue(text);
    }
    this.baseline = text;
    this.commentsDirty = false;
    this.setDirty(false);
    this.previewText.set(text);
    this.state.set('ready');
    this.attachComments(monaco);
    try {
      await this.comments.load(file.id);
      this.hadComments = this.comments.threads().length > 0;
    } catch {
      // A file the server cannot keep comments for (not text, too large) has none.
    }
  }

  private onContentChanged(): void {
    const value = this.model?.getValue() ?? '';
    this.setDirty(value !== this.baseline || this.commentsDirty);
    // Where the threads are is read once typing pauses.
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.refreshTimer = setTimeout(() => this.comments.refresh(), 150);
    if (this.isMarkdown()) {
      if (this.previewTimer) clearTimeout(this.previewTimer);
      this.previewTimer = setTimeout(() => this.previewText.set(value), PREVIEW_DEBOUNCE_MS);
    }
  }

  private setDirty(dirty: boolean): void {
    if (this.dirty() === dirty) return;
    this.dirty.set(dirty);
    this.dirtyChange.emit(dirty);
  }

  private teardown(): void {
    this.generation += 1;
    if (this.previewTimer) clearTimeout(this.previewTimer);
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.comments.attach(null);
    this.previewComments.dispose();
    this.surface = null;
    this.editor?.dispose();
    this.model?.dispose();
    this.editor = null;
    this.model = null;
  }
}

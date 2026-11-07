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
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { MarkdownModule } from 'ngx-markdown';

import { renderLatexMath } from '../../core/utils/latex-math.helpers';
import { MONACO_LOADER, type Monaco } from '../file-workspace/editors/monaco/monaco-loader';
import { appTheme, followAppTheme, monacoThemeFor } from '../file-workspace/editors/monaco/monaco-themes';
import { NotebookCell, resolveAttachments } from './nbformat';
import { NotebookOutputComponent } from './notebook-output.component';

/** Colouring is a nicety; a huge cell stays plain. */
const MAX_COLORIZE_CHARS = 100_000;
const MIN_EDITOR_HEIGHT_PX = 38;

/**
 * One cell. At rest it is rendered Markdown or colourized code, which is cheap and works on a phone;
 * only the cell being edited holds a Monaco editor, so a long notebook does not hold hundreds of them.
 */
@Component({
  selector: 'app-notebook-cell',
  standalone: true,
  imports: [MarkdownModule, NotebookOutputComponent],
  templateUrl: './notebook-cell.component.html',
  styleUrl: './notebook-cell.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class NotebookCellComponent {
  private readonly document = inject(DOCUMENT);
  private readonly loadMonaco = inject(MONACO_LOADER);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly editorHost = viewChild<ElementRef<HTMLElement>>('editor');

  readonly cell = input.required<NotebookCell>();
  /** A Monaco language id. */
  readonly language = input('python');
  readonly active = input(false);
  readonly editing = input(false);
  readonly readOnly = input(true);

  readonly sourceChange = output<string>();
  readonly saveRequested = output<void>();
  readonly runNext = output<void>();
  readonly stopEditing = output<void>();

  readonly highlighted = signal<SafeHtml | null>(null);
  readonly markdown = computed(() => resolveAttachments(this.cell()));
  readonly prompt = computed(() => {
    const count = this.cell().executionCount;
    return count === null ? '[ ]' : `[${count}]`;
  });

  private editor: import('monaco-editor').editor.IStandaloneCodeEditor | null = null;
  private model: import('monaco-editor').editor.ITextModel | null = null;
  private monaco: Monaco | null = null;
  private colorizeRun = 0;

  constructor() {
    effect(() => {
      const cell = this.cell();
      const language = this.language();
      const editing = this.editing();
      untracked(() => {
        if (cell.type === 'code' && !editing) void this.colorize(cell.source, language);
      });
    });
    afterRenderEffect(() => {
      const target = this.editorHost()?.nativeElement ?? null;
      untracked(() => {
        if (target && !this.editor) void this.mountEditor(target);
        else if (!target && this.editor) this.unmountEditor();
      });
    });
    // Undo can replace the source while the cell is being edited.
    effect(() => {
      const source = this.cell().source;
      untracked(() => {
        if (this.model && this.model.getValue() !== source) this.model.setValue(source);
      });
    });
    const stopTheme = followAppTheme(this.document, theme => this.monaco?.editor.setTheme(theme));
    const destroy = inject(DestroyRef);
    destroy.onDestroy(stopTheme);
    destroy.onDestroy(() => {
      this.colorizeRun += 1;
      this.unmountEditor();
    });
  }

  onMarkdownReady(): void {
    void renderLatexMath(this.host.nativeElement);
  }

  private async colorize(source: string, language: string): Promise<void> {
    const run = ++this.colorizeRun;
    this.highlighted.set(null);
    if (!source || source.length > MAX_COLORIZE_CHARS) return;
    try {
      const monaco = await this.loadMonaco(this.document);
      monaco.editor.setTheme(monacoThemeFor(appTheme(this.document)));
      const html = await monaco.editor.colorize(source, language, { tabSize: 4 });
      // Monaco escapes the text and emits only spans with token classes.
      if (run === this.colorizeRun) this.highlighted.set(this.sanitizer.bypassSecurityTrustHtml(html));
    } catch {
      // The plain source is already shown.
    }
  }

  private async mountEditor(target: HTMLElement): Promise<void> {
    let monaco: Monaco;
    try {
      monaco = await this.loadMonaco(this.document);
    } catch {
      return;
    }
    // The cell may have stopped being edited while Monaco loaded.
    if (this.editor || !target.isConnected) return;
    this.monaco = monaco;
    const cell = this.cell();
    const language = cell.type === 'markdown' ? 'markdown' : cell.type === 'code' ? this.language() : 'plaintext';
    this.model = monaco.editor.createModel(cell.source, language, monaco.Uri.parse(`vfs:///notebook-cell/${cell.uid}`));
    this.model.onDidChangeContent(() => this.sourceChange.emit(this.model?.getValue() ?? ''));
    const editor = monaco.editor.create(target, {
      model: this.model,
      theme: monacoThemeFor(appTheme(this.document)),
      automaticLayout: true,
      minimap: { enabled: false },
      lineNumbers: 'off',
      folding: false,
      glyphMargin: false,
      lineDecorationsWidth: 8,
      overviewRulerLanes: 0,
      renderLineHighlight: 'none',
      scrollBeyondLastLine: false,
      scrollbar: { vertical: 'hidden', alwaysConsumeMouseWheel: false },
      wordWrap: cell.type === 'code' ? 'off' : 'on',
    });
    this.editor = editor;
    const fit = () => {
      const height = Math.max(MIN_EDITOR_HEIGHT_PX, editor.getContentHeight());
      target.style.height = `${height}px`;
      editor.layout();
    };
    editor.onDidContentSizeChange(fit);
    fit();
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => this.saveRequested.emit());
    editor.addCommand(monaco.KeyMod.Shift | monaco.KeyCode.Enter, () => this.runNext.emit());
    editor.addCommand(monaco.KeyCode.Escape, () => this.stopEditing.emit());
    editor.focus();
  }

  private unmountEditor(): void {
    this.editor?.dispose();
    this.model?.dispose();
    this.editor = null;
    this.model = null;
  }
}

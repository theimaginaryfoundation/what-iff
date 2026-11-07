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
  imports: [MarkdownModule],
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

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly language = signal('plaintext');
  readonly isMarkdown = computed(() => previewKindFor(this.file()) === 'markdown');
  readonly showPreview = signal(true);
  readonly previewText = signal('');

  private monaco: Monaco | null = null;
  private editor: import('monaco-editor').editor.IStandaloneCodeEditor | null = null;
  private model: import('monaco-editor').editor.ITextModel | null = null;
  private baseline = '';
  private previewTimer: ReturnType<typeof setTimeout> | null = null;
  private rendered = false;
  private generation = 0;

  constructor() {
    afterNextRender(() => {
      this.rendered = true;
      void this.mount();
    });
    effect(() => {
      const blob = this.blob();
      this.file();
      if (!this.rendered) return;
      untracked(() => void this.loadText(blob));
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
    return new Blob([text], { type: this.file().file_type || 'text/plain' });
  }

  markSaved(): void {
    this.baseline = this.model?.getValue() ?? this.baseline;
    this.setDirty(false);
  }

  togglePreview(): void {
    this.showPreview.update(v => !v);
  }

  onMarkdownReady(): void {
    void renderLatexMath(this.host.nativeElement);
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
    this.setDirty(false);
    this.previewText.set(text);
    this.state.set('ready');
  }

  private onContentChanged(): void {
    const value = this.model?.getValue() ?? '';
    this.setDirty(value !== this.baseline);
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
    this.editor?.dispose();
    this.model?.dispose();
    this.editor = null;
    this.model = null;
  }
}

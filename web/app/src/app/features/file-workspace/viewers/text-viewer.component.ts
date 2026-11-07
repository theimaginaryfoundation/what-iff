import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { renderLatexMath } from '../../../core/utils/latex-math.helpers';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';
import { MONACO_LOADER, monacoLanguageFor } from '../editors/monaco/monaco-loader';
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
  imports: [MarkdownModule, CheckboxComponent],
  templateUrl: './text-viewer.component.html',
  styleUrl: './text-viewer.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TextViewerComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly document = inject(DOCUMENT);
  private readonly sanitizer = inject(DomSanitizer);
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
  readonly highlighted = signal<SafeHtml | null>(null);
  readonly markdownTooBig = computed(() => this.text().length > MAX_MARKDOWN_RENDER_CHARS);
  readonly csv = computed(() => (this.kind() === 'csv' && this.decoded() ? parseCsv(this.text(), csvDelimiterFor(this.file())) : null));
  readonly canRender = computed(() => this.kind() === 'csv' || (this.kind() === 'markdown' && !this.markdownTooBig()));
  readonly showRendered = computed(() => this.canRender() && this.rendered());

  private generation = 0;

  constructor() {
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
  }

  private async decode(blob: Blob, generation: number): Promise<void> {
    try {
      const decoded = decodeTextPreview(new Uint8Array(await blob.arrayBuffer()));
      if (generation !== this.generation) return;
      this.decoded.set(decoded);
      this.highlighted.set(null);
      if (this.kind() === 'text' && !decoded.binary && decoded.text.length > 0 && decoded.text.length <= MAX_HIGHLIGHT_CHARS) {
        void this.highlight(decoded.text, generation);
      }
    } catch {
      if (generation === this.generation) this.failed.set(true);
    }
  }

  private async highlight(text: string, generation: number): Promise<void> {
    try {
      const monaco = await this.loadMonaco(this.document);
      const language = monacoLanguageFor(monaco, this.file().name);
      if (language === 'plaintext' || generation !== this.generation) return;
      const html = await monaco.editor.colorize(text, language, { tabSize: 4 });
      // Monaco escapes the text and emits only spans with token classes; nothing from the file
      // reaches the DOM as markup.
      if (generation === this.generation) this.highlighted.set(this.sanitizer.bypassSecurityTrustHtml(html));
    } catch {
      // Highlighting is a nicety; the plain text is already shown.
    }
  }
}

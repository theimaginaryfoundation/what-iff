import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { renderLatexMath } from '../../../core/utils/latex-math.helpers';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';
import {
  csvDelimiterFor,
  DecodedText,
  decodeTextPreview,
  FilePreviewKind,
  MAX_MARKDOWN_RENDER_CHARS,
  parseCsv,
  previewKindFor,
} from '../../gallery/helpers/file-preview.helpers';

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

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  readonly kind = computed<FilePreviewKind>(() => previewKindFor(this.file()));
  readonly decoded = signal<DecodedText | null>(null);
  readonly failed = signal(false);
  /** Markdown and CSV: the rendered view (true) or the raw text. */
  readonly rendered = signal(true);
  readonly wrapLines = signal(true);

  readonly text = computed(() => this.decoded()?.text ?? '');
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
      if (generation === this.generation) this.decoded.set(decoded);
    } catch {
      if (generation === this.generation) this.failed.set(true);
    }
  }
}

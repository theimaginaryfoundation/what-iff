import { DatePipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
} from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { MarkdownModule } from 'ngx-markdown';
import { Observable, of } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { saveBlobAsFile } from '../../../core/utils/download.helpers';
import {
  ArrowLeftIconComponent,
  DownloadIconComponent,
  EditIconComponent,
} from '../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import {
  csvDelimiterFor,
  DecodedText,
  decodeTextPreview,
  FilePreviewKind,
  fileTypeLabel,
  MAX_MARKDOWN_RENDER_CHARS,
  parseCsv,
  previewKindFor,
} from '../helpers/file-preview.helpers';
import { galleryDisplayBaseName, galleryFilenameFromBaseName } from '../helpers/gallery-filename.helpers';
import { folderLabel } from '../helpers/gallery-folder.helpers';
import { formatSource, sourceForImage } from '../helpers/image-source.helpers';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

/**
 * Shows one gallery file inside the gallery tab, read-only: text and code as text, Markdown and
 * CSV rendered (with the raw text a click away), PDFs in the browser's own viewer, and a download
 * for anything it cannot show. Editing is deliberately out of scope; rename, move and delete are
 * the same metadata actions the image popup has.
 */
@Component({
  selector: 'app-file-viewer',
  standalone: true,
  imports: [
    DatePipe,
    RouterLink,
    MarkdownModule,
    TooltipDirective,
    ArrowLeftIconComponent,
    DownloadIconComponent,
    EditIconComponent,
  ],
  templateUrl: './file-viewer.component.html',
  styleUrl: './file-viewer.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FileViewerComponent {
  private readonly files = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly sanitizer = inject(DomSanitizer);

  readonly file = input.required<FileAttachment>();
  /** The personality this file is a document of, by its current name, if any. */
  readonly personalityName = input<string | null>(null);
  readonly hasPrev = input(false);
  readonly hasNext = input(false);

  readonly close = output<void>();
  readonly previous = output<void>();
  readonly next = output<void>();
  readonly delete = output<FileAttachment>();
  readonly rename = output<{ id: string; name: string }>();
  readonly moveToFolder = output<string>();

  /**
   * Whether the browser can show a PDF inline. Android WebView (the mobile app) cannot, and says so
   * here; a browser too old to have the flag is assumed to have a viewer.
   */
  readonly pdfInline = (globalThis.navigator as { pdfViewerEnabled?: boolean } | undefined)?.pdfViewerEnabled !== false;

  readonly kind = computed<FilePreviewKind>(() => previewKindFor(this.file()));
  readonly typeLabel = computed(() => fileTypeLabel(this.file()));
  readonly title = computed(() => this.file().name || 'Untitled file');
  readonly folderName = computed(() => {
    const folder = this.file().folder ?? '';
    return folder === '' ? 'Top level' : folderLabel(folder);
  });
  /** Where the file came from, in words: a personality's document, or uploaded / generated in a thread. */
  readonly origin = computed(() => {
    const file = this.file();
    const personality = this.personalityName() ?? file.personalities?.[0]?.name ?? null;
    if (file.personality_id && personality) {
      return `Document of ${personality}`;
    }
    const source = sourceForImage(file);
    return source === 'unknown' ? '' : formatSource(source);
  });

  readonly state = signal<LoadState>('idle');
  readonly decoded = signal<DecodedText | null>(null);
  /** An object URL for a PDF or image being shown, revoked when the file changes. */
  readonly objectUrl = signal<string | null>(null);
  readonly safeObjectUrl = computed<SafeResourceUrl | null>(() => {
    const url = this.objectUrl();
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  });
  /** Markdown and CSV: the rendered view (true) or the raw text. */
  readonly rendered = signal(true);
  readonly wrapLines = signal(true);
  readonly downloading = signal(false);
  readonly isRenaming = signal(false);
  readonly renameDraft = signal('');

  readonly text = computed(() => this.decoded()?.text ?? '');
  readonly markdownTooBig = computed(() => this.text().length > MAX_MARKDOWN_RENDER_CHARS);
  readonly csv = computed(() => (this.kind() === 'csv' && this.decoded() ? parseCsv(this.text(), csvDelimiterFor(this.file())) : null));
  /** Markdown and CSV can switch between rendered and raw; markdown too big to render stays raw. */
  readonly canRender = computed(() => this.kind() === 'csv' || (this.kind() === 'markdown' && !this.markdownTooBig()));
  readonly showRendered = computed(() => this.canRender() && this.rendered());

  private blob: Blob | null = null;
  /** Bumped on every reset, so bytes for a file that is no longer shown are dropped when they arrive. */
  private generation = 0;
  /** Identity of what is shown, so a metadata change (a move, a rename) does not refetch the bytes. */
  private readonly contentKey = computed(() => `${this.file().id}|${this.kind()}`);

  constructor() {
    effect(onCleanup => {
      this.contentKey();
      const file = untracked(this.file);
      const kind = untracked(this.kind);
      untracked(() => this.reset());
      if (kind === 'none' || (kind === 'pdf' && !this.pdfInline)) {
        return;
      }
      this.state.set('loading');
      const generation = this.generation;
      // Untracked: a source that emits synchronously would otherwise make show()'s signal reads
      // dependencies of this effect, and its writes would re-run it.
      const subscription = untracked(() =>
        this.files.downloadFileAttachment(file.id).subscribe({
          next: blob => void this.show(blob, kind, generation),
          error: () => this.state.set('error'),
        }),
      );
      onCleanup(() => subscription.unsubscribe());
    });
    inject(DestroyRef).onDestroy(() => this.revokeObjectUrl());
  }

  download(): void {
    const file = this.file();
    this.downloading.set(true);
    const blob$: Observable<Blob> = this.blob ? of(this.blob) : this.files.downloadFileAttachment(file.id);
    blob$.subscribe({
      next: blob => {
        this.downloading.set(false);
        saveBlobAsFile(blob, file.name || `file-${file.id}`);
      },
      error: () => {
        this.downloading.set(false);
        void this.confirmationService.alert({
          title: 'Download failed',
          message: 'Could not download this file. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  startRename(): void {
    this.renameDraft.set(galleryDisplayBaseName(this.file().name));
    this.isRenaming.set(true);
  }

  cancelRename(): void {
    this.isRenaming.set(false);
  }

  submitRename(): void {
    const file = this.file();
    const nextName = galleryFilenameFromBaseName(file.name, this.renameDraft());
    this.isRenaming.set(false);
    if (nextName && nextName !== file.name) {
      this.rename.emit({ id: file.id, name: nextName });
    }
  }

  private async show(blob: Blob, kind: FilePreviewKind, generation: number): Promise<void> {
    this.blob = blob;
    if (kind === 'pdf' || kind === 'image') {
      // The server sends the stored MIME type; a PDF uploaded without one still needs to be a PDF here.
      const typed = kind === 'pdf' && blob.type !== 'application/pdf' ? new Blob([blob], { type: 'application/pdf' }) : blob;
      this.revokeObjectUrl();
      this.objectUrl.set(URL.createObjectURL(typed));
      this.state.set('ready');
      return;
    }
    try {
      const decoded = decodeTextPreview(new Uint8Array(await blob.arrayBuffer()));
      if (generation !== this.generation) return;
      this.decoded.set(decoded);
      this.state.set('ready');
    } catch {
      if (generation === this.generation) this.state.set('error');
    }
  }

  private reset(): void {
    this.generation += 1;
    this.blob = null;
    this.revokeObjectUrl();
    this.decoded.set(null);
    this.state.set('idle');
    this.rendered.set(true);
    this.isRenaming.set(false);
  }

  private revokeObjectUrl(): void {
    const url = this.objectUrl();
    if (url) {
      URL.revokeObjectURL(url);
      this.objectUrl.set(null);
    }
  }
}

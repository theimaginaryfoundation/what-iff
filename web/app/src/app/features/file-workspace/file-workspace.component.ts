import { DatePipe, NgComponentOutlet } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  Type,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Observable, of, take } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { saveBlobAsFile } from '../../core/utils/download.helpers';
import {
  ArrowLeftIconComponent,
  ContractIconComponent,
  DownloadIconComponent,
  EditIconComponent,
  ExpandIconComponent,
  XIconComponent,
} from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { galleryDisplayBaseName, galleryFilenameFromBaseName } from '../gallery/helpers/gallery-filename.helpers';
import { FilePreviewKind, fileTypeLabel, previewKindFor } from '../gallery/helpers/file-preview.helpers';
import { folderLabel } from '../gallery/helpers/gallery-folder.helpers';
import { formatSource, sourceForImage } from '../gallery/helpers/image-source.helpers';
import { VIEWER_LOADER } from './viewers/registry';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

/**
 * The file workspace (ADR 0x043 §1): one file's toolbar (name, type, where it is, what can be done
 * to it) over the viewer the registry picks for its kind. The same component serves the gallery
 * (in the page, with a back button, Move and Prev/Next) and the chat (beside or over the thread,
 * with close and expand). It fetches the bytes once, for the viewer and the download, and refetches
 * only when the file or its kind changes, never for a rename or a move.
 */
@Component({
  selector: 'app-file-workspace',
  standalone: true,
  imports: [
    DatePipe,
    NgComponentOutlet,
    RouterLink,
    TooltipDirective,
    ArrowLeftIconComponent,
    ContractIconComponent,
    DownloadIconComponent,
    EditIconComponent,
    ExpandIconComponent,
    XIconComponent,
  ],
  templateUrl: './file-workspace.component.html',
  styleUrl: './file-workspace.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FileWorkspaceComponent {
  private readonly files = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly loadViewer = inject(VIEWER_LOADER);

  readonly file = input.required<FileAttachment>();
  /** The personality this file is a document of, by its current name, if any. */
  readonly personalityName = input<string | null>(null);
  /** When set, a back button with this label replaces the close button (the gallery's "Gallery"). */
  readonly backLabel = input<string | null>(null);
  readonly hasPrev = input(false);
  readonly hasNext = input(false);
  /** Prev / Next through the host's list of files (the gallery). */
  readonly showNavigation = input(false);
  /** "Move to…", where the host has the folder picker (the gallery). */
  readonly showMove = input(false);
  /** The expand / collapse control (the chat's docked layout). */
  readonly expandable = input(false);
  readonly expanded = input(false);
  /** A link to the gallery, where the file can be filed and found again (the chat). */
  readonly showGalleryLink = input(false);

  readonly close = output<void>();
  readonly previous = output<void>();
  readonly next = output<void>();
  readonly delete = output<FileAttachment>();
  readonly rename = output<{ id: string; name: string }>();
  readonly moveToFolder = output<string>();
  readonly toggleExpand = output<void>();

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
    if (file.personality_id && personality) return `Document of ${personality}`;
    const source = sourceForImage(file);
    return source === 'unknown' ? '' : formatSource(source);
  });

  readonly state = signal<LoadState>('idle');
  readonly blob = signal<Blob | null>(null);
  readonly viewer = signal<Type<unknown> | null>(null);
  readonly viewerInputs = computed(() => {
    const blob = this.blob();
    return blob ? { file: this.file(), blob } : null;
  });
  readonly downloading = signal(false);
  readonly isRenaming = signal(false);
  readonly renameDraft = signal('');

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
      const load = this.loadViewer(kind);
      if (!load) return;
      this.state.set('loading');
      const generation = this.generation;
      void load.then(type => {
        if (generation === this.generation) this.viewer.set(type);
      });
      const subscription = untracked(() =>
        this.files.downloadFileAttachment(file.id).subscribe({
          next: blob => {
            if (generation !== this.generation) return;
            this.blob.set(blob);
            this.state.set('ready');
          },
          error: () => {
            if (generation === this.generation) this.state.set('error');
          },
        }),
      );
      onCleanup(() => subscription.unsubscribe());
    });
    inject(DestroyRef).onDestroy(() => (this.generation += 1));
  }

  download(): void {
    if (this.downloading()) return;
    const file = this.file();
    this.downloading.set(true);
    const cached = this.blob();
    const blob$: Observable<Blob> = cached ? of(cached) : this.files.downloadFileAttachment(file.id);
    blob$.pipe(take(1)).subscribe({
      next: blob => {
        this.downloading.set(false);
        saveBlobAsFile(blob, file.name || `file-${file.id}`);
      },
      error: () => {
        this.downloading.set(false);
        void this.confirmationService.alert({ title: 'Download failed', message: 'Could not download this file. Please try again.', type: 'danger' });
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
    if (nextName && nextName !== file.name) this.rename.emit({ id: file.id, name: nextName });
  }

  private reset(): void {
    this.generation += 1;
    this.blob.set(null);
    this.viewer.set(null);
    this.state.set('idle');
    this.isRenaming.set(false);
  }
}

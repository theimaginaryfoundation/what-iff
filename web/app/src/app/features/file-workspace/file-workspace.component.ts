import { DOCUMENT, DatePipe, NgComponentOutlet } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  ComponentRef,
  DestroyRef,
  Type,
  ViewContainerRef,
  computed,
  effect,
  forwardRef,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Observable, of, take } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { conflictVersionOf } from '../../core/utils/api-error.helpers';
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
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { COMMENT_SAVER, CommentSaveResult, CommentSaver, FileEditor } from './editors/file-editor';
import { ImageMarkupComponent, MarkupResult } from './markup/image-markup.component';
import { galleryDisplayBaseName, galleryFilenameFromBaseName } from '../gallery/helpers/gallery-filename.helpers';
import { FilePreviewKind, fileTypeLabel, previewKindFor } from '../gallery/helpers/file-preview.helpers';
import { folderLabel, folderPathError, normalizeFolderPath } from '../gallery/helpers/gallery-folder.helpers';
import { formatSource, sourceForImage } from '../gallery/helpers/image-source.helpers';
import { FileWorkspaceService } from './file-workspace.service';
import { EDITOR_LOADER, VIEWER_LOADER, hasEditor, viewerFor } from './viewers/registry';

type LoadState = 'idle' | 'loading' | 'ready' | 'error';

/** The image types markup can draw on: what browsers decode and image vendors accept (ADR 0x047). */
const MARKUP_TYPES = new Set(['image/png', 'image/jpeg', 'image/jpg', 'image/webp']);

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
    ModalComponent,
    ImageMarkupComponent,
  ],
  templateUrl: './file-workspace.component.html',
  styleUrl: './file-workspace.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  // The viewer it hosts saves comments made while viewing through it (ADR 0x045).
  providers: [{ provide: COMMENT_SAVER, useExisting: forwardRef(() => FileWorkspaceComponent) }],
})
export class FileWorkspaceComponent implements CommentSaver {
  private readonly files = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly workspace = inject(FileWorkspaceService);
  private readonly document = inject(DOCUMENT);
  private readonly loadViewer = inject(VIEWER_LOADER);
  private readonly loadEditor = inject(EDITOR_LOADER);
  private readonly editorOutlet = viewChild('editorOutlet', { read: ViewContainerRef });

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
  /** Whether editing is offered: not on a phone (ADR 0x043 §1). */
  readonly canEdit = input(true);
  /** The conversation a save is made from, carried on the save so its container is updated. */
  readonly chatId = input<string | null>(null);
  /** Whether "Mark up" is offered: the host can add the result to a message (the chat; ADR 0x047). */
  readonly markupAvailable = input(false);
  /** A warning for the markup surface, such as an image vendor that cannot edit. */
  readonly markupNotice = input<string | null>(null);

  readonly close = output<void>();
  readonly previous = output<void>();
  readonly next = output<void>();
  readonly delete = output<FileAttachment>();
  readonly rename = output<{ id: string; name: string }>();
  readonly moveToFolder = output<string>();
  readonly toggleExpand = output<void>();
  /** The file's metadata after a save (a new content_version), for the host to adopt. */
  readonly fileChanged = output<FileAttachment>();
  /** Save As: the host creates the file where the user chose and opens it. */
  readonly saveAs = output<{ name: string; folder: string; blob: Blob }>();
  /** Markup to add to the next message; the host calls finishMarkup when it has. */
  readonly markupSubmit = output<{ file: FileAttachment; result: MarkupResult }>();

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

  // Editing (ADR 0x043 §3) and markup (ADR 0x047).
  readonly mode = signal<'view' | 'edit' | 'markup'>('view');
  readonly editable = computed(() => this.canEdit() && hasEditor(this.kind(), this.file().file_type) && this.state() === 'ready');
  readonly editorDirty = signal(false);
  /** The editor holds work beyond its content (a running notebook) that closing it would discard. */
  readonly editorBusy = signal(false);
  /** Leaving edit or markup mode would lose something: unsaved changes, work in progress, a drawing. */
  readonly holdsWork = computed(() => this.editorDirty() || this.editorBusy() || this.markupDirty());
  /**
   * Comments can be written while viewing a file the user may change (ADR 0x045). Kept beside the
   * file (text, images) that is all they need; written into it (saveComments), the file needs an editor too.
   */
  readonly canComment = computed(() => this.canEdit() && this.state() === 'ready' && this.mode() === 'view');
  /** The file whose lossy save the user accepted for comments, so they are asked once, not per comment. */
  private lossyCommentsAccepted: string | null = null;
  readonly canMarkup = computed(
    () =>
      this.markupAvailable() &&
      this.kind() === 'image' &&
      MARKUP_TYPES.has((this.file().file_type || '').toLowerCase()) &&
      this.state() === 'ready',
  );
  readonly markupDirty = signal(false);
  readonly markupSending = signal(false);
  readonly saving = signal(false);
  readonly saveError = signal<string | null>(null);
  /** The current version the server reported on a 412; the conflict dialog is open while set. */
  readonly conflictVersion = signal<number | null>(null);
  readonly saveAsOpen = signal(false);
  readonly saveAsName = signal('');
  readonly saveAsFolder = signal('');
  readonly saveAsError = signal<string | null>(null);
  private editorRef: ComponentRef<FileEditor> | null = null;
  /** The version of the bytes the editor holds. The file's metadata can move on during an edit. */
  private loadedVersion = 1;

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
      this.loadedVersion = file.content_version ?? 1;
      this.state.set('loading');
      const generation = this.generation;
      load.then(
        type => {
          if (generation === this.generation) this.viewer.set(type);
        },
        error => {
          console.error('The viewer could not be loaded', error);
          if (generation === this.generation) this.state.set('error');
        },
      );
      const subscription = untracked(() =>
        this.files.downloadFileContent(file.id).subscribe({
          next: ({ blob, version }) => {
            if (generation !== this.generation) return;
            // The version of these bytes, which the file row (possibly cached) may not match.
            if (version !== null) this.loadedVersion = version;
            this.blob.set(blob);
            // A viewer that failed to load stays an error even when the bytes arrive after it.
            if (this.state() !== 'error') this.state.set('ready');
          },
          error: () => {
            if (generation === this.generation) this.state.set('error');
          },
        }),
      );
      onCleanup(() => subscription.unsubscribe());
    });
    // A new blob (after a save or a reload) or new metadata reaches the open editor.
    effect(() => {
      const blob = this.blob();
      const file = this.file();
      untracked(() => {
        if (this.editorRef && blob) {
          this.editorRef.setInput('file', file);
          this.editorRef.setInput('blob', blob);
        }
      });
    });
    // Route changes, resizes across a breakpoint and closing the tab all ask this.
    effect(() => {
      const unsaved = this.mode() !== 'view' && this.holdsWork();
      untracked(() => this.workspace.setUnsaved(unsaved));
    });
    const view = this.document.defaultView;
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      if (this.mode() !== 'view' && this.holdsWork()) event.preventDefault();
    };
    view?.addEventListener('beforeunload', warnBeforeUnload);
    inject(DestroyRef).onDestroy(() => {
      view?.removeEventListener('beforeunload', warnBeforeUnload);
      this.workspace.setUnsaved(false);
      this.generation += 1;
      this.destroyEditor();
    });
  }

  // --- editing -----------------------------------------------------------------------------------

  async startEdit(): Promise<void> {
    if (!this.editable() || this.mode() === 'edit') return;
    // A comment save running in the viewer replaces the bytes (or reloads them on a conflict) when
    // it lands: the editor opens on what it leaves, or that would replace what is typed.
    if (this.commentSave) {
      await this.settleCommentSaves();
      if (this.mode() !== 'view' || this.state() !== 'ready') return;
    }
    const load = this.loadEditor(this.kind());
    const outlet = this.editorOutlet();
    const blob = this.blob();
    if (!load || !outlet || !blob) return;
    this.mode.set('edit');
    this.saveError.set(null);
    const type = (await load) as Type<FileEditor>;
    if (this.mode() !== 'edit' || this.editorRef) return;
    const ref = outlet.createComponent(type);
    ref.setInput('file', this.file());
    ref.setInput('blob', blob);
    if (viewerFor(this.kind())?.editInPlace) ref.setInput('readOnly', false);
    const instance = ref.instance as FileEditor & {
      dirtyChange?: { subscribe(fn: (dirty: boolean) => void): unknown };
      busyChange?: { subscribe(fn: (busy: boolean) => void): unknown };
      saveRequested?: { subscribe(fn: () => void): unknown };
    };
    instance.dirtyChange?.subscribe(dirty => this.editorDirty.set(dirty));
    instance.busyChange?.subscribe(busy => this.editorBusy.set(busy));
    instance.saveRequested?.subscribe(() => void this.save());
    this.editorRef = ref;
  }

  /** Leaves edit or markup mode, asking first when there are unsaved changes or a drawing. */
  async stopEdit(): Promise<boolean> {
    if (this.mode() === 'markup') return this.stopMarkup();
    if (this.mode() !== 'edit') return true;
    if (this.holdsWork() && !(await this.confirmationService.confirmDiscardChanges())) return false;
    this.destroyEditor();
    this.mode.set('view');
    return true;
  }

  // --- markup (ADR 0x047) -------------------------------------------------------------------------

  startMarkup(): void {
    if (!this.canMarkup() || this.mode() !== 'view') return;
    this.saveError.set(null);
    this.markupDirty.set(false);
    this.mode.set('markup');
  }

  async stopMarkup(): Promise<boolean> {
    if (this.mode() !== 'markup') return true;
    if (this.markupSending()) return false;
    if (this.markupDirty() && !(await this.confirmationService.confirmDiscardChanges())) return false;
    this.markupDirty.set(false);
    this.mode.set('view');
    return true;
  }

  onMarkupSubmitted(result: MarkupResult): void {
    if (this.markupSending()) return;
    this.markupSending.set(true);
    this.saveError.set(null);
    this.markupSubmit.emit({ file: this.file(), result });
  }

  /** The host added the markup to the message (back to the image) or could not (the drawing stays). */
  finishMarkup(added: boolean, message = 'Could not add the markup to your message. Please try again.'): void {
    this.markupSending.set(false);
    if (!added) {
      this.saveError.set(message);
      return;
    }
    this.markupDirty.set(false);
    if (this.mode() === 'markup') this.mode.set('view');
  }

  async requestClose(): Promise<void> {
    if (await this.stopEdit()) this.close.emit();
  }

  async goPrevious(): Promise<void> {
    if (await this.stopEdit()) this.previous.emit();
  }

  async goNext(): Promise<void> {
    if (await this.stopEdit()) this.next.emit();
  }

  /** Expanding moves the workspace to another layout, which rebuilds the editor. */
  async requestToggleExpand(): Promise<void> {
    if (await this.stopEdit()) this.toggleExpand.emit();
  }

  /** Saves the editor's content as the file's new version; a stale version opens the conflict dialog. */
  async save(opts: { overwrite?: boolean } = {}): Promise<void> {
    const editor = this.editorRef?.instance;
    const file = this.file();
    if (!editor || this.saving()) return;
    this.saving.set(true);
    this.saveError.set(null);
    let content: Blob;
    try {
      content = await editor.content();
    } catch (error) {
      // The editor could not produce the file (a format this browser cannot encode): its reason
      // is the useful message, and nothing was sent.
      this.saveError.set(error instanceof Error && error.message ? error.message : 'This file could not be prepared for saving.');
      this.saving.set(false);
      return;
    }
    try {
      // Asked once the content exists: writing it is what tells an editor everything it dropped.
      const losses = editor.losses?.() ?? [];
      if (losses.length && !opts.overwrite) {
        this.saving.set(false);
        if (!(await this.confirmLossySave(losses))) return;
        this.saving.set(true);
      }
      const ifMatch = opts.overwrite ? null : this.loadedVersion;
      const updated = await this.writeContent(file, content, ifMatch);
      this.conflictVersion.set(null);
      this.loadedVersion = updated.content_version ?? this.loadedVersion + 1;
      this.blob.set(content);
      editor.markSaved(content);
      this.editorDirty.set(editor.dirty());
      const stored = { ...file, ...updated, chat_id: updated.chat_id ?? file.chat_id };
      this.fileChanged.emit(stored);
      if (editor.saved) {
        try {
          await editor.saved(stored, content);
        } catch {
          this.saveError.set('The file was saved, but its comments were not. Save again to retry.');
        }
        this.editorDirty.set(editor.dirty());
      }
    } catch (error) {
      const current = conflictVersionOf(error);
      if (current !== null) {
        this.conflictVersion.set(current);
      } else {
        this.saveError.set('Could not save the file. Please try again.');
      }
    } finally {
      this.saving.set(false);
    }
  }

  /**
   * Writes the viewer's comments as the file's new version (CommentSaver). A save that would lose
   * content asks first, once per file; declining, or a version written elsewhere meanwhile, reloads
   * the file as it is, without the comment.
   */
  async saveComments(editor: Pick<FileEditor, 'content' | 'markSaved' | 'losses'>): Promise<CommentSaveResult> {
    const running = this.writeComments(editor);
    this.commentSave = running;
    try {
      return await running;
    } finally {
      if (this.commentSave === running) this.commentSave = null;
    }
  }

  /**
   * Waits until no comment save is running: a viewer's autosave queues the next save behind the
   * one running and starts it once that lands, so after each one this waits a task for the next
   * to start (a comment posted during the first save would otherwise be written, or refused, once
   * the editor is open).
   */
  private async settleCommentSaves(): Promise<void> {
    while (this.commentSave) {
      await this.commentSave;
      await new Promise(resolve => setTimeout(resolve));
    }
  }

  /** A comment save running in the viewer (saveComments), which startEdit waits for. */
  private commentSave: Promise<CommentSaveResult> | null = null;

  private async writeComments(editor: Pick<FileEditor, 'content' | 'markSaved' | 'losses'>): Promise<CommentSaveResult> {
    const file = this.file();
    if (!this.canComment() || !this.editable()) return 'failed';
    let content: Blob;
    try {
      content = await editor.content();
    } catch {
      return 'failed';
    }
    const losses = editor.losses?.() ?? [];
    if (losses.length && this.lossyCommentsAccepted !== file.id) {
      if (!(await this.confirmLossySave(losses))) {
        this.reloadContent();
        return 'cancelled';
      }
      this.lossyCommentsAccepted = file.id;
    }
    try {
      const updated = await this.writeContent(file, content, this.loadedVersion);
      this.loadedVersion = updated.content_version ?? this.loadedVersion + 1;
      this.blob.set(content);
      editor.markSaved(content);
      this.fileChanged.emit({ ...file, ...updated, chat_id: updated.chat_id ?? file.chat_id });
      return 'saved';
    } catch (error) {
      if (conflictVersionOf(error) !== null) {
        this.reloadContent();
        return 'conflict';
      }
      return 'failed';
    }
  }

  private writeContent(file: FileAttachment, content: Blob, ifMatch: number | null): Promise<FileAttachment> {
    return new Promise<FileAttachment>((resolve, reject) =>
      this.files
        .replaceContent(file.id, content, file.file_type, ifMatch, this.chatId())
        .pipe(take(1))
        .subscribe({ next: resolve, error: reject }),
    );
  }

  /** CommentSaver: a viewer found the file changed elsewhere. Only a viewer asks, so no edits are lost. */
  showCurrentContent(): void {
    if (this.mode() === 'view') this.reloadContent();
  }

  /** Shows the file as stored now, dropping what the viewer holds. */
  private reloadContent(): void {
    this.generation += 1;
    this.refetch();
  }

  /** The editor cannot keep everything the original had: saving over it loses that for good. */
  private confirmLossySave(losses: readonly string[]): Promise<boolean> {
    return this.confirmationService.confirm({
      title: 'Save over the original?',
      message: `This editor cannot keep: ${losses.join('; ')}. Saving replaces the original without them. Use Save as to keep the original file.`,
      confirmText: 'Save anyway',
      cancelText: 'Cancel',
      type: 'warning',
    });
  }

  /** Conflict: drop the edits and show what is on the server now. */
  reloadAfterConflict(): void {
    this.loadedVersion = this.conflictVersion() ?? this.loadedVersion;
    this.conflictVersion.set(null);
    this.destroyEditor();
    this.mode.set('view');
    this.generation += 1;
    this.refetch();
  }

  overwriteAfterConflict(): void {
    this.conflictVersion.set(null);
    void this.save({ overwrite: true });
  }

  dismissConflict(): void {
    this.conflictVersion.set(null);
  }

  openSaveAs(): void {
    const file = this.file();
    this.saveAsName.set(file.name);
    this.saveAsFolder.set(file.folder ?? '');
    this.saveAsError.set(null);
    this.conflictVersion.set(null);
    this.saveAsOpen.set(true);
  }

  cancelSaveAs(): void {
    this.saveAsOpen.set(false);
  }

  async submitSaveAs(): Promise<void> {
    const name = this.saveAsName().trim();
    if (!name || /[\\/]/.test(name)) {
      this.saveAsError.set('Enter a file name without slashes.');
      return;
    }
    const rawFolder = this.saveAsFolder().trim();
    let folder = '';
    if (rawFolder) {
      const problem = folderPathError(rawFolder);
      if (problem) {
        this.saveAsError.set(problem);
        return;
      }
      folder = normalizeFolderPath(rawFolder) ?? '';
    }
    let content: Blob | null;
    try {
      content = this.editorRef ? await this.editorRef.instance.content() : this.blob();
    } catch (error) {
      this.saveAsError.set(error instanceof Error ? error.message : 'This file could not be prepared for saving.');
      return;
    }
    if (!content) return;
    this.saveAsOpen.set(false);
    this.saveAs.emit({ name, folder, blob: content });
  }

  /** Re-reads the current file's bytes (after a conflict reload). */
  private refetch(): void {
    const file = this.file();
    this.state.set('loading');
    const generation = this.generation;
    this.files
      .downloadFileContent(file.id)
      .pipe(take(1))
      .subscribe({
        next: ({ blob, version }) => {
          if (generation !== this.generation) return;
          if (version !== null) this.loadedVersion = version;
          this.blob.set(blob);
          this.state.set('ready');
        },
        error: () => {
          if (generation === this.generation) this.state.set('error');
        },
      });
  }

  private destroyEditor(): void {
    this.editorRef?.destroy();
    this.editorRef = null;
    this.editorDirty.set(false);
    this.editorBusy.set(false);
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
    if (nextName && nextName !== file.name) this.rename.emit({ id: file.id, name: nextName });
  }

  private reset(): void {
    this.generation += 1;
    this.destroyEditor();
    this.mode.set('view');
    this.markupDirty.set(false);
    this.markupSending.set(false);
    this.conflictVersion.set(null);
    this.saveAsOpen.set(false);
    this.saveError.set(null);
    this.blob.set(null);
    this.viewer.set(null);
    this.state.set('idle');
    this.isRenaming.set(false);
  }
}

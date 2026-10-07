import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  ElementRef,
  inject,
  Injector,
  OnInit,
  signal,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { EMPTY, catchError } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { GalleryKind, ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityService } from '../../core/services/personality.service';
import { PersonalityMediaJobService } from '../../core/services/personality-media-job.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { DEFAULT_THREAD_NAME } from '../../core/models/chat.model';
import { FileAttachment, isImageAttachment } from '../../core/models/file-attachment.model';
import { Personality, PersonalityExpression, buildPersonalityUpdateRequest } from '../../core/models/personality.model';
import { environment } from '../../../environments/environment';
import { GalleryFilters, toGalleryTileVm } from './helpers/gallery-vm.helpers';
import { AssignAsExpressionFlowComponent } from './components/assign-as-expression-flow.component';
import {
  GalleryFileImportRequest, GalleryImportModalComponent, GalleryImportPersonalityOption,
} from './components/gallery-import-modal.component';
import { GalleryPersonalityOption } from './components/gallery-filter-bar.component';
import { GalleryFolderToolsComponent } from './components/gallery-folder-tools.component';
import { GalleryGridComponent } from './components/gallery-grid.component';
import { ImageDetailModalComponent } from './components/image-detail-modal.component';
import { FileViewerComponent } from './components/file-viewer.component';
import { PersonalityExpressionsManagerComponent } from '../personality/detail/personality-expressions-manager.component';
import { PersonalityMediaJobBannerComponent } from '../personality/components/personality-media-job-banner.component';
import { mediaJobFinished$ } from './helpers/gallery-job-refresh.helpers';
import { sourceForImage } from './helpers/image-source.helpers';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { personalityAccent } from '../personality/helpers/personality-vm.helpers';
import { personalityCoverUrl } from '../personality/helpers/cover-image.helpers';

type GalleryMode = 'gallery' | 'expressions';
/**
 * Gallery images carry only `created_at` — the API has no per-image "last used" signal — so creation
 * time is the one real sort key. A "Last used" option is deliberately absent until that data exists.
 */
type GallerySort = 'created';

@Component({
  selector: 'app-gallery-page',
  standalone: true,
  imports: [
    GalleryGridComponent,
    GalleryFolderToolsComponent,
    ImageDetailModalComponent,
    FileViewerComponent,
    GalleryImportModalComponent,
    AssignAsExpressionFlowComponent,
    PersonalityExpressionsManagerComponent,
    PersonalityMediaJobBannerComponent,
    HelpHintComponent,
    TooltipDirective,
  ],
  templateUrl: './gallery-page.component.html',
  styleUrl: './gallery-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryPageComponent implements OnInit {
  readonly view = inject(GalleryViewService);
  private readonly galleryService = inject(ImageGalleryService);
  private readonly chatService = inject(ChatService);
  private readonly personalityService = inject(PersonalityService);
  private readonly fileAttachmentService = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly mediaJobs = inject(PersonalityMediaJobService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  readonly kindOptions: readonly { kind: GalleryKind; label: string; tooltip: string }[] = [
    { kind: 'all', label: 'All', tooltip: 'Images and files together' },
    { kind: 'images', label: 'Images', tooltip: 'Only images' },
    { kind: 'files', label: 'Files', tooltip: 'Documents, code and data from your threads and personalities' },
  ];

  /** The file open in the viewer (the `file` query parameter), if any. */
  readonly openFileId = signal<string | null>(null);
  /** Its metadata: the loaded row at once, then the server's copy (which also names its thread). */
  readonly openFile = signal<FileAttachment | null>(null);
  readonly openFileError = signal<string | null>(null);
  readonly viewerOpen = computed(() => this.openFileId() !== null);
  /** Where the page was scrolled when the viewer opened, to return there on the way back. */
  private browseScrollTop: number | null = null;

  readonly personalities = signal<Personality[]>([]);
  readonly mode = signal<GalleryMode>('gallery');
  readonly pageTitle = computed(() => this.mode() === 'gallery' ? 'Gallery' : 'Expression Manager');
  readonly sort = signal<GallerySort>('created');
  readonly sortDescending = signal(true);
  readonly expressionsByPersonality = signal<Record<string, readonly PersonalityExpression[]>>({});
  readonly expressionLoadingByPersonality = signal<Record<string, boolean>>({});
  readonly importOpen = signal(false);
  readonly assignOpen = signal(false);
  readonly assignImageId = signal<string | null>(null);
  readonly importSubmitting = signal(false);
  readonly expressionsToggleUpdatingByPersonality = signal<Record<string, boolean>>({});

  readonly assignmentEnabled = !!(environment as { enableGalleryExpressionAssignment?: boolean }).enableGalleryExpressionAssignment;

  readonly sourceCounts = computed(() => {
    const images = this.view.images();
    const counts = { all: images.length, generated: 0, imported: 0 };
    for (const image of images) {
      const source = sourceForImage(image);
      if (source === 'generated') counts.generated += 1;
      if (source === 'uploaded') counts.imported += 1;
    }
    return counts;
  });
  readonly sortedImages = computed(() => {
    const direction = this.sortDescending() ? -1 : 1;
    return [...this.view.filteredImages()].sort(
      (a, b) => (Date.parse(a.created_at) - Date.parse(b.created_at)) * direction,
    );
  });
  /** What an empty grid says: a new folder is a prompt to fill it, not a failed search. */
  readonly emptyMessage = computed(() => {
    if (this.view.browsingFolders() && this.view.currentFolder() !== '') {
      return 'This folder is empty. Use Select to pick images or files, then Move to folder to file them here.';
    }
    switch (this.view.kind()) {
      case 'images':
        return 'No images match these filters yet.';
      case 'files':
        return 'No files match these filters yet. Files you share in threads and documents you give a personality show up here.';
      default:
        return 'Nothing matches these filters yet.';
    }
  });
  readonly tiles = computed(() => {
    const namesById = this.personalityNames();
    return this.sortedImages().map(image =>
      toGalleryTileVm(image, this.galleryService.getImageUrl.bind(this.galleryService), namesById),
    );
  });
  readonly selectedTile = computed(() => {
    const selected = this.view.selectedImage();
    if (!selected) return null;
    return toGalleryTileVm(selected, this.galleryService.getImageUrl.bind(this.galleryService), this.personalityNames());
  });
  readonly selectedIndex = computed(() => {
    const selectedId = this.view.selectedImageId();
    if (!selectedId) return -1;
    return this.view.filteredImages().findIndex(row => row.id === selectedId);
  });
  /** The non-image files in the grid's order, which Prev / Next in the viewer step through. */
  readonly filesInView = computed(() => this.sortedImages().filter(row => !isImageAttachment(row)));
  private readonly openFileIndex = computed(() => {
    const id = this.openFileId();
    return id ? this.filesInView().findIndex(row => row.id === id) : -1;
  });
  readonly hasPrevFile = computed(() => this.openFileIndex() > 0);
  readonly hasNextFile = computed(() => this.openFileIndex() >= 0 && this.openFileIndex() < this.filesInView().length - 1);
  readonly hasPrev = computed(() => this.selectedIndex() > 0);
  readonly hasNext = computed(() => this.selectedIndex() >= 0 && this.selectedIndex() < this.view.filteredImages().length - 1);
  readonly selectedPersonalityFilterIds = computed(() => this.view.selectedPersonalityIds());
  readonly filteredExpressionPersonalities = computed(() => {
    const selected = this.selectedPersonalityFilterIds();
    if (selected.length === 0) {
      return this.personalities();
    }
    const selectedSet = new Set(selected);
    return this.personalities().filter(personality => selectedSet.has(personality.id));
  });
  readonly personalityOptions = computed<GalleryPersonalityOption[]>(() =>
    this.personalities().map(row => ({ id: row.id, name: row.name })),
  );
  readonly importPersonalityOptions = computed<GalleryImportPersonalityOption[]>(() =>
    this.personalities().map(personality => ({
      id: personality.id,
      name: personality.name,
      accentColor: personality.accent_color ?? null,
      coverImageUrl: personalityCoverUrl(
        personality,
        [],
        this.galleryService.getImageUrl.bind(this.galleryService),
      ),
      thumbnailCircle: personality.thumbnail_circle ?? null,
    })),
  );
  readonly personalityNames = computed<Record<string, string>>(() =>
    this.personalities().reduce<Record<string, string>>((acc, personality) => {
      acc[personality.id] = personality.name;
      return acc;
    }, {}),
  );
  /** A move made from the viewer (through the move dialog) shows up in its details line. */
  private readonly syncOpenFileFolder = effect(() => {
    const move = this.view.lastMove();
    const file = untracked(this.openFile);
    if (move && file && move.ids.has(file.id)) {
      this.openFile.set({ ...file, folder: move.folder || undefined });
    }
  });
  private readonly syncImportRequest = effect(() => {
    const requestTick = this.view.importRequestTick();
    if (requestTick === 0) return;
    this.importOpen.set(true);
  });

  ngOnInit(): void {
    this.view.setMode(this.mode());
    this.view.loadInitial();
    this.view.loadFolders();
    this.loadPersonalities();
    this.refreshWhenImageJobsFinish();
    this.route.queryParamMap
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(params => {
        const mode = params.get('mode');
        if (mode === 'gallery' || mode === 'expressions') {
          this.setMode(mode);
        }

        const personalityId = params.get('personality_id') || params.get('personalityId');
        if (personalityId) {
          this.view.setSelectedPersonalityIds([personalityId]);
        }

        const imageId = params.get('image');
        if (imageId) {
          this.view.openDetail(imageId);
        }

        this.showFile(params.get('file'));
      });
  }

  onFilterChange(partial: Partial<GalleryFilters>): void {
    this.view.setFilters(partial);
  }

  setMode(mode: GalleryMode): void {
    const returningToGallery = mode === 'gallery' && this.mode() === 'expressions';
    this.mode.set(mode);
    if (returningToGallery) {
      // Expressions generated or assigned in the manager are new images (and folders) the list loaded
      // earlier does not have.
      this.view.refresh();
      this.view.loadFolders();
    }
    this.view.setMode(mode);
    if (mode === 'expressions') {
      this.view.disableGlobalAssociations();
    }
    if (mode === 'expressions') {
      for (const personality of this.personalities()) {
        if (!this.expressionsByPersonality()[personality.id]) {
          this.loadExpressions(personality.id);
        }
      }
    }
  }

  setSourceFilter(source: GalleryFilters['source']): void {
    this.onFilterChange({ source });
  }

  /** Tooltip for a sort button, reflecting the current direction. */
  sortTooltip(sort: GallerySort): string {
    const active = this.sort() === sort;
    if (!active) return 'Sort by date created, newest first';
    return this.sortDescending() ? 'Newest first — click to flip' : 'Oldest first — click to flip';
  }

  setSort(sort: GallerySort): void {
    if (this.sort() === sort) {
      this.sortDescending.update(value => !value);
      return;
    }
    this.sort.set(sort);
    this.sortDescending.set(true);
  }

  onExpressionsChanged(personalityId: string, next: readonly PersonalityExpression[]): void {
    this.expressionsByPersonality.update(current => ({ ...current, [personalityId]: next }));
  }

  onExpressionsEnabledChanged(personality: Personality, enabled: boolean): void {
    if (personality.expressions_enabled === enabled) return;
    this.setExpressionsToggleUpdating(personality.id, true);
    const previous = personality;
    this.personalities.update(rows =>
      rows.map(row => row.id === personality.id ? { ...row, expressions_enabled: enabled } : row),
    );
    const request = buildPersonalityUpdateRequest(personality, { expressions_enabled: enabled });
    this.personalityService.updatePersonality(personality.id, request).subscribe({
      next: updated => {
        this.personalities.update(rows =>
          rows.map(row => row.id === updated.id ? updated : row),
        );
        this.setExpressionsToggleUpdating(personality.id, false);
      },
      error: async () => {
        this.personalities.update(rows =>
          rows.map(row => row.id === personality.id ? previous : row),
        );
        this.setExpressionsToggleUpdating(personality.id, false);
        await this.confirmationService.alert({
          title: 'Update failed',
          message: 'Could not update expression image setting. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  expressionsToggleUpdatingFor(personalityId: string): boolean {
    return this.expressionsToggleUpdatingByPersonality()[personalityId] ?? false;
  }

  private setExpressionsToggleUpdating(personalityId: string, updating: boolean): void {
    this.expressionsToggleUpdatingByPersonality.update(current => ({
      ...current,
      [personalityId]: updating,
    }));
  }

  onOpenImage(imageId: string): void {
    const row = this.view.images().find(image => image.id === imageId);
    if (row && !isImageAttachment(row)) {
      this.router.navigate([], { queryParams: { file: imageId }, queryParamsHandling: 'merge' });
      return;
    }
    this.view.openDetail(imageId);
    this.router.navigate([], {
      queryParams: { image: imageId },
      queryParamsHandling: 'merge',
    });
  }

  onCloseDetail(): void {
    this.view.closeDetail();
    this.router.navigate([], {
      queryParams: { image: null },
      queryParamsHandling: 'merge',
    });
  }

  onRename(payload: { id: string; name: string }): void {
    this.galleryService.renameImage(payload.id, payload.name).subscribe({
      next: updated => this.view.upsertImage(updated),
      error: async () => {
        await this.confirmationService.alert({
          title: 'Rename failed',
          message: 'Could not rename this image. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  /** Something dragged was dropped on a folder tile; a refusal shows in the folder error banner. */
  onDropOnFolder(folderPath: string): void {
    void this.view.dropOn(folderPath);
  }

  /** From the image popup: close it and open the same move dialog select mode uses. */
  onMoveToFolder(imageId: string): void {
    this.onCloseDetail();
    this.view.requestMove([imageId]);
  }

  onAddToThread(payload: { imageId: string; chatId: string }): void {
    this.onCloseDetail();
    void this.router.navigate(['/chat', payload.chatId], {
      queryParams: { galleryImageId: payload.imageId },
    });
  }

  onStartNewChat(imageId: string): void {
    this.onCloseDetail();
    this.chatService.createChat({ name: DEFAULT_THREAD_NAME }).subscribe({
      next: chat => {
        void this.router.navigate(['/chat', chat.id], {
          queryParams: { galleryImageId: imageId },
        });
      },
      error: async () => {
        await this.confirmationService.alert({
          title: 'Could not start thread',
          message: 'Failed to create a new thread. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  async onDelete(imageId: string): Promise<void> {
    const image = this.view.images().find(row => row.id === imageId);
    if (!image) return;
    if (!isImageAttachment(image)) {
      await this.onDeleteFile(image);
      return;
    }
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete image',
      message: `Delete "${image.name}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) {
      return;
    }
    this.galleryService.deleteImage(imageId).subscribe({
      next: () => this.view.removeImage(imageId),
      error: async () => {
        await this.confirmationService.alert({
          title: 'Delete failed',
          message: 'Could not delete this image. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  // --- file viewer ----------------------------------------------------------------------------

  closeFile(): void {
    this.router.navigate([], { queryParams: { file: null }, queryParamsHandling: 'merge' });
  }

  /** Prev / Next through the files in the grid, without stacking a history entry per step. */
  stepFile(delta: -1 | 1): void {
    const target = this.filesInView()[this.openFileIndex() + delta];
    if (target) {
      this.router.navigate([], { queryParams: { file: target.id }, queryParamsHandling: 'merge', replaceUrl: true });
    }
  }

  async onDeleteFile(file: FileAttachment): Promise<void> {
    const personality = file.personality_id
      ? this.personalityNames()[file.personality_id] ?? file.personalities?.[0]?.name ?? 'its personality'
      : null;
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete file',
      message: personality
        ? `Delete "${file.name}"? It is one of ${personality}'s documents, so ${personality} will no longer be able to search or read it. This cannot be undone.`
        : `Delete "${file.name}"? Threads it was shared in will no longer be able to open it. This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) {
      return;
    }
    this.fileAttachmentService.deleteFileAttachment(file.id).subscribe({
      next: () => {
        this.view.removeImage(file.id);
        this.view.loadFolders();
        if (this.openFileId() === file.id) {
          this.closeFile();
        }
      },
      error: async () => {
        await this.confirmationService.alert({
          title: 'Delete failed',
          message: 'Could not delete this file. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  onRenameFile(payload: { id: string; name: string }): void {
    this.galleryService.renameImage(payload.id, payload.name).subscribe({
      next: updated => {
        if (this.view.images().some(row => row.id === updated.id)) {
          this.view.upsertImage(updated);
        }
        // The rename response does not carry the thread link the viewer got from the info read.
        this.openFile.update(file => (file?.id === updated.id ? { ...file, ...updated, chat_id: file.chat_id } : file));
      },
      error: async () => {
        await this.confirmationService.alert({
          title: 'Rename failed',
          message: 'Could not rename this file. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  /**
   * Opens (or closes) the viewer for the `file` query parameter. The row already loaded shows at
   * once; the server's copy follows, so a link to a file in another folder still opens.
   */
  private showFile(fileId: string | null): void {
    const previous = this.openFileId();
    if (fileId === previous) {
      return;
    }
    this.openFileId.set(fileId);
    this.openFileError.set(null);
    if (!fileId) {
      this.openFile.set(null);
      this.restoreBrowseScroll();
      return;
    }
    if (previous === null) {
      this.saveBrowseScroll();
    }
    if (this.mode() !== 'gallery') {
      this.setMode('gallery');
    }
    this.openFile.set(this.view.images().find(row => row.id === fileId) ?? null);
    this.galleryService.getFileInfo(fileId).subscribe({
      next: info => {
        if (this.openFileId() === fileId) {
          this.openFile.set(info);
        }
      },
      error: () => {
        if (this.openFileId() === fileId && !this.openFile()) {
          this.openFileError.set('This file could not be found. It may have been deleted.');
        }
      },
    });
  }

  private saveBrowseScroll(): void {
    const scroller = scrollParent(this.host.nativeElement);
    this.browseScrollTop = scroller?.scrollTop ?? null;
    if (scroller) {
      scroller.scrollTop = 0;
    }
  }

  /** Back in the grid where you left it, once it is shown again. */
  private restoreBrowseScroll(): void {
    const top = this.browseScrollTop;
    this.browseScrollTop = null;
    if (top === null) {
      return;
    }
    afterNextRender(
      () => {
        const scroller = scrollParent(this.host.nativeElement);
        if (scroller) {
          scroller.scrollTop = top;
        }
      },
      { injector: this.injector },
    );
  }

  openImportModal(): void {
    this.importOpen.set(true);
  }

  closeImportModal(): void {
    this.importOpen.set(false);
  }

  onImportFile(request: GalleryFileImportRequest): void {
    this.importSubmitting.set(true);
    const import$ = request.scope === 'global'
      ? this.galleryService.importImage(request.file, {
          title: request.title,
          description: request.description,
          // An import lands in the folder being viewed, so it is where you expect it.
          folder: this.view.browsingFolders() ? this.view.currentFolder() : '',
        })
      : request.personalityId
        ? this.fileAttachmentService.uploadPersonalityFileAttachment(
            request.personalityId,
            request.file,
            { title: request.title, description: request.description },
          )
        : null;
    if (!import$) {
      this.importSubmitting.set(false);
      void this.confirmationService.alert({
        title: 'Import failed',
        message: 'Please choose a personality for "Pin to personality" imports.',
        type: 'danger',
      });
      return;
    }
    import$.subscribe({
      next: uploaded => {
        this.importSubmitting.set(false);
        this.importOpen.set(false);
        this.view.upsertImage(uploaded);
        this.view.loadFolders();
      },
      error: async () => {
        this.importSubmitting.set(false);
        await this.confirmationService.alert({
          title: 'Upload failed',
          message: 'Could not upload that image.',
          type: 'danger',
        });
      },
    });
  }

  onRequestAssign(imageId: string): void {
    if (!this.assignmentEnabled) {
      return;
    }
    this.assignImageId.set(imageId);
    this.assignOpen.set(true);
  }

  closeAssignModal(): void {
    this.assignOpen.set(false);
    this.assignImageId.set(null);
  }

  onAssigned(): void {
    this.assignOpen.set(false);
    this.assignImageId.set(null);
  }

  /**
   * Images made by a background job (expression grids, portraits) appear when it finishes, even if
   * the job was started on another page and is still running when the gallery opens.
   */
  private refreshWhenImageJobsFinish(): void {
    this.mediaJobs
      .refreshActiveJob()
      .pipe(
        catchError(() => EMPTY),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe();
    mediaJobFinished$(this.mediaJobs)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => {
        this.view.refresh();
        this.view.loadFolders();
      });
  }

  private loadPersonalities(): void {
    this.personalityService.listPersonalities(1, 200).subscribe({
      next: response => {
        const rows = response.results ?? [];
        this.personalities.set(rows);
        const knownIds = new Set(rows.map(row => row.id));
        this.expressionsByPersonality.update(current =>
          Object.fromEntries(Object.entries(current).filter(([id]) => knownIds.has(id))),
        );
        this.expressionLoadingByPersonality.update(current =>
          Object.fromEntries(Object.entries(current).filter(([id]) => knownIds.has(id))),
        );
        if (this.mode() === 'expressions') {
          for (const personality of rows) {
            this.loadExpressions(personality.id);
          }
        }
      },
      error: () => this.personalities.set([]),
    });
  }

  private loadExpressions(personalityId: string): void {
    this.expressionLoadingByPersonality.update(current => ({ ...current, [personalityId]: true }));
    this.personalityService.listExpressions(personalityId).subscribe({
      next: rows => {
        this.expressionsByPersonality.update(current => ({ ...current, [personalityId]: rows ?? [] }));
        this.expressionLoadingByPersonality.update(current => ({ ...current, [personalityId]: false }));
      },
      error: () => {
        this.expressionsByPersonality.update(current => ({ ...current, [personalityId]: [] }));
        this.expressionLoadingByPersonality.update(current => ({ ...current, [personalityId]: false }));
      },
    });
  }

  expressionsForPersonality(personalityId: string): readonly PersonalityExpression[] {
    return this.expressionsByPersonality()[personalityId] ?? [];
  }

  expressionsLoadingFor(personalityId: string): boolean {
    return this.expressionLoadingByPersonality()[personalityId] ?? false;
  }

  expressionAccentFor(personality: Personality): string {
    return personalityAccent(personality);
  }

  expressionAvatarFor(personality: Personality): string | null {
    return personalityCoverUrl(
      personality,
      [],
      this.galleryService.getImageUrl.bind(this.galleryService),
    );
  }

}

/** The nearest ancestor that scrolls (the app's main pane), else the document's own scroller. */
function scrollParent(element: HTMLElement): HTMLElement | null {
  for (let node = element.parentElement; node; node = node.parentElement) {
    const overflowY = getComputedStyle(node).overflowY;
    if ((overflowY === 'auto' || overflowY === 'scroll') && node.scrollHeight > node.clientHeight) {
      return node;
    }
  }
  return (document.scrollingElement as HTMLElement | null) ?? null;
}

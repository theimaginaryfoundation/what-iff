import { computed, Injectable, signal, inject } from '@angular/core';
import { firstValueFrom, Subscription } from 'rxjs';
import { finalize } from 'rxjs/operators';

import { FileAttachment } from '../models/file-attachment.model';
import { ImageGalleryService } from './image-gallery.service';
import {
  applyGalleryFilters,
  DEFAULT_GALLERY_FILTERS,
  GalleryFilters,
  GallerySourceFilter,
} from '../../features/gallery/helpers/gallery-vm.helpers';
import { sourceForImage } from '../../features/gallery/helpers/image-source.helpers';
import {
  breadcrumbsFor,
  childFolderTiles,
  GalleryFolder,
  isWithinFolder,
  normalizeFolderPath,
  folderPathError,
} from '../../features/gallery/helpers/gallery-folder.helpers';
import { canDropOn, folderPathAfterDrop, GalleryDrag } from '../../features/gallery/helpers/gallery-dnd.helpers';

export type GalleryAssociationFilterMode = 'all' | 'global' | 'personality';
export type GalleryMode = 'gallery' | 'expressions';

@Injectable({ providedIn: 'root' })
export class GalleryViewService {
  private readonly galleryService = inject(ImageGalleryService);

  readonly pageSize = 40;
  readonly images = signal<FileAttachment[]>([]);
  readonly totalCount = signal(0);
  readonly currentPage = signal(0);
  readonly isLoading = signal(false);
  readonly isLoadingMore = signal(false);
  readonly error = signal<string | null>(null);
  readonly filters = signal<GalleryFilters>({ ...DEFAULT_GALLERY_FILTERS });
  readonly selectedImageId = signal<string | null>(null);
  readonly mode = signal<GalleryMode>('gallery');
  readonly associationFilterMode = signal<GalleryAssociationFilterMode>('all');
  readonly selectedPersonalityIds = signal<string[]>([]);
  readonly importRequestTick = signal(0);

  /** Folders that hold images (all levels), as the server lists them. */
  readonly folders = signal<GalleryFolder[]>([]);
  /** The folder being viewed; "" is the top level. */
  readonly currentFolder = signal('');
  /** Folders created in this session that are still empty. A folder only exists on the server once it holds an image. */
  readonly pendingFolders = signal<string[]>([]);
  /** Flat view of every image regardless of folder (the pre-folders gallery). */
  readonly showAll = signal(false);
  readonly selectionMode = signal(false);
  readonly selectedIds = signal<ReadonlySet<string>>(new Set());
  /** A failed move or rename, shown without replacing the grid. */
  readonly folderError = signal<string | null>(null);
  /** The folder whose rename / move dialog is open, if any. */
  readonly folderEditing = signal<string | null>(null);
  /** What is being dragged right now (images or a folder), so drop targets can tell if they accept it. */
  readonly drag = signal<GalleryDrag | null>(null);
  /** Images a "Move to…" was asked for outside select mode (from the image popup), if any. */
  readonly moveRequest = signal<readonly string[] | null>(null);

  /**
   * The single in-flight list request (first page or load-more). A reload cancels
   * it so a response for a superseded filter/page can never land on top of the
   * new result set (appending a stale page, or advancing currentPage past a page
   * that was never loaded).
   */
  private inflight: Subscription | null = null;

  readonly filteredImages = computed(() => {
    const base = applyGalleryFilters(this.images(), this.filters());
    const mode = this.associationFilterMode();
    if (mode === 'all') {
      return base;
    }
    if (mode === 'global') {
      return base.filter(image => !hasPersonalityAssociation(image));
    }
    const selected = this.selectedPersonalityIds();
    if (selected.length === 0) {
      return base;
    }
    const selectedSet = new Set(selected);
    return base.filter(image => {
      if (image.personality_id && selectedSet.has(image.personality_id)) {
        return true;
      }
      return (image.personalities ?? []).some(personality => selectedSet.has(personality.id));
    });
  });
  /**
   * Whether the grid is showing one folder. Searching looks across every folder, and "show all" is
   * the flat view, so both ignore the folder being viewed.
   */
  readonly browsingFolders = computed(() => !this.showAll() && this.filters().query.trim() === '');
  readonly folderTiles = computed(() =>
    this.browsingFolders() ? childFolderTiles(this.folders(), this.currentFolder(), this.pendingFolders()) : [],
  );
  readonly breadcrumbs = computed(() => breadcrumbsFor(this.currentFolder()));
  readonly selectedCount = computed(() => this.selectedIds().size);
  readonly selectedImage = computed(() => this.filteredImages().find(i => i.id === this.selectedImageId()) ?? null);
  readonly hasMore = computed(() => this.images().length < this.totalCount());
  readonly availableSources = computed<GallerySourceFilter[]>(() => {
    const found = new Set<GallerySourceFilter>(['all']);
    for (const image of this.images()) {
      found.add(sourceForImage(image));
    }
    return [...found];
  });

  loadInitial(): void {
    // Cancel first: its finalize resets the loading flags, which we then set anew.
    this.cancelInflight();
    this.currentPage.set(1);
    this.error.set(null);
    this.isLoading.set(true);
    const activeFilters = this.filters();
    const associationMode = this.associationFilterMode();
    const personalityId = activeFilters.personalityId === 'all' ? undefined : activeFilters.personalityId;
    this.inflight = this.galleryService
      .listImages(1, this.pageSize, {
        name: activeFilters.query,
        personalityId,
        globalOnly: associationMode === 'global',
        folder: this.activeFolder(),
      })
      .pipe(finalize(() => this.isLoading.set(false)))
      .subscribe({
        next: response => {
          this.images.set(response.results ?? []);
          this.totalCount.set(response.total_count ?? 0);
        },
        error: () => {
          this.images.set([]);
          this.totalCount.set(0);
          this.error.set('Failed to load gallery images.');
        },
      });
  }

  loadNextPage(): void {
    if (!this.hasMore() || this.isLoadingMore() || this.isLoading()) {
      return;
    }
    const nextPage = this.currentPage() + 1;
    const activeFilters = this.filters();
    const associationMode = this.associationFilterMode();
    const personalityId = activeFilters.personalityId === 'all' ? undefined : activeFilters.personalityId;
    this.isLoadingMore.set(true);
    this.inflight = this.galleryService
      .listImages(nextPage, this.pageSize, {
        name: activeFilters.query,
        personalityId,
        globalOnly: associationMode === 'global',
        folder: this.activeFolder(),
      })
      .pipe(finalize(() => this.isLoadingMore.set(false)))
      .subscribe({
        next: response => {
          const nextRows = response.results ?? [];
          this.images.update(existing => [...existing, ...nextRows]);
          this.totalCount.set(response.total_count ?? this.totalCount());
          this.currentPage.set(nextPage);
        },
        error: () => {
          this.error.set('Failed to load more images.');
        },
      });
  }

  setFilters(partial: Partial<GalleryFilters>): void {
    const previous = this.filters();
    const next = { ...previous, ...partial };
    this.filters.set(next);
    // source and dateRange are applied client-side by filteredImages over the rows
    // already loaded; refetching for them only threw away loaded pages, so a
    // toggle and toggle-back showed a different set than before.
    if (next.query !== previous.query || next.personalityId !== previous.personalityId) {
      this.loadInitial();
    }
  }

  setSelectedPersonalityIds(ids: readonly string[]): void {
    this.selectedPersonalityIds.set([...ids]);
    this.associationFilterMode.set(ids.length > 0 ? 'personality' : 'all');
    this.loadInitial();
  }

  selectAllAssociations(): void {
    this.selectedPersonalityIds.set([]);
    this.associationFilterMode.set('all');
    this.loadInitial();
  }

  selectGlobalAssociations(): void {
    this.selectedPersonalityIds.set([]);
    this.associationFilterMode.set('global');
    this.loadInitial();
  }

  disableGlobalAssociations(): void {
    if (this.associationFilterMode() !== 'global') {
      return;
    }
    this.selectAllAssociations();
  }

  requestImportModalOpen(): void {
    this.importRequestTick.update(value => value + 1);
  }

  setMode(mode: GalleryMode): void {
    this.mode.set(mode);
  }

  resetFilters(): void {
    this.filters.set({ ...DEFAULT_GALLERY_FILTERS });
    this.loadInitial();
  }

  refresh(): void {
    this.loadInitial();
  }

  openDetail(imageId: string): void {
    this.selectedImageId.set(imageId);
  }

  closeDetail(): void {
    this.selectedImageId.set(null);
  }

  nextDetail(): void {
    const selected = this.selectedImageId();
    if (!selected) return;
    const list = this.filteredImages();
    const index = list.findIndex(row => row.id === selected);
    if (index >= 0 && index < list.length - 1) {
      this.selectedImageId.set(list[index + 1].id);
    }
  }

  previousDetail(): void {
    const selected = this.selectedImageId();
    if (!selected) return;
    const list = this.filteredImages();
    const index = list.findIndex(row => row.id === selected);
    if (index > 0) {
      this.selectedImageId.set(list[index - 1].id);
    }
  }

  removeImage(id: string): void {
    this.images.update(existing => existing.filter(row => row.id !== id));
    this.totalCount.update(total => Math.max(0, total - 1));
    if (this.selectedImageId() === id) {
      this.selectedImageId.set(null);
    }
  }

  // --- folders --------------------------------------------------------------------------------

  /** The folder to ask the server for: undefined (every image) unless viewing one. */
  private activeFolder(): string | undefined {
    return this.browsingFolders() ? this.currentFolder() : undefined;
  }

  loadFolders(): void {
    this.galleryService.listFolders().subscribe({
      next: folders => {
        this.folders.set(folders);
        // A folder that now holds an image (or has one beneath it) is real; stop tracking it as pending.
        this.pendingFolders.update(pending => pending.filter(path => !folders.some(folder => isWithinFolder(folder.path, path))));
      },
      // The tiles keep showing what they had; the images themselves load separately.
      error: () => undefined,
    });
  }

  openFolder(path: string): void {
    this.showAll.set(false);
    this.currentFolder.set(path);
    this.clearSelection();
    this.folderError.set(null);
    this.loadInitial();
  }

  /** Asks for the move dialog for these images, without needing them to be selected. */
  requestMove(ids: readonly string[]): void {
    this.folderError.set(null);
    this.moveRequest.set(ids.length > 0 ? [...ids] : null);
  }

  clearMoveRequest(): void {
    this.moveRequest.set(null);
    this.folderError.set(null);
  }

  startEditingFolder(path: string): void {
    this.folderError.set(null);
    this.folderEditing.set(path);
  }

  stopEditingFolder(): void {
    this.folderEditing.set(null);
    this.folderError.set(null);
  }

  setShowAll(showAll: boolean): void {
    this.showAll.set(showAll);
    this.clearSelection();
    this.loadInitial();
  }

  /**
   * Starts a folder under the one being viewed and opens it. It lives in this session only until an
   * image is moved into it. Returns why it could not be made, or null.
   */
  createFolder(rawName: string): string | null {
    const name = rawName.trim();
    if (name === '') {
      return 'Give the folder a name';
    }
    const base = this.currentFolder();
    const candidate = base === '' ? name : `${base}/${name}`;
    const problem = folderPathError(candidate);
    if (problem) {
      return problem;
    }
    const path = normalizeFolderPath(candidate) ?? '';
    this.pendingFolders.update(pending => (pending.includes(path) ? pending : [...pending, path]));
    this.openFolder(path);
    return null;
  }

  /** Files images in a folder ("" is the top level). Resolves true when the move went through. */
  async moveImages(ids: readonly string[], rawFolder: string): Promise<boolean> {
    const folder = normalizeFolderPath(rawFolder);
    if (folder === null) {
      this.folderError.set(folderPathError(rawFolder) ?? 'That folder name is not allowed');
      return false;
    }
    this.folderError.set(null);
    try {
      await firstValueFrom(this.galleryService.moveImages(ids, folder));
    } catch (error) {
      this.folderError.set(describeFolderError(error, 'Could not move the images.'));
      return false;
    }
    const moved = new Set(ids);
    if (this.browsingFolders()) {
      // The moved images leave this folder's grid, unless they were moved into it.
      if (folder !== this.currentFolder()) {
        const before = this.images().length;
        this.images.update(rows => rows.filter(row => !moved.has(row.id)));
        this.totalCount.update(total => Math.max(0, total - (before - this.images().length)));
      }
    } else {
      this.images.update(rows => rows.map(row => (moved.has(row.id) ? { ...row, folder: folder || undefined } : row)));
    }
    this.clearSelection();
    this.loadFolders();
    return true;
  }

  /** Renames a folder, carrying everything beneath it. Resolves true when it went through. */
  async moveFolder(from: string, rawTo: string): Promise<boolean> {
    const to = normalizeFolderPath(rawTo);
    if (to === null) {
      this.folderError.set(folderPathError(rawTo) ?? 'That folder name is not allowed');
      return false;
    }
    if (to === from) {
      return true;
    }
    if (isWithinFolder(to, from)) {
      this.folderError.set('A folder cannot be moved into itself');
      return false;
    }
    this.folderError.set(null);
    try {
      await firstValueFrom(this.galleryService.moveFolder(from, to));
    } catch (error) {
      this.folderError.set(describeFolderError(error, 'Could not move the folder.'));
      return false;
    }
    const rewrite = (path: string) => (isWithinFolder(path, from) ? `${to}${path.slice(from.length)}`.replace(/^\//, '') : path);
    this.pendingFolders.update(pending => pending.map(rewrite));
    this.currentFolder.update(rewrite);
    this.loadFolders();
    this.loadInitial();
    return true;
  }

  // --- drag and drop --------------------------------------------------------------------------

  /**
   * Starts dragging an image. Dragging one that is part of the selection takes the whole selection
   * with it; dragging any other takes just that image.
   */
  beginImageDrag(imageId: string): void {
    const selected = this.selectedIds();
    this.drag.set({ kind: 'images', ids: selected.has(imageId) ? [...selected] : [imageId] });
  }

  beginFolderDrag(path: string): void {
    this.drag.set({ kind: 'folder', path });
  }

  endDrag(): void {
    this.drag.set(null);
  }

  acceptsDrop(target: string): boolean {
    return canDropOn(this.drag(), target);
  }

  /** Drops what is being dragged on a folder ("" is the top level). Resolves true when it moved. */
  async dropOn(target: string): Promise<boolean> {
    const drag = this.drag();
    this.endDrag();
    if (!canDropOn(drag, target)) {
      return false;
    }
    return drag?.kind === 'folder'
      ? this.moveFolder(drag.path, folderPathAfterDrop(drag.path, target))
      : this.moveImages(drag?.kind === 'images' ? drag.ids : [], target);
  }

  // --- selection ------------------------------------------------------------------------------

  setSelectionMode(on: boolean): void {
    this.selectionMode.set(on);
    if (!on) {
      this.selectedIds.set(new Set());
    }
  }

  toggleSelected(id: string): void {
    this.selectedIds.update(current => {
      const next = new Set(current);
      if (!next.delete(id)) {
        next.add(id);
      }
      return next;
    });
  }

  selectAllShown(): void {
    this.selectedIds.set(new Set(this.filteredImages().map(image => image.id)));
  }

  clearSelection(): void {
    this.selectedIds.set(new Set());
  }

  private cancelInflight(): void {
    this.inflight?.unsubscribe();
    this.inflight = null;
  }

  upsertImage(image: FileAttachment): void {
    this.images.update(existing => {
      const index = existing.findIndex(row => row.id === image.id);
      if (index === -1) {
        return [image, ...existing];
      }
      const clone = [...existing];
      clone[index] = image;
      return clone;
    });
  }
}

function hasPersonalityAssociation(image: FileAttachment): boolean {
  if (image.personality_id) return true;
  return (image.personalities ?? []).length > 0;
}

/** The server's message for a refused move (it says why), else a plain fallback. */
function describeFolderError(error: unknown, fallback: string): string {
  const message = (error as { error?: { error?: unknown } } | null)?.error?.error;
  return typeof message === 'string' && message.trim() !== '' ? message : fallback;
}

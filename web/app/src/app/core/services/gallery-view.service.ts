import { computed, Injectable, signal, inject } from '@angular/core';
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
    this.currentPage.set(1);
    this.error.set(null);
    this.isLoading.set(true);
    const activeFilters = this.filters();
    const associationMode = this.associationFilterMode();
    const personalityId = activeFilters.personalityId === 'all' ? undefined : activeFilters.personalityId;
    this.galleryService
      .listImages(1, this.pageSize, { name: activeFilters.query, personalityId, globalOnly: associationMode === 'global' })
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
    this.galleryService
      .listImages(nextPage, this.pageSize, { name: activeFilters.query, personalityId, globalOnly: associationMode === 'global' })
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
    this.filters.update(current => ({ ...current, ...partial }));
    this.loadInitial();
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

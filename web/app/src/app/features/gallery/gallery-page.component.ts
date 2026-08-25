import { ChangeDetectionStrategy, Component, computed, DestroyRef, effect, inject, OnInit, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';

import { ChatService } from '../../core/services/chat.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Personality, PersonalityExpression, UpdatePersonalityRequest } from '../../core/models/personality.model';
import { environment } from '../../../environments/environment';
import { GalleryFilters, toGalleryTileVm } from './helpers/gallery-vm.helpers';
import { AssignAsExpressionFlowComponent } from './components/assign-as-expression-flow.component';
import {
  GalleryFileImportRequest, GalleryImportModalComponent, GalleryImportPersonalityOption,
} from './components/gallery-import-modal.component';
import { GalleryPersonalityOption } from './components/gallery-filter-bar.component';
import { GalleryGridComponent } from './components/gallery-grid.component';
import { ImageDetailModalComponent } from './components/image-detail-modal.component';
import { PersonalityExpressionsManagerComponent } from '../personality/detail/personality-expressions-manager.component';
import { PersonalityMediaJobBannerComponent } from '../personality/components/personality-media-job-banner.component';
import { sourceForImage } from './helpers/image-source.helpers';
import { personalityAccent } from '../personality/helpers/personality-vm.helpers';
import { personalityCoverUrl } from '../personality/helpers/cover-image.helpers';

type GalleryMode = 'gallery' | 'expressions';
type GallerySort = 'created' | 'last_used';

@Component({
  selector: 'app-gallery-page',
  standalone: true,
  imports: [
    GalleryGridComponent,
    ImageDetailModalComponent,
    GalleryImportModalComponent,
    AssignAsExpressionFlowComponent,
    PersonalityExpressionsManagerComponent,
    PersonalityMediaJobBannerComponent,
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
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

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
    const sortBy = this.sort();
    return [...this.view.filteredImages()].sort((a, b) => {
      const dateA = Date.parse(a.created_at);
      const dateB = Date.parse(b.created_at);
      if (sortBy === 'last_used') {
        // Last-used metadata is not yet exposed by the API; use creation time fallback.
        return (dateA - dateB) * direction;
      }
      return (dateA - dateB) * direction;
    });
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
  private readonly syncImportRequest = effect(() => {
    const requestTick = this.view.importRequestTick();
    if (requestTick === 0) return;
    this.importOpen.set(true);
  });

  ngOnInit(): void {
    this.view.setMode(this.mode());
    this.view.loadInitial();
    this.loadPersonalities();
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
      });
  }

  onFilterChange(partial: Partial<GalleryFilters>): void {
    this.view.setFilters(partial);
  }

  setMode(mode: GalleryMode): void {
    this.mode.set(mode);
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
    const request: UpdatePersonalityRequest = {
      name: personality.name,
      system_prompt: personality.system_prompt,
      auto_pin_memories: personality.auto_pin_memories,
      cover_image_id: personality.cover_image_id,
      scratchpad: personality.scratchpad,
      scratchpad_update_prompt: personality.scratchpad_update_prompt,
      expressions_enabled: enabled,
      image_style: personality.image_style,
    };
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

  onAddToThread(payload: { imageId: string; chatId: string }): void {
    this.onCloseDetail();
    void this.router.navigate(['/chat', payload.chatId], {
      queryParams: { galleryImageId: payload.imageId },
    });
  }

  onStartNewChat(imageId: string): void {
    this.onCloseDetail();
    this.chatService.createChat({ name: 'New Chat' }).subscribe({
      next: chat => {
        void this.router.navigate(['/chat', chat.id], {
          queryParams: { galleryImageId: imageId },
        });
      },
      error: async () => {
        await this.confirmationService.alert({
          title: 'Could not start thread',
          message: 'Failed to create a new chat. Please try again from the chat page.',
          type: 'danger',
        });
      },
    });
  }

  async onDelete(imageId: string): Promise<void> {
    const image = this.view.images().find(row => row.id === imageId);
    if (!image) return;
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

  openImportModal(): void {
    this.importOpen.set(true);
  }

  closeImportModal(): void {
    this.importOpen.set(false);
  }

  onImportFile(request: GalleryFileImportRequest): void {
    this.importSubmitting.set(true);
    const import$ = request.scope === 'global'
      ? this.galleryService.importImage(request.file, { title: request.title, description: request.description })
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
        message: 'Please choose a personality for "Pin to character" imports.',
        type: 'danger',
      });
      return;
    }
    import$.subscribe({
      next: uploaded => {
        this.importSubmitting.set(false);
        this.importOpen.set(false);
        this.view.upsertImage(uploaded);
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

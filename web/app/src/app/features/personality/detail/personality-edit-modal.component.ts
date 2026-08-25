import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { AsyncPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { finalize, take } from 'rxjs/operators';

import {
  Personality,
  PersonalityThumbnailCircle,
  PromptDefaults,
  UpdatePersonalityRequest,
} from '../../../core/models/personality.model';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ModalComponent, ModalDismissReason } from '../../../shared/ui/modal/modal.component';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { ArrowRightIconComponent, BellIconComponent, BrainIconComponent } from '../../../shared/ui/icons/icons';
import { PersonalityAttachmentsListComponent } from './personality-attachments-list.component';
import { PersonalityPortraitFocusEditorComponent } from './personality-portrait-focus-editor.component';
import {
  TEXT_LIMIT_HARD_MAX,
  TEXT_LIMIT_WARNING_THRESHOLD,
} from '../../../core/constants/text-limits.constants';

interface PersonalityEditDraft {
  name: string;
  system_prompt: string;
  scratchpad: string;
  scratchpad_update_prompt: string;
  auto_pin_memories: boolean;
  accent_color: string | null;
  cover_image_id: string | null;
  thumbnail_circle: PersonalityThumbnailCircle | null;
}

const PORTRAIT_EDITOR_WIDTH = 200;
const PORTRAIT_EDITOR_HEIGHT = 267;
const THUMBNAIL_PREVIEW_SIZE = 96;
const DEFAULT_THUMBNAIL_CIRCLE: PersonalityThumbnailCircle = { cx: 0.5, cy: 0.42, r: 0.34 };

@Component({
  selector: 'app-personality-edit-modal',
  standalone: true,
  imports: [
    AsyncPipe,
    AuthImagePipe,
    FormsModule,
    ModalComponent,
    PersonalityAttachmentsListComponent,
    PersonalityPortraitFocusEditorComponent,
    BellIconComponent,
    BrainIconComponent,
    ArrowRightIconComponent,
  ],
  template: `
    <ui-modal [open]="open()" [labelledBy]="titleId" size="lg" (dismiss)="onModalDismiss($event)">
      <div modal-header>
        <h2 [id]="titleId" class="text-base font-semibold text-(--color-text-primary)">Edit Personality</h2>
      </div>

      @if (draft(); as value) {
        <div class="flex flex-col gap-5">
          <section class="rounded-xl bg-(--color-surface-elevated) p-3">
            <div class="flex flex-wrap items-start gap-4">
              <app-personality-portrait-focus-editor
                [imageUrl]="(coverImageUrl() | authImage | async) ?? null"
                [circle]="value.thumbnail_circle"
                (circleChange)="onCircleChange($event)"
              />
              <div class="flex min-w-[13rem] flex-1 flex-col gap-2">
                <p class="text-xs font-semibold text-(--color-text-secondary)">Thumbnail preview</p>
                <div
                  class="relative h-24 w-24 overflow-hidden rounded-full border-2 shadow-[0_2px_12px_rgba(0,0,0,0.25)]"
                  [style.border-color]="value.accent_color || 'rgb(194,87,42)'"
                >
                  @if (coverImageUrl(); as url) {
                    @if ((url | authImage | async); as previewUrl) {
                      <div
                        class="absolute left-0 top-0 h-[16.6875rem] w-[12.5rem]"
                        [style.transform]="thumbnailPreviewTransform()"
                        style="transform-origin: top left;"
                      >
                        <img class="h-full w-full object-cover object-top" [src]="previewUrl" alt="" />
                      </div>
                    }
                  } @else {
                    <div class="flex h-full w-full items-center justify-center bg-(--color-surface-input) text-xs text-(--color-text-muted)">No image</div>
                  }
                </div>
                <div class="flex w-full flex-col gap-2">
                  <input
                    #coverFileInput
                    type="file"
                    accept="image/*"
                    class="hidden"
                    (change)="onCoverFileSelected($event)"
                  />
                  <button
                    type="button"
                    class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-1.5 text-xs text-(--color-text-secondary) hover:bg-(--color-surface-card)"
                    [disabled]="coverUploading()"
                    (click)="uploadCoverPhoto()"
                  >{{ coverUploading() ? 'Uploading…' : 'Upload photo' }}</button>
                  <button
                    type="button"
                    class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-1.5 text-xs text-(--color-text-secondary) hover:bg-(--color-surface-card)"
                    (click)="loadGallery()"
                  >Select from gallery</button>
                  <button
                    type="button"
                    class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-1.5 text-xs text-(--color-text-secondary) hover:bg-(--color-surface-card)"
                    (click)="manageExpressions()"
                  >Manage Expressions</button>
                  <button
                    type="button"
                    class="w-full rounded-md border border-red-600/40 bg-red-500/15 px-3 py-1.5 text-xs font-semibold text-red-700 hover:bg-red-500/25 dark:text-red-300 dark:hover:bg-red-500/20"
                    [disabled]="deleting() || saving()"
                    (click)="confirmDelete()"
                  >{{ deleting() ? 'Deleting…' : 'Delete personality' }}</button>
                </div>
                @if (galleryOpen()) {
                  <section class="rounded-lg border border-border-base bg-(--color-surface-elevated) p-3">
                    <p class="mb-2 text-sm font-medium text-(--color-text-primary)">Choose Portrait</p>
                    @if (galleryLoading()) {
                      <p class="text-xs text-(--color-text-secondary)">Loading gallery…</p>
                    } @else {
                      <div class="grid gap-2" style="grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));">
                        @for (image of galleryImages(); track image.id) {
                          <button
                            type="button"
                            class="aspect-square overflow-hidden rounded-md border border-border-base"
                            (click)="selectCover(image)"
                          >
                            <img
                              class="h-full w-full object-cover"
                              [src]="(imageThumbUrl(image) | authImage | async) ?? ''"
                              [alt]="image.name"
                            />
                          </button>
                        }
                      </div>
                    }
                  </section>
                }
                <p class="text-[11px] text-(--color-text-muted)">Drag to reposition · corner handle to resize.</p>
              </div>
            </div>
          </section>

          <section class="flex flex-col gap-3">
            <label class="flex flex-col gap-1">
              <span class="text-xs font-semibold text-(--color-text-secondary)">Name</span>
              <input
                type="text"
                class="w-full rounded-md border border-border-base bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
                [ngModel]="value.name"
                (ngModelChange)="setDraftField('name', $event)"
              />
            </label>

            <div class="flex flex-col gap-1">
              <p class="text-xs font-semibold text-(--color-text-secondary)">Accent Color</p>
              <div class="flex flex-wrap items-center gap-2">
                @for (preset of accentPresets; track preset) {
                  <button
                    type="button"
                    class="h-6 w-6 rounded-full shadow-sm"
                    [style.background]="preset"
                    [attr.aria-label]="'Set accent color ' + preset"
                    (click)="setDraftField('accent_color', preset)"
                  ></button>
                }
                <input
                  type="color"
                  class="h-8 w-10 rounded bg-(--color-surface-input) p-1"
                  [ngModel]="value.accent_color ?? '#4f46e5'"
                  (ngModelChange)="setDraftField('accent_color', $event)"
                />
              </div>
              <span class="text-[11px] text-(--color-text-muted)">Pick a preset or choose a custom color</span>
            </div>

            <label class="flex flex-col gap-1">
              <span class="text-xs font-semibold text-(--color-text-secondary)">System Prompt</span>
              <textarea
                rows="6"
                class="w-full resize-y rounded-lg border border-border-base bg-(--color-surface-input) px-3 py-2 text-sm leading-relaxed text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
                [ngModel]="value.system_prompt"
                (ngModelChange)="setDraftField('system_prompt', $event)"
              ></textarea>
              <div
                class="flex items-center justify-between text-xs text-(--color-text-secondary)"
                [class.text-amber-500]="systemPromptNearLimit()"
                [class.text-red-500]="systemPromptOverLimit()"
              >
                <span>{{ systemPromptCountLabel() }} / {{ systemPromptHardLimitLabel }} characters</span>
                @if (systemPromptOverLimit()) {
                  <span role="alert">Prompt exceeds max length.</span>
                } @else if (systemPromptNearLimit()) {
                  <span>Large prompts may degrade output quality.</span>
                }
              </div>
            </label>

            <div class="flex flex-col gap-1">
              <span class="text-xs font-semibold text-(--color-text-secondary)">Scratchpad</span>
              <textarea
                rows="4"
                class="w-full resize-y rounded-lg border border-border-base bg-(--color-surface-input) px-3 py-2 text-sm leading-relaxed text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
                [ngModel]="value.scratchpad"
                (ngModelChange)="setDraftField('scratchpad', $event)"
              ></textarea>

              <div class="rounded-lg border border-border-base bg-(--color-surface-elevated)">
                <button
                  type="button"
                  class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-xs font-semibold text-(--color-text-secondary) hover:text-(--color-text-primary)"
                  (click)="toggleScratchpadAdvanced()"
                >
                  <span>Advanced scratchpad settings</span>
                  <span class="text-[10px] opacity-60">{{ scratchpadAdvancedOpen() ? '▲' : '▼' }}</span>
                </button>

                @if (scratchpadAdvancedOpen()) {
                  <div class="flex flex-col gap-4 border-t border-border-base px-3 pb-3 pt-3">

                    @if (scratchpadHistory().length > 0) {
                      <div class="flex flex-col gap-2">
                        <p class="text-xs font-semibold text-(--color-text-secondary)">Revert to a previous version</p>
                        <ul class="flex flex-col divide-y divide-border-base rounded-lg border border-border-base">
                          @for (version of scratchpadHistory(); track $index; let i = $index) {
                            <li class="flex items-center justify-between gap-3 px-3 py-2">
                              <p class="min-w-0 flex-1 truncate text-xs text-(--color-text-secondary)" [title]="version">
                                <span class="mr-1.5 font-semibold text-(--color-text-primary)">v{{ scratchpadHistory().length - i }}</span>{{ version || '(empty)' }}
                              </p>
                              <button
                                type="button"
                                class="shrink-0 rounded-md border border-border-base bg-(--color-surface-input) px-2 py-0.5 text-xs text-(--color-text-primary) hover:bg-(--color-surface-card)"
                                (click)="revertScratchpad(version)"
                              >Restore</button>
                            </li>
                          }
                        </ul>
                      </div>
                    }

                    <label class="flex flex-col gap-1">
                      <span class="text-xs font-semibold text-(--color-text-secondary)">Custom scratchpad update prompt</span>
                      @if (promptDefaultsLoading()) {
                        <p class="text-xs text-(--color-text-muted)">Loading default…</p>
                      } @else if (promptDefaultsError()) {
                        <p class="text-xs text-red-500">Failed to load the system default. You can still enter a custom prompt below.</p>
                      } @else {
                        <textarea
                          rows="5"
                          class="w-full resize-y rounded-lg border border-border-base bg-(--color-surface-input) px-3 py-2 font-mono text-xs leading-relaxed text-(--color-text-primary) outline-none focus:border-accent focus:ring-1 focus:ring-accent"
                          [placeholder]="scratchpadUpdatePromptPlaceholder()"
                          [ngModel]="value.scratchpad_update_prompt"
                          (ngModelChange)="setDraftField('scratchpad_update_prompt', $event)"
                        ></textarea>
                        <span class="text-[11px] text-(--color-text-muted)">Leave blank to use the system default. Overrides the prompt the agent receives when writing a scratchpad checkpoint.</span>
                      }
                    </label>

                  </div>
                }
              </div>
            </div>
          </section>

          <section
            class="flex items-start gap-3 rounded-xl border border-(--color-border-default) bg-(--color-surface-elevated) p-3"
          >
            <span class="mt-0.5 shrink-0 text-amber-400" aria-hidden="true">
              <ui-bell-icon [size]="18" />
            </span>
            <div class="min-w-0 flex-1">
              <p class="text-sm font-semibold text-(--color-text-primary)">Auto-pin new User memories</p>
              <p class="mt-1 text-xs leading-relaxed text-(--color-text-secondary)">
                When enabled, new User-scoped memories created while this personality is active will automatically be pinned to this personality, making them accessible only to this personality.
              </p>
            </div>
            <button
              type="button"
              role="switch"
              class="relative mt-1 h-6 w-11 shrink-0 rounded-full transition-colors"
              [class.bg-(--color-accent)]="value.auto_pin_memories"
              [class.bg-(--color-surface-input)]="!value.auto_pin_memories"
              [attr.aria-checked]="value.auto_pin_memories"
              [attr.aria-label]="'Auto-pin new User memories'"
              (click)="setDraftField('auto_pin_memories', !value.auto_pin_memories)"
            >
              <span
                class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform"
                [class.translate-x-5]="value.auto_pin_memories"
              ></span>
            </button>
          </section>

          @if (personality(); as current) {
            <div class="flex flex-col gap-1">
              <span class="text-xs font-semibold text-(--color-text-secondary)">Attached Files</span>
              <app-personality-attachments-list [personalityId]="current.id" [borderless]="true" />
            </div>
          }
        </div>
      }

      <div modal-footer class="flex w-full items-center justify-between gap-2">
        <button
          type="button"
          class="inline-flex items-center gap-1.5 rounded-lg border border-border-base bg-transparent px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
          (click)="viewMemories()"
        >
          <ui-brain-icon [size]="12" />
          <span>View Memories</span>
          <ui-arrow-right-icon [size]="12" />
        </button>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="rounded-lg border border-border-base bg-transparent px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
            (click)="cancel()"
          >Cancel</button>
          <button
            type="button"
            class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"
            [disabled]="saving() || deleting() || !draft() || systemPromptOverLimit()"
            (click)="save()"
          >{{ saving() ? 'Saving…' : 'Save Changes' }}</button>
        </div>
      </div>
    </ui-modal>

    <ui-modal
      [open]="systemPromptWarningOpen()"
      [labelledBy]="systemPromptWarningTitleId"
      size="md"
      (dismiss)="systemPromptWarningOpen.set(false)"
    >
      <div modal-header>
        <h2 [id]="systemPromptWarningTitleId" class="text-base font-semibold text-(--color-text-primary)">
          Large prompt warning
        </h2>
      </div>
      <p class="text-sm text-(--color-text-secondary)">
        This system prompt is over {{ systemPromptWarningLimitLabel }} characters and may reduce response quality.
        Save anyway?
      </p>
      <div modal-footer class="flex justify-end gap-2">
        <button
          type="button"
          class="rounded-lg border border-border-base bg-transparent px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
          (click)="systemPromptWarningOpen.set(false)"
        >Cancel</button>
        <button
          type="button"
          class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95"
          (click)="confirmWarningAndSave()"
        >Save anyway</button>
      </div>
    </ui-modal>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityEditModalComponent {
  readonly open = input(false);
  readonly personality = input<Personality | null>(null);

  readonly dismissed = output<void>();
  readonly saved = output<Personality>();
  readonly deleted = output<string>();

  private readonly personalityService = inject(PersonalityService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly fileAttachmentService = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);
  private readonly coverFileInput = viewChild<ElementRef<HTMLInputElement>>('coverFileInput');

  readonly titleId = `personality-edit-modal-${Math.random().toString(36).slice(2, 10)}`;
  readonly systemPromptWarningTitleId = `personality-edit-warning-${Math.random().toString(36).slice(2, 10)}`;
  readonly draft = signal<PersonalityEditDraft | null>(null);
  readonly saving = signal(false);
  readonly deleting = signal(false);

  readonly galleryOpen = signal(false);
  readonly galleryLoading = signal(false);
  readonly galleryImages = signal<FileAttachment[]>([]);
  readonly coverUploading = signal(false);

  readonly scratchpadAdvancedOpen = signal(false);
  readonly promptDefaults = signal<PromptDefaults | null>(null);
  readonly promptDefaultsLoading = signal(false);
  readonly promptDefaultsError = signal(false);

  readonly scratchpadHistory = computed(() => this.personality()?.scratchpad_history ?? []);
  readonly systemPromptHardLimitLabel = TEXT_LIMIT_HARD_MAX.toLocaleString();
  readonly systemPromptWarningLimitLabel = TEXT_LIMIT_WARNING_THRESHOLD.toLocaleString();
  readonly systemPromptCount = computed(() => this.draft()?.system_prompt.length ?? 0);
  readonly systemPromptCountLabel = computed(() => this.systemPromptCount().toLocaleString());
  readonly systemPromptOverLimit = computed(() => this.systemPromptCount() > TEXT_LIMIT_HARD_MAX);
  readonly systemPromptNearLimit = computed(
    () => this.systemPromptCount() >= TEXT_LIMIT_WARNING_THRESHOLD && !this.systemPromptOverLimit(),
  );
  readonly systemPromptWarningOpen = signal(false);
  private readonly systemPromptWarningAcknowledged = signal(false);
  readonly scratchpadUpdatePromptPlaceholder = computed(() => {
    const defaults = this.promptDefaults();
    if (!defaults) return 'Leave blank to use the system default…';
    const text = defaults.scratchpad_update_prompt ?? '';
    if (!text) return 'Leave blank to use the system default…';
    const preview = text.slice(0, 120);
    return `Default: ${preview}${text.length > 120 ? '…' : ''}`;
  });

  readonly accentPresets = ['#C2572A', '#7A5AF8', '#0EA5E9', '#22C55E', '#F97316', '#EC4899'];

  readonly coverImageUrl = computed(() => {
    const coverId = this.draft()?.cover_image_id;
    if (!coverId) return null;
    return this.imageGallery.getImageUrl(coverId, 'full');
  });
  readonly thumbnailPreviewTransform = computed(() => {
    const circle = this.draft()?.thumbnail_circle;
    if (!circle || circle.r <= 0) return null;
    const radiusPx = circle.r * Math.min(PORTRAIT_EDITOR_WIDTH, PORTRAIT_EDITOR_HEIGHT);
    if (radiusPx <= 0) return null;
    const scale = (THUMBNAIL_PREVIEW_SIZE / 2) / radiusPx;
    const tx = THUMBNAIL_PREVIEW_SIZE / 2 - circle.cx * PORTRAIT_EDITOR_WIDTH * scale;
    const ty = THUMBNAIL_PREVIEW_SIZE / 2 - circle.cy * PORTRAIT_EDITOR_HEIGHT * scale;
    return `translate(${tx.toFixed(2)}px, ${ty.toFixed(2)}px) scale(${scale.toFixed(4)})`;
  });

  constructor() {
    effect(() => {
      const open = this.open();
      const personality = this.personality();
      if (!open || !personality) return;
      this.draft.set({
        name: personality.name,
        system_prompt: personality.system_prompt,
        scratchpad: personality.scratchpad ?? '',
        scratchpad_update_prompt: personality.scratchpad_update_prompt ?? '',
        auto_pin_memories: personality.auto_pin_memories,
        accent_color: personality.accent_color ?? null,
        cover_image_id: personality.cover_image_id ?? null,
        thumbnail_circle: personality.thumbnail_circle ?? DEFAULT_THUMBNAIL_CIRCLE,
      });
      this.scratchpadAdvancedOpen.set(false);
    });
  }

  /** True when the draft differs from the personality it was hydrated from. */
  readonly isDirty = computed(() => {
    const draft = this.draft();
    const original = this.personality();
    if (!draft || !original) return false;
    const originalCircle = original.thumbnail_circle ?? DEFAULT_THUMBNAIL_CIRCLE;
    return (
      draft.name !== original.name ||
      draft.system_prompt !== original.system_prompt ||
      (draft.scratchpad ?? '') !== (original.scratchpad ?? '') ||
      (draft.scratchpad_update_prompt ?? '') !== (original.scratchpad_update_prompt ?? '') ||
      draft.auto_pin_memories !== original.auto_pin_memories ||
      (draft.accent_color ?? null) !== (original.accent_color ?? null) ||
      (draft.cover_image_id ?? null) !== (original.cover_image_id ?? null) ||
      !thumbnailCirclesEqual(draft.thumbnail_circle, originalCircle)
    );
  });

  onModalDismiss(reason: ModalDismissReason): void {
    // The explicit close button dismisses immediately; backdrop/Escape are
    // guarded so an accidental click/keypress can't discard unsaved edits.
    if (reason === 'close-button') {
      this.cancel();
      return;
    }
    void this.requestSoftDismiss();
  }

  private async requestSoftDismiss(): Promise<void> {
    if (this.isDirty() && !(await this.confirmationService.confirmDiscardChanges())) {
      return;
    }
    this.cancel();
  }

  setDraftField<K extends keyof PersonalityEditDraft>(key: K, value: PersonalityEditDraft[K]): void {
    this.draft.update(current => current ? { ...current, [key]: value } : current);
    if (key === 'system_prompt') {
      this.systemPromptWarningAcknowledged.set(false);
      if (this.systemPromptWarningOpen()) {
        this.systemPromptWarningOpen.set(false);
      }
    }
  }

  onCircleChange(circle: PersonalityThumbnailCircle): void {
    this.setDraftField('thumbnail_circle', circle);
  }

  uploadCoverPhoto(): void {
    this.coverFileInput()?.nativeElement.click();
  }

  onCoverFileSelected(event: Event): void {
    const personality = this.personality();
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!personality || !file) {
      return;
    }
    this.coverUploading.set(true);
    this.fileAttachmentService
      .uploadPersonalityFileAttachment(personality.id, file)
      .pipe(
        take(1),
        finalize(() => this.coverUploading.set(false)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: attachment => {
          this.setDraftField('cover_image_id', attachment.id);
          input.value = '';
        },
        error: () => {
          input.value = '';
          void this.confirmationService.alert({
            message: 'Failed to upload photo. Please try again.',
            type: 'danger',
          });
        },
      });
  }

  loadGallery(): void {
    this.galleryOpen.set(true);
    this.galleryLoading.set(true);
    this.imageGallery.listImages(1, 40).subscribe({
      next: response => {
        this.galleryImages.set(response.results ?? []);
        this.galleryLoading.set(false);
      },
      error: () => {
        this.galleryImages.set([]);
        this.galleryLoading.set(false);
      },
    });
  }

  imageThumbUrl(image: FileAttachment): string {
    return this.imageGallery.getImageUrl(image.id, 'thumbnail');
  }

  selectCover(image: FileAttachment): void {
    this.setDraftField('cover_image_id', image.id);
    this.galleryOpen.set(false);
  }

  manageExpressions(): void {
    const personalityId = this.personality()?.id;
    this.dismissed.emit();
    void this.router.navigate(['/gallery'], {
      queryParams: {
        mode: 'expressions',
        ...(personalityId ? { personality_id: personalityId } : {}),
      },
    });
  }

  async confirmDelete(): Promise<void> {
    const personality = this.personality();
    if (!personality || this.deleting()) return;
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete personality',
      message: 'Are you sure? This will permanently delete this Personality.',
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.deleting.set(true);
    this.personalityService.deletePersonality(personality.id).subscribe({
      next: () => {
        this.deleting.set(false);
        this.deleted.emit(personality.id);
        this.dismissed.emit();
      },
      error: async () => {
        this.deleting.set(false);
        await this.confirmationService.alert({
          message: 'Failed to delete personality. Please try again.',
          type: 'danger',
        });
      },
    });
  }

  toggleScratchpadAdvanced(): void {
    const opening = !this.scratchpadAdvancedOpen();
    this.scratchpadAdvancedOpen.set(opening);
    if (opening && !this.promptDefaults() && !this.promptDefaultsLoading()) {
      this.promptDefaultsLoading.set(true);
      this.promptDefaultsError.set(false);
      this.personalityService.getPromptDefaults().pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
        next: defaults => {
          this.promptDefaults.set(defaults);
          this.promptDefaultsLoading.set(false);
        },
        error: err => {
          console.error('Failed to load prompt defaults', err);
          this.promptDefaultsLoading.set(false);
          this.promptDefaultsError.set(true);
        },
      });
    }
  }

  revertScratchpad(content: string): void {
    this.setDraftField('scratchpad', content);
  }

  viewMemories(): void {
    const personalityId = this.personality()?.id;
    void this.router.navigate(['/memories'], {
      queryParams: personalityId ? { personality_id: personalityId } : undefined,
    });
  }

  cancel(): void {
    this.galleryOpen.set(false);
    this.dismissed.emit();
  }

  save(): void {
    const personality = this.personality();
    const draft = this.draft();
    if (!personality || !draft) return;
    if (this.systemPromptOverLimit()) {
      return;
    }
    if (this.systemPromptNearLimit() && !this.systemPromptWarningAcknowledged()) {
      this.systemPromptWarningOpen.set(true);
      return;
    }
    const request: UpdatePersonalityRequest = {
      name: draft.name.trim(),
      system_prompt: draft.system_prompt.trim(),
      scratchpad: draft.scratchpad,
      scratchpad_update_prompt: (draft.scratchpad_update_prompt ?? '').trim(),
      auto_pin_memories: draft.auto_pin_memories,
      expressions_enabled: personality.expressions_enabled,
      image_style: personality.image_style,
      cover_image_id: draft.cover_image_id,
      accent_color: draft.accent_color,
      thumbnail_circle: draft.thumbnail_circle,
      memory_search_prompt: personality.memory_search_prompt,
      memory_write_prompt: personality.memory_write_prompt,
      archival_model: personality.archival_model,
    };
    this.saving.set(true);
    this.personalityService.updatePersonality(personality.id, request).subscribe({
      next: updated => {
        this.saving.set(false);
        this.saved.emit(updated);
        this.dismissed.emit();
      },
      error: () => {
        this.saving.set(false);
      },
    });
  }

  confirmWarningAndSave(): void {
    this.systemPromptWarningAcknowledged.set(true);
    this.systemPromptWarningOpen.set(false);
    this.save();
  }
}

function thumbnailCirclesEqual(
  a: PersonalityThumbnailCircle | null,
  b: PersonalityThumbnailCircle | null,
): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return a.cx === b.cx && a.cy === b.cy && a.r === b.r;
}

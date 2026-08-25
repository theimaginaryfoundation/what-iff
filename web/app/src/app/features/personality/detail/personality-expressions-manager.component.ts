import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  input,
  OnInit,
  output,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NgClass } from '@angular/common';
import { FormsModule } from '@angular/forms';

import {
  PersonalityExpression,
  UpdatePersonalityExpressionRequest,
} from '../../../core/models/personality.model';
import {
  ExpressionAssignmentService,
  OptimisticContext,
} from '../../../core/services/expression-assignment.service';
import {
  isDefaultExpressionGridComplete,
  isValidExpressionKey,
  MergedExpression,
  slotsFromPersistedExpressions,
} from '../helpers/expressions.helpers';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { AsyncPipe } from '@angular/common';

/**
 * Renders persisted expression slots only (no client-side default placeholders).
 * Users add keys manually or run **Generate default expressions** to create the 3×3 grid.
 * Optimistic UI is delegated to `ExpressionAssignmentService`.
 *
 * Default-grid POST uses nano likeness plus one medium-quality image (~$0.01), not
 * quota-metered; the server skips when the grid is already complete unless force=true.
 */
@Component({
  selector: 'app-personality-expressions-manager',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe, NgClass, FormsModule, ModalComponent],
  template: `
    <section
      class="expressions-manager flex flex-col gap-3 rounded-xl border bg-(--color-surface-card) p-4"
      [style.border-color]="accentBorder()"
      [style.--expressions-accent]="accent()"
      aria-label="Personality expressions"
    >
      <header class="flex flex-wrap items-center justify-between gap-2">
        <div class="min-w-0">
          @if (personalityName()?.trim()) {
            <h2 class="truncate text-[0.9375rem] font-bold" [style.color]="accent()">{{ personalityName() }}</h2>
          } @else {
            <h2 class="text-base font-semibold text-(--color-text-primary)">Expressions</h2>
          }
          <p class="text-xs text-(--color-text-secondary)">
            {{ slots().length }} slot{{ slots().length === 1 ? '' : 's' }} ·
            {{ missingCount() }} unset
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <button
            type="button"
            class="inline-flex items-center rounded-lg border border-border-base bg-(--color-surface-base) px-3 py-1.5 text-sm font-semibold text-(--color-text-primary) disabled:opacity-50"
            [disabled]="gridGenerating()"
            [title]="defaultGridComplete() ? 'Re-run image generation for all nine default slots (recreates any deleted defaults)' : 'Create the nine default expression slots and generate their images'"
            (click)="onGenerateDefaultGrid(defaultGridComplete())"
          >{{ gridButtonLabel() }}</button>
          <button
            type="button"
            class="inline-flex items-center rounded-lg px-3 py-1.5 text-sm font-semibold"
            style="background: color-mix(in srgb, var(--expressions-accent) 16%, transparent); color: var(--expressions-accent);"
            [disabled]="gridGenerating()"
            (click)="onCreateCustomKey()"
          >+ Add expression</button>

          <span class="h-5 w-px bg-(--color-border-base)" aria-hidden="true"></span>

          <button
            type="button"
            role="switch"
            class="relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-50"
            [class.bg-(--color-accent)]="expressionsEnabled()"
            [class.bg-(--color-surface-input)]="!expressionsEnabled()"
            [attr.aria-checked]="expressionsEnabled()"
            [attr.aria-label]="'Expression images in chat'"
            [title]="expressionsEnabled() ? 'Hide expression images in chat' : 'Show expression images in chat'"
            [disabled]="expressionsToggleUpdating()"
            (click)="onToggleExpressionsEnabled()"
          >
            <span
              class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform"
              [class.translate-x-5]="expressionsEnabled()"
            ></span>
          </button>
          <span
            class="text-xs font-medium"
            [class.text-(--color-text-secondary)]="expressionsEnabled()"
            [class.text-amber-600]="!expressionsEnabled()"
            [class.dark:text-amber-400]="!expressionsEnabled()"
          >Images in chat</span>
        </div>
      </header>

      @if (errorMessage()) {
        <p class="rounded-md border border-red-500 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">{{ errorMessage() }}</p>
      }

      @if (!expressionsEnabled()) {
        <p class="rounded-md border border-amber-400/40 bg-amber-50/60 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-400" role="status">
          Expression images are hidden in chat. Toggle "Images in chat" to re-enable them.
        </p>
      }

      <div [ngClass]="{'opacity-40 pointer-events-none select-none': !expressionsEnabled()}">
        @if (slots().length === 0) {
          <p class="rounded-md border border-border-base bg-(--color-surface-input) px-3 py-3 text-sm text-(--color-text-secondary)" role="status">
            No expression slots yet. Use <strong class="font-semibold text-(--color-text-primary)">Generate default expressions</strong> for a starter 3×3 grid, or <strong class="font-semibold text-(--color-text-primary)">Add expression</strong> to define your own keys.
          </p>
        }

        <ul class="expressions-grid grid gap-2" style="grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));" role="list">
          @for (slot of visibleSlots(); track slot.expressionKey) {
            <li
              class="expression-slot group relative overflow-hidden rounded-md border border-border-base bg-(--color-surface-base)"
              role="listitem"
              [attr.aria-label]="slotAriaLabel(slot)"
            >
              <div
                class="w-full border-b border-border-base bg-(--color-surface-input) px-2 py-1 text-center text-[0.8125rem] font-medium text-(--color-text-primary) truncate"
                [title]="slot.expressionKey"
              >{{ slot.expressionKey }}</div>
              <div class="relative aspect-square bg-(--color-surface-elevated)">
                @if (slotImageUrl(slot); as imageUrl) {
                  <img
                    class="h-full w-full object-cover"
                    [src]="(imageUrl | authImage | async) ?? ''"
                    [alt]="slot.label || slot.expressionKey"
                    loading="lazy"
                    decoding="async"
                  />
                } @else {
                  <div class="flex h-full w-full items-center justify-center text-[0.6875rem] text-(--color-text-muted)">
                    No image
                  </div>
                }
                <div class="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/55 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
                  <button
                    type="button"
                    class="rounded-md border border-border-base bg-(--color-surface-base) px-2 py-1 text-[0.6875rem] font-semibold tracking-wide text-(--color-text-primary)"
                    (click)="onAssign(slot)"
                  >REPLACE</button>
                  @if (slot.imageId) {
                    <button
                      type="button"
                      class="rounded-md border border-white/25 bg-black/25 px-2 py-1 text-[0.625rem] text-white"
                      (click)="onClear(slot)"
                    >CLEAR</button>
                  }
                </div>
              </div>
              <input
                type="text"
                class="w-full rounded-none border-x-0 border-b-0 border-t border-border-base bg-(--color-surface-input) px-2 py-1.5 text-[0.6875rem] text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-1 focus:ring-(--color-accent)"
                [placeholder]="'Label / when to use (optional)'"
                [ngModel]="slot.label ?? ''"
                (ngModelChange)="onLabelChange(slot, $event)"
                (blur)="onLabelCommit(slot)"
              />
              @if (slot.expressionKey !== 'thinking') {
                <button
                  type="button"
                  class="absolute right-1 top-1 rounded bg-black/45 px-1 text-[0.625rem] text-white opacity-0 transition-opacity group-hover:opacity-100"
                  [attr.aria-label]="'Remove ' + slot.expressionKey"
                  (click)="onRemove(slot)"
                >✕</button>
              }
            </li>
          }
        </ul>

        @if (hasHiddenSlots() || showAll()) {
          <div class="flex justify-center">
            <button
              type="button"
              class="inline-flex items-center gap-1.5 rounded-lg px-3 py-1 text-xs"
              style="color: var(--expressions-accent);"
              (click)="toggleShowAll()"
            >
              {{ showAll() ? 'Show less ▲' : 'Show all ' + slots().length + ' ▼' }}
            </button>
          </div>
        }
      </div>
    </section>

    <ui-modal [open]="isCustomKeyOpen()" [labelledBy]="customKeyLabelId" size="sm" (dismiss)="closeCustomKey()">
      <div modal-header>
        <h2 [id]="customKeyLabelId" class="text-base font-semibold text-(--color-text-primary)">Add expression key</h2>
      </div>
      <div class="flex flex-col gap-2">
        <label class="flex flex-col gap-1 text-sm">
          <span class="font-medium text-(--color-text-primary)">Key</span>
          <input
            type="text"
            class="rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 font-mono text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
            placeholder="e.g. mischievous"
            [(ngModel)]="customKeyDraft"
          />
          <span class="text-xs text-(--color-text-secondary)">Lowercase letters, digits, hyphens, or underscores; up to 64 characters.</span>
        </label>
        @if (customKeyError()) {
          <p class="text-xs text-red-500" role="alert">{{ customKeyError() }}</p>
        }
      </div>
      <div modal-footer class="flex justify-end gap-2">
        <button
          type="button"
          class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
          (click)="closeCustomKey()"
        >Cancel</button>
        <button
          type="button"
          class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"
          [disabled]="!customKeyDraft.trim()"
          (click)="submitCustomKey()"
        >Add</button>
      </div>
    </ui-modal>

    <ui-modal [open]="isGalleryOpen()" [labelledBy]="galleryLabelId" size="lg" (dismiss)="closeGallery()">
      <div modal-header>
        <div class="flex w-full items-center gap-3">
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <span class="h-[1.625rem] w-[1.625rem] overflow-hidden rounded-full border border-border-base bg-(--color-surface-input)">
              @if (personalityAvatarUrl(); as avatarUrl) {
                <img
                  class="h-full w-full object-cover object-top"
                  [src]="(avatarUrl | authImage | async) ?? ''"
                  [alt]="personalityName() || 'Personality avatar'"
                  loading="lazy"
                  decoding="async"
                />
              } @else {
                <span class="inline-flex h-full w-full items-center justify-center text-[0.625rem] font-semibold text-(--color-text-secondary)">
                  {{ (personalityName() || 'P').charAt(0).toUpperCase() }}
                </span>
              }
            </span>
            <div class="min-w-0">
              <div [id]="galleryLabelId" class="truncate text-sm font-bold text-(--color-text-primary)">Replace "{{ activeKey() }}"</div>
              <div class="mt-0.5 text-[0.6875rem] text-(--color-text-secondary)">Sorted by most recent activity. Pick any image to use in its place.</div>
            </div>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <button
              type="button"
              class="inline-flex items-center gap-2 rounded-md border border-border-base bg-transparent px-2.5 py-1.5 text-xs font-medium text-(--color-text-secondary)"
              title="Only show images pinned to this personality or from threads that include them"
              (click)="toggleGalleryPersonalityOnly()"
            >
              <span class="relative inline-flex h-3.5 w-6 shrink-0 rounded-full border border-border-base"
                [style.background]="galleryPersonalityOnly() ? 'var(--color-accent)' : 'var(--color-surface-muted)'">
                <span
                  class="absolute top-[1px] h-[9px] w-[9px] rounded-full bg-white shadow-[0_1px_2px_rgba(0,0,0,0.3)] transition-[left] duration-150"
                  [style.left]="galleryPersonalityOnly() ? '13px' : '1px'"
                ></span>
              </span>
              {{ personalityName() || 'Personality' }} only
            </button>
          </div>
        </div>
      </div>
      <div class="rounded-md border border-border-base bg-(--color-surface-input) px-3 py-2">
        <div class="relative">
          <span class="pointer-events-none absolute left-0 top-1/2 -translate-y-1/2 text-(--color-text-muted)">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/>
            </svg>
          </span>
          <input
            type="search"
            class="w-full bg-transparent pl-5 text-[0.8125rem] text-(--color-text-primary) outline-none placeholder:text-(--color-text-secondary)"
            [ngModel]="galleryQuery()"
            (ngModelChange)="galleryQuery.set($event)"
            placeholder="Search image titles and prompts…"
          />
        </div>
      </div>
      <div class="max-h-[34rem] overflow-y-auto px-0.5">
        @if (galleryLoading()) {
          <p class="p-4 text-sm text-(--color-text-secondary)" role="status">Loading gallery…</p>
        } @else if (filteredGalleryImages().length === 0) {
          <p class="p-4 text-sm text-(--color-text-secondary)">
            No matching images for this personality.
          </p>
        } @else {
          <ul class="grid gap-2 p-1" style="grid-template-columns: repeat(auto-fill, minmax(136px, 1fr));" role="list">
            @for (image of filteredGalleryImages(); track image.id) {
              <li role="listitem">
                <button
                  type="button"
                  class="group relative w-full overflow-hidden rounded-md border border-border-base bg-(--color-surface-base) outline-none focus-visible:ring-2 focus-visible:ring-(--color-accent)"
                  [attr.aria-label]="'Use ' + image.name"
                  (click)="onPickImage(image)"
                >
                  <img
                    class="aspect-square w-full object-cover"
                    [src]="(galleryThumbUrl(image) | authImage | async) ?? ''"
                    [alt]="image.name"
                    loading="lazy"
                    decoding="async"
                  />
                  <div class="flex items-center gap-1.5 px-2 py-1.5">
                    <span class="truncate text-[0.6875rem] font-semibold text-(--color-text-primary)">{{ image.name }}</span>
                  </div>
                </button>
              </li>
            }
          </ul>
        }
      </div>
      <div modal-footer class="flex w-full items-center justify-between gap-2">
        <span class="text-[0.6875rem] text-(--color-text-muted)">{{ filteredGalleryImages().length }} images</span>
        <button
          type="button"
          class="rounded-md border border-border-base px-3 py-1.5 text-xs text-(--color-text-secondary)"
          (click)="closeGallery()"
        >Cancel</button>
      </div>
    </ui-modal>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityExpressionsManagerComponent implements OnInit {
  readonly personalityId = input.required<string>();
  readonly expressions = input.required<readonly PersonalityExpression[]>();
  readonly personalityName = input<string | null>(null);
  readonly personalityAvatarUrl = input<string | null>(null);
  readonly accentColor = input<string | null>(null);
  readonly collapsedLimit = input(18);
  /** Whether expression picking is enabled. Defaults to true. */
  readonly expressionsEnabled = input<boolean>(true);
  /** True while parent persists a toggle change. */
  readonly expressionsToggleUpdating = input<boolean>(false);

  readonly expressionsChanged = output<readonly PersonalityExpression[]>();
  /** Emitted when the user toggles expression images in chat; parent should persist. */
  readonly expressionsEnabledChanged = output<boolean>();

  private readonly assignment = inject(ExpressionAssignmentService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly personalityApi = inject(PersonalityService);
  private readonly mediaJobs = inject(PersonalityMediaJobService);
  private readonly destroyRef = inject(DestroyRef);

  readonly customKeyLabelId = `expressions-add-key-${randomId()}`;
  readonly galleryLabelId = `expressions-pick-image-${randomId()}`;

  readonly errorMessage = signal<string | null>(null);
  readonly gridGenerating = signal(false);
  readonly showAll = signal(false);

  readonly slots = computed<MergedExpression[]>(() => slotsFromPersistedExpressions(this.expressions()));

  readonly missingCount = computed(() => this.slots().filter(s => !s.imageId).length);

  /** True when every canonical grid key has a row with an image (matches server skip logic). */
  readonly defaultGridComplete = computed(() => isDefaultExpressionGridComplete(this.expressions()));

  readonly gridButtonLabel = computed(() => {
    if (this.gridGenerating()) return 'Generating…';
    return this.defaultGridComplete() ? 'Regenerate default expressions' : 'Generate default expressions';
  });

  ngOnInit(): void {
    this.mediaJobs
      .refreshActiveJob()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(job => {
        if (job?.job_type === 'expression_grid' && job.personality_id === this.personalityId()) {
          if (job.status !== 'complete' && job.status !== 'failed') {
            this.gridGenerating.set(true);
            this.resumeGridPoll(job.job_id);
          }
        }
      });
  }
  readonly visibleSlots = computed<MergedExpression[]>(() => {
    if (this.showAll()) return this.slots();
    const limit = this.collapsedLimit();
    if (limit <= 0) return this.slots();
    return this.slots().slice(0, limit);
  });
  readonly hasHiddenSlots = computed(() =>
    this.slots().length > this.visibleSlots().length,
  );
  readonly accent = computed(() => this.accentColor()?.trim() || 'var(--color-accent)');
  readonly accentBorder = computed(() =>
    `color-mix(in srgb, ${this.accent()} 28%, var(--color-border-base))`,
  );

  readonly isCustomKeyOpen = signal(false);
  customKeyDraft = '';
  readonly customKeyError = signal<string | null>(null);

  readonly isGalleryOpen = signal(false);
  readonly activeKey = signal<string | null>(null);
  readonly galleryImages = signal<FileAttachment[]>([]);
  readonly galleryLoading = signal(false);
  readonly galleryQuery = signal('');
  readonly galleryPersonalityOnly = signal(true);
  readonly filteredGalleryImages = computed(() => {
    const key = this.galleryQuery().trim().toLowerCase();
    const filtered = this.galleryPersonalityOnly()
      ? this.galleryImages().filter(image =>
          (image.personalities ?? []).some(ref => ref.id === this.personalityId()) || image.personality_id === this.personalityId(),
        )
      : this.galleryImages();
    const sorted = [...filtered].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
    if (!key) return sorted;
    return sorted.filter(image => image.name.toLowerCase().includes(key));
  });

  private readonly labelDrafts = new Map<string, string>();

  slotAriaLabel(slot: MergedExpression): string {
    return `${slot.expressionKey}: ${slot.imageId ? 'set' : 'unset'}`;
  }

  slotImageUrl(slot: MergedExpression): string | null {
    if (slot.imageId) {
      return this.imageGallery.getImageUrl(slot.imageId, 'thumbnail');
    }
    return slot.imageUrl;
  }

  onLabelChange(slot: MergedExpression, value: string): void {
    this.labelDrafts.set(slot.expressionKey, value);
  }

  onLabelCommit(slot: MergedExpression): void {
    const draft = (this.labelDrafts.get(slot.expressionKey) ?? slot.label ?? '').trim();
    const original = (slot.label ?? '').trim();
    if (draft === original) return;
    const ctx = this.optimisticContext(slot.expressionKey, { label: draft || null });
    this.assignment.setLabel(this.personalityId(), slot.expressionKey, draft, ctx).subscribe({
      next: () => this.labelDrafts.delete(slot.expressionKey),
      error: err => this.flashError(err?.message ?? 'Failed to update label.'),
    });
  }

  onAssign(slot: MergedExpression): void {
    this.activeKey.set(slot.expressionKey);
    this.openGallery();
  }

  onClear(slot: MergedExpression): void {
    if (!slot.imageId) return;
    const ctx = this.optimisticContext(slot.expressionKey, { image_id: null });
    this.assignment.clear(this.personalityId(), slot.expressionKey, ctx).subscribe({
      error: err => this.flashError(err?.message ?? 'Failed to clear image.'),
    });
  }

  onRemove(slot: MergedExpression): void {
    const ctx = this.optimisticDeleteContext(slot.expressionKey);
    this.assignment.remove(this.personalityId(), slot.expressionKey, ctx).subscribe({
      error: err => this.flashError(err?.message ?? 'Failed to remove key.'),
    });
  }

  onGenerateDefaultGrid(force = false): void {
    if (this.gridGenerating()) return;
    this.gridGenerating.set(true);
    this.errorMessage.set(null);
    this.mediaJobs
      .startExpressionGrid(this.personalityId(), { force })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
      next: enqueued => this.resumeGridPoll(enqueued.job_id),
      error: err => {
        this.gridGenerating.set(false);
        const msg =
          err?.status === 409
            ? err?.error?.message ?? 'Another image job is already running.'
            : err?.error?.message ?? err?.message ?? 'Generation failed.';
        this.flashError(msg);
      },
    });
  }

  private resumeGridPoll(jobId: string): void {
    this.mediaJobs
      .pollUntilTerminal(jobId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
      next: job => {
        if (job.status === 'failed') {
          this.gridGenerating.set(false);
          this.flashError(job.error ?? 'Expression grid generation failed.');
          return;
        }
        if (job.status !== 'complete') {
          return;
        }
        this.personalityApi
          .listExpressions(this.personalityId())
          .pipe(takeUntilDestroyed(this.destroyRef))
          .subscribe({
            next: rows => {
              this.gridGenerating.set(false);
              this.expressionsChanged.emit(rows);
            },
            error: err => {
              this.gridGenerating.set(false);
              this.flashError(err?.message ?? 'Failed to reload expressions.');
            },
          });
      },
      error: err => {
        this.gridGenerating.set(false);
        this.flashError(err?.message ?? 'Generation failed.');
      },
    });
  }

  onCreateCustomKey(): void {
    this.customKeyDraft = '';
    this.customKeyError.set(null);
    this.isCustomKeyOpen.set(true);
  }

  onToggleExpressionsEnabled(): void {
    if (this.expressionsToggleUpdating()) return;
    this.expressionsEnabledChanged.emit(!this.expressionsEnabled());
  }

  closeCustomKey(): void {
    this.isCustomKeyOpen.set(false);
    this.customKeyDraft = '';
    this.customKeyError.set(null);
  }

  submitCustomKey(): void {
    const key = this.customKeyDraft.trim().toLowerCase();
    if (!key) {
      this.customKeyError.set('Key is required.');
      return;
    }
    if (!isValidExpressionKey(key)) {
      this.customKeyError.set('Use lowercase letters, digits, hyphens, or underscores. Max 64 chars.');
      return;
    }
    if (this.slots().some(slot => slot.expressionKey === key)) {
      this.customKeyError.set('That key already exists.');
      return;
    }
    const placeholder: PersonalityExpression = {
      expression_key: key,
      label: null,
      image_id: null,
      image_url: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    const next = [...this.expressions(), placeholder];
    this.expressionsChanged.emit(next);
    this.closeCustomKey();
    this.activeKey.set(key);
    this.openGallery();
  }

  toggleShowAll(): void {
    this.showAll.update(value => !value);
  }

  toggleGalleryPersonalityOnly(): void {
    this.galleryPersonalityOnly.update(value => !value);
    this.loadGalleryImages();
  }

  private openGallery(): void {
    this.isGalleryOpen.set(true);
    this.galleryQuery.set('');
    this.galleryPersonalityOnly.set(true);
    this.loadGalleryImages();
  }

  private loadGalleryImages(): void {
    this.galleryLoading.set(true);
    const personalityId = this.galleryPersonalityOnly() ? this.personalityId() : undefined;
    this.imageGallery.listImages(1, 60, { personalityId }).subscribe({
      next: response => {
        this.galleryImages.set(response.results ?? []);
        this.galleryLoading.set(false);
      },
      error: err => {
        console.error('Failed to load gallery', err);
        this.galleryImages.set([]);
        this.galleryLoading.set(false);
        this.flashError('Failed to load image gallery.');
      },
    });
  }

  closeGallery(): void {
    this.isGalleryOpen.set(false);
    this.activeKey.set(null);
  }

  galleryThumbUrl(image: FileAttachment): string {
    return this.imageGallery.getImageUrl(image.id, 'thumbnail');
  }

  onPickImage(image: FileAttachment): void {
    const key = this.activeKey();
    if (!key) return;
    const fullUrl = this.imageGallery.getImageUrl(image.id, 'full');
    const ctx = this.optimisticContext(key, { image_id: image.id });
    this.assignment.assignFromGallery(this.personalityId(), key, image.id, fullUrl, ctx).subscribe({
      next: () => {
        this.closeGallery();
      },
      error: err => {
        this.flashError(err?.message ?? 'Failed to assign image.');
      },
    });
  }

  private optimisticDeleteContext(key: string): OptimisticContext {
    return {
      snapshot: () => {
        const existing = this.expressions().find(e => e.expression_key === key);
        return {
          expressionKey: key,
          imageId: existing?.image_id ?? null,
          imageUrl: existing?.image_url ?? null,
          label: existing?.label ?? null,
          exists: !!existing,
        };
      },
      apply: () => {},
      applyDelete: () => {
        const list = [...this.expressions()];
        const idx = list.findIndex(e => e.expression_key === key);
        if (idx >= 0) {
          list.splice(idx, 1);
        }
        this.expressionsChanged.emit(list);
      },
      commit: () => {},
      rollback: snap => {
        const list = [...this.expressions()];
        const idx = list.findIndex(e => e.expression_key === snap.expressionKey);
        if (snap.exists && idx < 0) {
          list.push({
            expression_key: snap.expressionKey,
            label: snap.label,
            image_id: snap.imageId,
            image_url: snap.imageUrl,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          });
        } else if (snap.exists && idx >= 0) {
          list[idx] = {
            expression_key: snap.expressionKey,
            label: snap.label,
            image_id: snap.imageId,
            image_url: snap.imageUrl,
            created_at: list[idx].created_at,
            updated_at: list[idx].updated_at,
          };
        } else if (!snap.exists && idx >= 0) {
          list.splice(idx, 1);
        }
        this.expressionsChanged.emit(list);
      },
    };
  }

  private optimisticContext(
    key: string,
    _optimistic: UpdatePersonalityExpressionRequest,
  ): OptimisticContext {
    return {
      snapshot: () => {
        const existing = this.expressions().find(e => e.expression_key === key);
        return {
          expressionKey: key,
          imageId: existing?.image_id ?? null,
          imageUrl: existing?.image_url ?? null,
          label: existing?.label ?? null,
          exists: !!existing,
        };
      },
      apply: (snap, change, optimisticImageUrl) => {
        const list = [...this.expressions()];
        const idx = list.findIndex(e => e.expression_key === key);
        const merged: PersonalityExpression = {
          expression_key: key,
          label: change.label !== undefined ? change.label : snap.label,
          image_id: change.image_id !== undefined ? change.image_id : snap.imageId,
          image_url: change.image_id === null ? null : optimisticImageUrl ?? snap.imageUrl,
          created_at: idx >= 0 ? list[idx].created_at : new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        if (idx >= 0) list[idx] = merged; else list.push(merged);
        this.expressionsChanged.emit(list);
      },
      commit: server => {
        const list = [...this.expressions()];
        const idx = list.findIndex(e => e.expression_key === server.expression_key);
        if (idx >= 0) list[idx] = server; else list.push(server);
        this.expressionsChanged.emit(list);
      },
      rollback: snap => {
        const list = [...this.expressions()];
        const idx = list.findIndex(e => e.expression_key === snap.expressionKey);
        if (!snap.exists) {
          if (idx >= 0) list.splice(idx, 1);
        } else if (idx >= 0) {
          list[idx] = {
            expression_key: snap.expressionKey,
            label: snap.label,
            image_id: snap.imageId,
            image_url: snap.imageUrl,
            created_at: list[idx].created_at,
            updated_at: list[idx].updated_at,
          };
        }
        this.expressionsChanged.emit(list);
      },
    };
  }

  private flashError(message: string): void {
    this.errorMessage.set(message);
    setTimeout(() => {
      if (this.errorMessage() === message) this.errorMessage.set(null);
    }, 4000);
  }
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}

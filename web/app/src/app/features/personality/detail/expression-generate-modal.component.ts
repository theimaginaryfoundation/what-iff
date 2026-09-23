import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  inject,
  input,
  OnDestroy,
  output,
  signal,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { AsyncPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { forkJoin, Observable, of, Subscription } from 'rxjs';
import { catchError, map, switchMap } from 'rxjs/operators';

import { PersonalityExpression } from '../../../core/models/personality.model';
import { FileAttachment } from '../../../core/models/file-attachment.model';
import {
  ExpressionCandidatesProgress,
  parseExpressionCandidatesProgress,
} from '../../../core/models/personality-media-job.model';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { SpinnerComponent } from '../../../shared/ui/spinner/spinner.component';
import { CheckIconComponent, UploadIconComponent, XIconComponent } from '../../../shared/ui/icons';
import {
  DEFAULT_EXPRESSION_SUGGESTIONS,
  expressionKeyFromName,
  expressionNameFromKey,
  isValidExpressionKey,
} from '../helpers/expressions.helpers';

/** One cell of the 3×3 generate grid. */
export interface GenerateCell {
  /** Editable free-text name; slugged to the expression key on generate/save. */
  name: string;
  /** Unassigned candidate gallery image for this cell, once generated. */
  imageId: string | null;
  /** Keep (assign on save) vs discard (delete on save / replace on regenerate). */
  keep: boolean;
}

const GRID_SIZE = 9;

function defaultCells(): GenerateCell[] {
  return DEFAULT_EXPRESSION_SUGGESTIONS.map(key => ({
    name: expressionNameFromKey(key),
    imageId: null,
    keep: true,
  }));
}

/**
 * "Generate" modal on the expressions manager: pick an optional reference image, edit the nine
 * expression names (defaults prefilled), generate one 3×3 grid of **candidates** (gallery images,
 * not yet assigned), then keep/discard each panel and save the keepers as expression slots.
 *
 * Regenerating keeps the cells marked ✓ and replaces only the discarded (✗) or empty ones; the
 * surplus panels from the new grid are deleted. Closing the modal only hides it — an in-flight
 * job and unsaved candidates survive until the user saves or discards, or the host is destroyed
 * (then unsaved candidates are deleted best-effort).
 */
@Component({
  selector: 'app-expression-generate-modal',
  standalone: true,
  imports: [
    AsyncPipe,
    AuthImagePipe,
    FormsModule,
    ModalComponent,
    SpinnerComponent,
    CheckIconComponent,
    UploadIconComponent,
    XIconComponent,
  ],
  template: `
    <ui-modal [open]="open()" [labelledBy]="labelId" size="lg" (dismiss)="dismiss.emit()">
      <div modal-header>
        <h2 [id]="labelId" class="text-base font-semibold text-(--color-text-primary)">Generate expressions</h2>
        <p class="mt-0.5 text-[0.6875rem] text-(--color-text-secondary)">
          Name nine expressions, optionally add a style reference, then keep the portraits you like.
        </p>
      </div>

      <div class="flex flex-col gap-4">
        <!-- Reference image -->
        <section class="flex flex-col gap-2" aria-label="Reference image">
          <div class="flex items-center gap-3">
            <div class="h-16 w-16 shrink-0 overflow-hidden rounded-md border border-border-base bg-(--color-surface-input)">
              @if (referenceThumbUrl(); as refUrl) {
                <img
                  class="h-full w-full object-cover"
                  [src]="(refUrl | authImage | async) ?? ''"
                  alt="Reference image"
                  decoding="async"
                />
              } @else {
                <div class="flex h-full w-full items-center justify-center px-1 text-center text-[0.625rem] text-(--color-text-muted)">No reference</div>
              }
            </div>
            <div class="flex min-w-0 flex-1 flex-col gap-1.5">
              <span class="text-sm font-medium text-(--color-text-primary)">Reference image <span class="font-normal text-(--color-text-secondary)">(optional)</span></span>
              <div class="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  class="rounded-md border border-border-base bg-(--color-surface-base) px-2.5 py-1 text-xs font-medium text-(--color-text-primary) disabled:opacity-50"
                  [disabled]="busy()"
                  [attr.aria-expanded]="pickerOpen()"
                  (click)="togglePicker()"
                >{{ pickerOpen() ? 'Hide gallery' : 'Choose from gallery' }}</button>
                <label
                  class="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-border-base bg-(--color-surface-base) px-2.5 py-1 text-xs font-medium text-(--color-text-primary)"
                  [class.pointer-events-none]="busy() || uploading()"
                  [class.opacity-50]="busy() || uploading()"
                >
                  @if (uploading()) {
                    <ui-spinner [size]="12" ariaLabel="Uploading" />
                  } @else {
                    <ui-upload-icon [size]="12" />
                  }
                  Upload
                  <input type="file" accept="image/*" class="sr-only" [disabled]="busy() || uploading()" (change)="onUpload($event)" />
                </label>
                @if (referenceImageId()) {
                  <button
                    type="button"
                    class="px-1 text-xs text-(--color-text-secondary) underline-offset-2 hover:underline disabled:opacity-50"
                    [disabled]="busy()"
                    (click)="referenceImageId.set(null)"
                  >Remove</button>
                }
              </div>
              <span class="text-[0.6875rem] text-(--color-text-muted)">Used to match art style and character design.</span>
            </div>
          </div>

          @if (pickerOpen()) {
            <div class="rounded-md border border-border-base bg-(--color-surface-input) p-2">
              <div class="mb-2 flex items-center justify-between gap-2">
                <span class="text-[0.6875rem] text-(--color-text-secondary)">{{ pickerAllImages() ? 'All images' : 'This personality' }}</span>
                <button type="button" class="text-[0.6875rem] font-medium text-(--color-accent)" (click)="togglePickerScope()">
                  {{ pickerAllImages() ? 'Only this personality' : 'Show all images' }}
                </button>
              </div>
              @if (pickerLoading()) {
                <p class="p-2 text-xs text-(--color-text-secondary)" role="status">Loading gallery…</p>
              } @else if (pickerImages().length === 0) {
                <p class="p-2 text-xs text-(--color-text-secondary)">No images yet — upload one instead.</p>
              } @else {
                <ul class="grid max-h-48 gap-1.5 overflow-y-auto" style="grid-template-columns: repeat(auto-fill, minmax(64px, 1fr));" role="list">
                  @for (image of pickerImages(); track image.id) {
                    <li role="listitem">
                      <button
                        type="button"
                        class="block w-full overflow-hidden rounded border-2 outline-none focus-visible:ring-2 focus-visible:ring-(--color-accent)"
                        [class.border-(--color-accent)]="image.id === referenceImageId()"
                        [class.border-transparent]="image.id !== referenceImageId()"
                        [attr.aria-label]="'Use ' + image.name + ' as reference'"
                        [attr.aria-pressed]="image.id === referenceImageId()"
                        (click)="pickReference(image)"
                      >
                        <img class="aspect-square w-full object-cover" [src]="(thumbUrl(image.id) | authImage | async) ?? ''" [alt]="image.name" loading="lazy" decoding="async" />
                      </button>
                    </li>
                  }
                </ul>
              }
            </div>
          }
        </section>

        @if (error()) {
          <p class="rounded-md border border-red-500 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">{{ error() }}</p>
        }

        <!-- 3×3 grid -->
        <ul class="grid grid-cols-3 gap-2" role="list" aria-label="Expressions to generate">
          @for (cell of cells(); track $index; let i = $index) {
            <li
              class="flex flex-col overflow-hidden rounded-md border bg-(--color-surface-base)"
              [class.border-border-base]="!cellError(i) && !(cell.imageId && cell.keep)"
              [class.border-(--color-accent)]="!cellError(i) && cell.imageId && cell.keep"
              [class.border-red-500]="!!cellError(i)"
              role="listitem"
            >
              <div class="relative aspect-square bg-(--color-surface-elevated)">
                @if (cell.imageId) {
                  <img
                    class="h-full w-full object-cover transition-opacity"
                    [class.opacity-30]="!cell.keep"
                    [src]="(thumbUrl(cell.imageId) | authImage | async) ?? ''"
                    [alt]="cell.name"
                    decoding="async"
                  />
                }
                @if (generating() && !isLocked(cell)) {
                  <div class="absolute inset-0 flex items-center justify-center bg-(--color-surface-elevated)/70">
                    <ui-spinner [size]="20" ariaLabel="Generating" />
                  </div>
                } @else if (!cell.imageId) {
                  <div class="flex h-full w-full items-center justify-center text-[0.6875rem] text-(--color-text-muted)">Not generated</div>
                }
                @if (cell.imageId && !generating()) {
                  <div class="absolute inset-x-0 bottom-0 flex justify-center gap-1.5 p-1.5">
                    <button
                      type="button"
                      class="inline-flex h-7 w-7 items-center justify-center rounded-full border shadow"
                      [class.bg-emerald-600]="cell.keep"
                      [class.border-emerald-600]="cell.keep"
                      [class.text-white]="cell.keep"
                      [class.bg-black/45]="!cell.keep"
                      [class.border-white/30]="!cell.keep"
                      [class.text-white/80]="!cell.keep"
                      [attr.aria-label]="'Keep ' + cell.name"
                      [attr.aria-pressed]="cell.keep"
                      [disabled]="saving()"
                      (click)="setKeep(i, true)"
                    ><ui-check-icon [size]="14" /></button>
                    <button
                      type="button"
                      class="inline-flex h-7 w-7 items-center justify-center rounded-full border shadow"
                      [class.bg-red-600]="!cell.keep"
                      [class.border-red-600]="!cell.keep"
                      [class.text-white]="!cell.keep"
                      [class.bg-black/45]="cell.keep"
                      [class.border-white/30]="cell.keep"
                      [class.text-white/80]="cell.keep"
                      [attr.aria-label]="'Discard ' + cell.name"
                      [attr.aria-pressed]="!cell.keep"
                      [disabled]="saving()"
                      (click)="setKeep(i, false)"
                    ><ui-x-icon [size]="14" /></button>
                  </div>
                }
                @if (replacesExisting(i)) {
                  <span class="absolute left-1 top-1 rounded bg-black/55 px-1 text-[0.5625rem] font-medium text-white" title="Saving replaces this expression's current image">replaces</span>
                }
              </div>
              <input
                type="text"
                class="w-full rounded-none border-x-0 border-b-0 border-t border-border-base bg-(--color-surface-input) px-2 py-1.5 text-center text-[0.8125rem] text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-1 focus:ring-(--color-accent) disabled:opacity-60"
                maxlength="64"
                [attr.aria-label]="'Expression ' + (i + 1) + ' name'"
                [attr.aria-invalid]="!!cellError(i)"
                [disabled]="busy()"
                [ngModel]="cell.name"
                (ngModelChange)="setName(i, $event)"
              />
              @if (cellError(i); as msg) {
                <span class="px-2 pb-1 text-center text-[0.625rem] text-red-500">{{ msg }}</span>
              }
            </li>
          }
        </ul>
      </div>

      <div modal-footer class="flex w-full flex-wrap items-center justify-between gap-2">
        <span class="text-[0.6875rem] text-(--color-text-muted)" role="status">{{ statusText() }}</span>
        <div class="flex flex-wrap items-center gap-2">
          @if (hasCandidates()) {
            <button
              type="button"
              class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated) disabled:opacity-50"
              [disabled]="busy()"
              (click)="discardAll()"
            >Discard all</button>
          } @else {
            <button
              type="button"
              class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
              (click)="dismiss.emit()"
            >{{ generating() ? 'Hide' : 'Cancel' }}</button>
          }
          <button
            type="button"
            class="rounded-lg border border-(--color-accent) px-3 py-1.5 text-sm font-semibold text-(--color-accent) disabled:cursor-not-allowed disabled:opacity-50"
            [class.bg-(--color-accent)]="!hasCandidates()"
            [class.text-white]="!hasCandidates()"
            [disabled]="busy() || !canGenerate()"
            (click)="generate()"
          >
            @if (generating()) {
              Generating…
            } @else if (hasCandidates()) {
              Regenerate {{ regenerateCount() }}
            } @else {
              Generate
            }
          </button>
          @if (hasCandidates()) {
            <button
              type="button"
              class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"
              [disabled]="busy() || keptCount() === 0"
              (click)="save()"
            >{{ saving() ? 'Saving…' : 'Save ' + keptCount() }}</button>
          }
        </div>
      </div>
    </ui-modal>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ExpressionGenerateModalComponent implements OnDestroy {
  readonly personalityId = input.required<string>();
  readonly open = input(false);
  /** Personality cover image; preselected as the reference the first time the modal opens. */
  readonly coverImageId = input<string | null>(null);
  /** Current persisted expressions, used to flag keys whose image would be replaced. */
  readonly existingExpressions = input<readonly PersonalityExpression[]>([]);

  readonly dismiss = output<void>();
  /** Emitted with the full refreshed expression list after keepers are saved. */
  readonly saved = output<readonly PersonalityExpression[]>();
  /** True while a job runs or a save is in flight (host disables conflicting actions). */
  readonly busyChange = output<boolean>();

  private readonly mediaJobs = inject(PersonalityMediaJobService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly personalityApi = inject(PersonalityService);
  private readonly destroyRef = inject(DestroyRef);

  readonly labelId = `expressions-generate-${Math.random().toString(36).slice(2, 10)}`;

  readonly cells = signal<GenerateCell[]>(defaultCells());
  readonly referenceImageId = signal<string | null>(null);
  readonly generating = signal(false);
  readonly saving = signal(false);
  readonly uploading = signal(false);
  readonly error = signal<string | null>(null);

  readonly pickerOpen = signal(false);
  readonly pickerAllImages = signal(false);
  readonly pickerLoading = signal(false);
  readonly pickerImages = signal<FileAttachment[]>([]);

  private referenceInitialized = false;
  private pollSub: Subscription | null = null;

  readonly busy = computed(() => this.generating() || this.saving());
  readonly keys = computed(() => this.cells().map(c => expressionKeyFromName(c.name)));
  readonly hasCandidates = computed(() => this.cells().some(c => !!c.imageId));
  readonly keptCount = computed(() => this.cells().filter(c => c.imageId && c.keep).length);
  /** Cells a regenerate would fill: empty or marked discard. */
  readonly regenerateCount = computed(() => this.cells().filter(c => !this.isLocked(c)).length);
  readonly keyErrors = computed<(string | null)[]>(() => {
    const keys = this.keys();
    return keys.map((key, i) => {
      if (!key) return 'Name required';
      if (!isValidExpressionKey(key)) return 'Use letters or digits';
      if (keys.indexOf(key) !== i) return 'Duplicate name';
      return null;
    });
  });
  readonly canGenerate = computed(() =>
    this.keyErrors().every(e => e === null) && this.regenerateCount() > 0,
  );
  readonly referenceThumbUrl = computed(() => {
    const id = this.referenceImageId();
    return id ? this.thumbUrl(id) : null;
  });
  readonly statusText = computed(() => {
    if (this.saving()) return 'Saving…';
    if (this.generating()) return 'Generating — this takes about a minute. You can close this and come back.';
    if (this.hasCandidates()) {
      const discarded = this.cells().filter(c => c.imageId && !c.keep).length;
      return `${this.keptCount()} kept · ${discarded} discarded`;
    }
    return 'One image generates all nine portraits.';
  });

  constructor() {
    effect(() => {
      const open = this.open();
      const cover = this.coverImageId();
      if (open && !this.referenceInitialized) {
        this.referenceInitialized = true;
        untracked(() => this.referenceImageId.set(cover));
      }
    });
  }

  ngOnDestroy(): void {
    // Unsaved candidates are throwaway; drop them rather than leave nine orphans in the gallery.
    // (An in-flight job's output can't be reached from here and stays in the gallery.)
    if (!this.saving()) {
      for (const cell of this.cells()) {
        if (cell.imageId) this.deleteImageQuietly(cell.imageId);
      }
    }
  }

  isLocked(cell: GenerateCell): boolean {
    return !!cell.imageId && cell.keep;
  }

  cellError(index: number): string | null {
    return this.keyErrors()[index];
  }

  replacesExisting(index: number): boolean {
    const cell = this.cells()[index];
    if (!cell?.imageId || !cell.keep) return false;
    const key = this.keys()[index];
    return this.existingExpressions().some(e => e.expression_key === key && !!e.image_id);
  }

  thumbUrl(imageId: string): string {
    return this.imageGallery.getImageUrl(imageId, 'thumbnail');
  }

  setName(index: number, name: string): void {
    this.updateCell(index, { name });
  }

  setKeep(index: number, keep: boolean): void {
    this.updateCell(index, { keep });
  }

  // --- Reference picker ---

  togglePicker(): void {
    const next = !this.pickerOpen();
    this.pickerOpen.set(next);
    if (next) this.loadPickerImages();
  }

  togglePickerScope(): void {
    this.pickerAllImages.update(v => !v);
    this.loadPickerImages();
  }

  pickReference(image: FileAttachment): void {
    this.referenceImageId.set(image.id);
    this.pickerOpen.set(false);
  }

  onUpload(event: Event): void {
    const inputEl = event.target as HTMLInputElement;
    const file = inputEl.files?.[0];
    inputEl.value = '';
    if (!file) return;
    if (!file.type.startsWith('image/')) {
      this.error.set('Reference must be an image.');
      return;
    }
    this.uploading.set(true);
    this.error.set(null);
    this.imageGallery
      .importImage(file)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: attachment => {
          this.uploading.set(false);
          this.referenceImageId.set(attachment.id);
          this.pickerImages.update(list => [attachment, ...list.filter(i => i.id !== attachment.id)]);
        },
        error: err => {
          this.uploading.set(false);
          this.error.set(err?.error?.message ?? 'Failed to upload reference image.');
        },
      });
  }

  private loadPickerImages(): void {
    this.pickerLoading.set(true);
    const personalityId = this.pickerAllImages() ? undefined : this.personalityId();
    this.imageGallery
      .listImages(1, 60, { personalityId })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: res => {
          const sorted = [...(res.results ?? [])].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
          this.pickerImages.set(sorted);
          this.pickerLoading.set(false);
        },
        error: () => {
          this.pickerImages.set([]);
          this.pickerLoading.set(false);
          this.error.set('Failed to load image gallery.');
        },
      });
  }

  // --- Generate ---

  generate(): void {
    if (this.busy() || !this.canGenerate()) return;
    this.error.set(null);
    this.setGenerating(true);
    this.mediaJobs
      .startExpressionCandidates(this.personalityId(), this.keys(), this.referenceImageId())
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: enqueued => this.pollJob(enqueued.job_id),
        error: err => {
          this.setGenerating(false);
          this.error.set(
            err?.status === 409
              ? err?.error?.message ?? 'Another image job is already running.'
              : err?.error?.message ?? err?.message ?? 'Generation failed.',
          );
        },
      });
  }

  /**
   * Re-attaches to an in-flight candidate job (e.g. after a page reload). Resets the grid to the
   * job's names; candidates from any earlier round in a previous page session are not recoverable.
   */
  resume(jobId: string, progress: ExpressionCandidatesProgress): void {
    if (this.busy()) return;
    const names = progress.expressions.slice(0, GRID_SIZE).map(expressionNameFromKey);
    this.cells.set(names.map(name => ({ name, imageId: null, keep: true })));
    this.referenceInitialized = true;
    this.referenceImageId.set(progress.reference_image_id ?? null);
    this.error.set(null);
    this.setGenerating(true);
    this.pollJob(jobId);
  }

  private pollJob(jobId: string): void {
    this.pollSub?.unsubscribe();
    this.pollSub = this.mediaJobs
      .pollUntilTerminal(jobId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: job => {
          if (job.status === 'failed') {
            this.setGenerating(false);
            this.error.set(job.error || 'Expression generation failed.');
            return;
          }
          if (job.status !== 'complete') return;
          this.setGenerating(false);
          const progress = parseExpressionCandidatesProgress(job.progress);
          const candidates = progress?.candidates ?? [];
          if (candidates.length !== GRID_SIZE) {
            this.error.set('Generation finished without results. Please try again.');
            for (const c of candidates) this.deleteImageQuietly(c.image_id);
            return;
          }
          this.applyCandidates(candidates.map(c => c.image_id));
        },
        error: err => {
          this.setGenerating(false);
          this.error.set(err?.message ?? 'Generation failed.');
        },
      });
  }

  /** Fills unlocked cells with the new round; deletes replaced and surplus images. */
  private applyCandidates(newImageIds: string[]): void {
    const next = this.cells().map((cell, i) => {
      const fresh = newImageIds[i];
      if (this.isLocked(cell)) {
        this.deleteImageQuietly(fresh);
        return cell;
      }
      if (cell.imageId) this.deleteImageQuietly(cell.imageId);
      return { ...cell, imageId: fresh, keep: true };
    });
    this.cells.set(next);
  }

  // --- Save / discard ---

  save(): void {
    if (this.busy() || this.keptCount() === 0) return;
    const keys = this.keys();
    const errors = this.keyErrors();
    const cells = this.cells();
    if (cells.some((c, i) => c.imageId && c.keep && errors[i])) {
      this.error.set('Fix the highlighted names before saving.');
      return;
    }

    this.error.set(null);
    this.saving.set(true);
    this.busyChange.emit(true);

    const upserts: Observable<unknown>[] = [];
    const discards: string[] = [];
    cells.forEach((cell, i) => {
      if (!cell.imageId) return;
      if (cell.keep) {
        upserts.push(this.personalityApi.upsertExpression(this.personalityId(), keys[i], { image_id: cell.imageId }));
      } else {
        discards.push(cell.imageId);
      }
    });

    forkJoin(upserts)
      .pipe(
        switchMap(() => this.personalityApi.listExpressions(this.personalityId())),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: rows => {
          for (const id of discards) this.deleteImageQuietly(id);
          this.saving.set(false);
          this.busyChange.emit(false);
          this.reset();
          this.saved.emit(rows);
        },
        error: err => {
          this.saving.set(false);
          this.busyChange.emit(false);
          this.error.set(err?.error?.message ?? err?.message ?? 'Failed to save expressions.');
        },
      });
  }

  discardAll(): void {
    if (this.busy()) return;
    for (const cell of this.cells()) {
      if (cell.imageId) this.deleteImageQuietly(cell.imageId);
    }
    this.cells.update(cells => cells.map(c => ({ ...c, imageId: null, keep: true })));
    this.error.set(null);
  }

  /** Clears candidates and restores default names; the reference choice is kept. */
  private reset(): void {
    this.cells.set(defaultCells());
    this.error.set(null);
  }

  private setGenerating(value: boolean): void {
    this.generating.set(value);
    this.busyChange.emit(value);
  }

  private updateCell(index: number, patch: Partial<GenerateCell>): void {
    this.cells.update(cells => cells.map((c, i) => (i === index ? { ...c, ...patch } : c)));
  }

  private deleteImageQuietly(imageId: string): void {
    this.imageGallery
      .deleteImage(imageId)
      .pipe(
        map(() => true),
        catchError(() => of(false)),
      )
      .subscribe();
  }
}

import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  OnChanges,
  signal,
  SimpleChanges,
} from '@angular/core';

import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachment, PendingFileAttachment } from '../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';

const SUPPORTED_FILE_TYPES = [
  '.c', '.cpp', '.cs', '.css', '.doc', '.docx', '.go', '.html',
  '.java', '.js', '.json', '.md', '.pdf', '.php', '.py',
  '.rb', '.sh', '.tex', '.ts', '.txt',
];
const MAX_FILES = 40;
const MAX_FILE_SIZE = 10 * 1024 * 1024; // 10MB

/**
 * Manages personality-scoped file attachments. The component owns its own
 * upload + delete logic but delegates network calls to `FileAttachmentService`.
 * The parent provides the personality ID and is otherwise hands-off.
 */
@Component({
  selector: 'app-personality-attachments-list',
  standalone: true,
  imports: [],
  template: `
    <section
      [class]="containerClass()"
      aria-label="Personality attachments"
    >
      <header class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-base font-semibold text-(--color-text-primary)">Attachments</h2>
          <p class="text-xs text-(--color-text-secondary)">{{ remaining() }} of {{ maxFiles }} slots available · 10MB max per file</p>
        </div>
        <div class="flex items-center gap-2">
          <input
            #fileInput
            id="personality-attachments-file-input"
            type="file"
            multiple
            class="hidden"
            (change)="onFilesSelected($event)"
          />
          <button
            type="button"
            class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-60"
            [disabled]="!canAddMoreFiles()"
            (click)="fileInput.click()"
          >Upload</button>
        </div>
      </header>

      @if (errorMessage()) {
        <p class="rounded-md border border-red-500 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">{{ errorMessage() }}</p>
      }

      @if (isLoading()) {
        <p class="text-sm text-(--color-text-secondary)" role="status">Loading attachments…</p>
      } @else if (attachments().length === 0 && pending().length === 0) {
        <p class="rounded-lg border border-dashed border-border-base bg-(--color-surface-elevated) p-6 text-center text-sm text-(--color-text-secondary)">
          No files attached yet. Upload reference docs to give this personality more context.
        </p>
      } @else {
        <ul class="flex flex-col divide-y divide-border-base">
          @for (attachment of attachments(); track attachment.id) {
            <li class="flex items-center justify-between gap-3 py-2">
              <div class="min-w-0">
                <p class="truncate text-sm font-medium text-(--color-text-primary)" [title]="attachment.name">{{ attachment.name }}</p>
                <p class="text-xs text-(--color-text-secondary)">{{ attachment.file_type || 'file' }}</p>
              </div>
              <button
                type="button"
                class="rounded-lg p-1.5 text-(--color-text-secondary) outline-none hover:bg-red-500/10 hover:text-red-600 focus-visible:ring-2 focus-visible:ring-red-500"
                [attr.aria-label]="'Delete ' + attachment.name"
                (click)="onDelete(attachment)"
              >✕</button>
            </li>
          }
          @for (item of pending(); track $index) {
            <li class="flex items-center justify-between gap-3 py-2">
              <div class="min-w-0">
                <p class="truncate text-sm text-(--color-text-primary)" [title]="$safeNavigationMigration(item.file?.name)">{{ item.file?.name }}</p>
                <p class="text-xs text-(--color-text-secondary)">
                  @if (item.uploadError) {
                    <span class="text-red-500">{{ item.uploadError }}</span>
                  } @else {
                    Uploading…
                  }
                </p>
              </div>
              @if (item.uploadError) {
                <div class="flex items-center gap-1">
                  <button
                    type="button"
                    class="rounded-lg border border-border-base px-2 py-1 text-xs text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
                    (click)="onRetry($index)"
                  >Retry</button>
                  <button
                    type="button"
                    class="rounded-lg p-1.5 text-(--color-text-secondary) hover:text-red-600"
                    [attr.aria-label]="'Discard ' + item.file?.name"
                    (click)="onRemovePending($index)"
                  >✕</button>
                </div>
              }
            </li>
          }
        </ul>
      }
    </section>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityAttachmentsListComponent implements OnChanges {
  readonly personalityId = input<string | null>(null);
  readonly borderless = input(false);

  private readonly fileAttachmentService = inject(FileAttachmentService);
  private readonly confirmation = inject(ConfirmationService);

  readonly attachments = signal<FileAttachment[]>([]);
  readonly pending = signal<PendingFileAttachment[]>([]);
  readonly isLoading = signal(false);
  readonly errorMessage = signal<string | null>(null);

  readonly maxFiles = MAX_FILES;
  readonly remaining = computed(() => this.maxFiles - (this.attachments().length + this.pending().length));
  readonly canAddMoreFiles = computed(() => this.remaining() > 0);
  readonly containerClass = computed(() =>
    this.borderless()
      ? 'attachments-list flex flex-col gap-3 bg-transparent p-0'
      : 'attachments-list flex flex-col gap-3 rounded-2xl border border-border-base bg-(--color-surface-card) p-4',
  );

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['personalityId']) {
      const id = this.personalityId();
      this.attachments.set([]);
      this.pending.set([]);
      this.errorMessage.set(null);
      if (id) this.loadAttachments(id);
    }
  }

  private loadAttachments(personalityId: string): void {
    this.isLoading.set(true);
    this.fileAttachmentService.listFileAttachments(1, this.maxFiles, { personality_id: personalityId, docs_only: true }).subscribe({
      next: response => {
        this.attachments.set(response.results);
        this.isLoading.set(false);
      },
      error: err => {
        console.error('Failed to load attachments', err);
        this.isLoading.set(false);
        this.flashError('Failed to load attachments. Please try again.');
      },
    });
  }

  onFilesSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;
    const files = Array.from(input.files);
    input.value = '';
    void this.handleFiles(files);
  }

  private async handleFiles(files: File[]): Promise<void> {
    const personalityId = this.personalityId();
    if (!personalityId) return;

    const valid: File[] = [];
    for (const file of files) {
      const error = this.validateFile(file);
      if (error) {
        this.flashError(error);
        continue;
      }
      valid.push(file);
    }
    if (valid.length === 0) return;
    if (valid.length > this.remaining()) {
      this.flashError(`Can only add ${this.remaining()} more file(s).`);
      return;
    }

    const startIndex = this.pending().length;
    const newPending: PendingFileAttachment[] = valid.map(file => ({ file, isUploading: true }));
    this.pending.update(current => [...current, ...newPending]);
    valid.forEach((file, offset) => this.uploadFile(personalityId, file, startIndex + offset));
  }

  private uploadFile(personalityId: string, file: File, pendingIndex: number): void {
    this.fileAttachmentService.uploadPersonalityFileAttachment(personalityId, file).subscribe({
      next: attachment => {
        this.pending.update(current => current.filter((_, i) => i !== pendingIndex));
        this.attachments.update(current => [...current, attachment]);
      },
      error: err => {
        console.error('Failed to upload', err);
        const message = err?.message ?? 'Upload failed';
        this.pending.update(current =>
          current.map((p, i) => (i === pendingIndex ? { ...p, isUploading: false, uploadError: message } : p)),
        );
        this.flashError(`Failed to upload "${file.name}": ${message}`);
      },
    });
  }

  onRetry(index: number): void {
    const personalityId = this.personalityId();
    if (!personalityId) return;
    const item = this.pending()[index];
    if (!item || !item.file || item.isUploading) return;
    this.pending.update(current =>
      current.map((p, i) => (i === index ? { ...p, isUploading: true, uploadError: undefined } : p)),
    );
    this.uploadFile(personalityId, item.file, index);
  }

  onRemovePending(index: number): void {
    this.pending.update(current => current.filter((_, i) => i !== index));
  }

  async onDelete(attachment: FileAttachment): Promise<void> {
    const confirmed = await this.confirmation.confirm({
      title: 'Delete attachment',
      message: `Delete "${attachment.name}"?`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.fileAttachmentService.deleteFileAttachment(attachment.id).subscribe({
      next: () => {
        this.attachments.update(current => current.filter(a => a.id !== attachment.id));
      },
      error: err => {
        console.error('Failed to delete attachment', err);
        this.flashError(`Failed to delete "${attachment.name}".`);
      },
    });
  }

  private validateFile(file: File): string | null {
    if (file.size > MAX_FILE_SIZE) {
      return `"${file.name}" is too large. Max 10MB.`;
    }
    const dot = file.name.lastIndexOf('.');
    const ext = dot >= 0 ? file.name.substring(dot).toLowerCase() : '';
    if (!SUPPORTED_FILE_TYPES.includes(ext)) {
      return `Type "${ext || 'unknown'}" is not supported.`;
    }
    return null;
  }

  private flashError(message: string): void {
    this.errorMessage.set(message);
    setTimeout(() => {
      if (this.errorMessage() === message) this.errorMessage.set(null);
    }, 4000);
  }
}

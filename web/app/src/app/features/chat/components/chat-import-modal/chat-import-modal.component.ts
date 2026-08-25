
import { ChangeDetectionStrategy, Component, DestroyRef, effect, inject, input, output, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { EmptyError, Subject, firstValueFrom, forkJoin, timer } from 'rxjs';
import { takeUntil } from 'rxjs/operators';

import { ChatService } from '../../../../core/services/chat.service';
import { JobService } from '../../../../core/services/job.service';
import { Chat } from '../../../../core/models/chat.model';
import { ChatImportProgress, Job } from '../../../../core/models/job.model';
import { ModalComponent } from '../../../../shared/ui/modal/modal.component';
import { ChatImportPickerComponent } from './chat-import-picker.component';
import { readableSize } from './chat-import-modal.utils';
import { ConversationImportChunkService, CHAT_IMPORT_CHUNK_BYTES } from './conversation-import-chunk.service';
import { ConversationZipService } from './conversation-zip.service';

/**
 * Above this, a .zip is too large to safely unzip in-browser; we ask the user to upload the extracted
 * conversations.json directly instead of holding the whole archive in memory.
 */
const MAX_ZIP_BYTES = 400 * 1024 * 1024;

/** Most-recent imported threads offered in the post-import picker. */
const CANDIDATE_LIMIT = 30;
/** Max threads a user can prepare (rehydrate + seed memories) in one pass — keeps cost bounded. */
const MAX_SELECT = 5;

type Stage = 'select' | 'extracting' | 'ready' | 'uploading' | 'running' | 'picker' | 'preparing' | 'done' | 'error';

/**
 * Decodes a persisted chat-import progress payload. The parsing-phase payload intentionally has
 * no counts, so it must not be treated as a zero-count update and overwrite a prior real count.
 */
export function parseChatImportProgress(raw: unknown): ChatImportProgress | null {
  if (raw == null || raw === '') return null;

  let parsed: unknown = raw;
  if (typeof raw === 'string') {
    try {
      parsed = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (!parsed || typeof parsed !== 'object') return null;

  const value = parsed as Record<string, unknown>;
  const imported = parseImportCount(value['imported']);
  const skipped = parseImportCount(value['skipped']);
  const total = parseImportCount(value['total']);
  if (imported === null || skipped === null || total === null) return null;

  return {
    phase: typeof value['phase'] === 'string' ? value['phase'] : '',
    ...(typeof value['source'] === 'string' ? { source: value['source'] } : {}),
    imported,
    skipped,
    total,
    ...(Array.isArray(value['imported_ids']) && value['imported_ids'].every(id => typeof id === 'string')
      ? { imported_ids: value['imported_ids'] }
      : {}),
  };
}

function parseImportCount(value: unknown): number | null {
  const count = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : Number.NaN;
  return Number.isInteger(count) && count >= 0 ? count : null;
}

@Component({
  selector: 'app-chat-import-modal',
  standalone: true,
  imports: [ModalComponent, ChatImportPickerComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <ui-modal [open]="open()" [labelledBy]="titleId" size="md" (dismiss)="onDismiss()">
      <div modal-header>
        <h2 [id]="titleId" class="import__title">Import conversations</h2>
        <p class="import__subtitle">Bring in your ChatGPT or Claude history. Imported chats land in your archive.</p>
      </div>

      <div class="import">
        @switch (stage()) {
          @case ('done') {
            <div class="import__result" role="status">
              <div class="import__result-icon import__result-icon--ok" aria-hidden="true">✓</div>
              <p class="import__result-title">Import complete</p>
              <p class="import__result-detail">
                @if ((progress()?.imported ?? 0) === 0 && (progress()?.skipped ?? 0) > 0) {
                  No new threads — all {{ progress()?.skipped }} were already imported.
                } @else {
                  Imported {{ progress()?.imported ?? 0 }} {{ (progress()?.imported ?? 0) === 1 ? 'thread' : 'threads' }}@if ((progress()?.skipped ?? 0) > 0) {, skipped {{ progress()?.skipped }} already-imported}.
                }
              </p>
              @if (preparedCount() > 0) {
                <p class="import__hint">
                  Preparing {{ preparedCount() }} {{ preparedCount() === 1 ? 'thread' : 'threads' }} now — {{ preparedCount() === 1 ? 'it' : 'they' }}'ll appear in your active list with a summary and seeded memories shortly.
                </p>
              } @else {
                <p class="import__hint">
                  Find them under the <strong>Archived</strong> tab. Restoring a thread prepares it for chat automatically.
                </p>
              }
            </div>
          }
          @case ('picker') {
            <app-chat-import-picker
              [candidates]="candidates()"
              [loading]="candidatesLoading()"
              [selectedIds]="selectedIds()"
              [maxSelect]="MAX_SELECT"
              (toggle)="toggleSelect($event)"
            />
          }
          @case ('preparing') {
            <div class="import__progress" role="status" aria-live="polite">
              <div class="import__spinner" aria-hidden="true"></div>
              <p class="import__progress-label">Preparing your threads…</p>
            </div>
          }
          @case ('error') {
            <div class="import__result" role="alert">
              <div class="import__result-icon import__result-icon--err" aria-hidden="true">!</div>
              <p class="import__result-title">Import failed</p>
              <p class="import__result-detail">{{ errorMsg() }}</p>
            </div>
          }
          @default {
            @if (stage() === 'uploading' || stage() === 'running') {
              <div class="import__progress" role="status" aria-live="polite">
                <div class="import__spinner" aria-hidden="true"></div>
                <p class="import__progress-label">{{ progressLabel() }}</p>
                @if ((progress()?.total ?? 0) > 0) {
                  <div class="import__bar" [attr.aria-valuenow]="progressPercent()" aria-valuemin="0" aria-valuemax="100" role="progressbar">
                    <span class="import__bar-fill" [style.width.%]="progressPercent()"></span>
                  </div>
                  <p class="import__progress-count">{{ (progress()?.imported ?? 0) + (progress()?.skipped ?? 0) }} / {{ progress()?.total }} threads</p>
                }
              </div>
            } @else {
              <label class="import__drop" [class.import__drop--busy]="stage() === 'extracting'">
                <input
                  type="file"
                  class="import__file-input"
                  accept=".json,.zip,application/json,application/zip"
                  (change)="onFileSelected($event)"
                  [disabled]="stage() === 'extracting'"
                />
                <span class="import__drop-icon" aria-hidden="true">⬆</span>
                @if (stage() === 'extracting') {
                  <span class="import__drop-text">Extracting conversations.json…</span>
                } @else if (selectedName()) {
                  <span class="import__drop-text">{{ selectedName() }}</span>
                  <span class="import__drop-sub">{{ readySubtitle() }}</span>
                } @else {
                  <span class="import__drop-text">Choose a file</span>
                  <span class="import__drop-sub">conversations.json or the full export .zip</span>
                }
              </label>

              @if (errorMsg()) {
                <p class="import__inline-error" role="alert">{{ errorMsg() }}</p>
              }

              <ul class="import__tips">
                <li>From ChatGPT: Settings → Data controls → Export, then drop the export <code>.zip</code> here (or unzip and pick <code>conversations.json</code>).</li>
                <li>From Claude: Settings → Privacy → Export Data, then drop the <code>.zip</code> or <code>conversations.json</code>.</li>
                <li>Imported threads land in your <strong>Archived</strong> tab. You can prepare a few right away, or restore them later.</li>
                <li>Large ChatGPT exports may be split into <code>conversations-000.json</code>, <code>conversations-001.json</code>, … — drop the whole <code>.zip</code> and we’ll merge them, or import each shard one at a time.</li>
              </ul>
            }
          }
        }
      </div>

      <div modal-footer class="import__footer">
        @if (stage() === 'done' || stage() === 'error') {
          <button type="button" class="import__btn import__btn--primary" (click)="onDismiss()">Close</button>
        } @else if (stage() === 'picker') {
          <button type="button" class="import__btn import__btn--ghost" (click)="skipPicker()">Skip for now</button>
          <button
            type="button"
            class="import__btn import__btn--primary"
            (click)="prepareSelected()"
            [disabled]="selectedCount() === 0"
          >
            Prepare {{ selectedCount() }} {{ selectedCount() === 1 ? 'thread' : 'threads' }}
          </button>
        } @else if (stage() === 'preparing') {
          <button type="button" class="import__btn import__btn--primary" disabled>Preparing…</button>
        } @else {
          <button type="button" class="import__btn import__btn--ghost" (click)="onDismiss()" [disabled]="stage() === 'uploading'">Cancel</button>
          <button
            type="button"
            class="import__btn import__btn--primary"
            (click)="startImport()"
            [disabled]="stage() !== 'ready'"
          >
            {{ stage() === 'uploading' || stage() === 'running' ? 'Importing…' : 'Import' }}
          </button>
        }
      </div>
    </ui-modal>
  `,
  styles: [`
    .import { display: flex; flex-direction: column; gap: 1rem; }
    .import__title { color: var(--color-text-primary); font-size: 1.0625rem; font-weight: 700; margin: 0; }
    .import__subtitle { color: var(--color-text-muted); font-size: 0.8125rem; margin: 0.25rem 0 0; }

    .import__drop {
      align-items: center;
      background: var(--color-surface-muted);
      border: 1.5px dashed var(--color-border-base);
      border-radius: 0.625rem;
      cursor: pointer;
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
      padding: 1.75rem 1rem;
      position: relative;
      text-align: center;
      transition: border-color 0.15s, background 0.15s;
    }
    .import__drop:hover { border-color: var(--color-accent); }
    .import__drop--busy { cursor: progress; opacity: 0.8; }
    .import__file-input { inset: 0; opacity: 0; position: absolute; width: 100%; height: 100%; cursor: pointer; }
    .import__file-input:disabled { cursor: progress; }
    .import__drop-icon { color: var(--color-accent); font-size: 1.5rem; line-height: 1; }
    .import__drop-text { color: var(--color-text-primary); font-size: 0.875rem; font-weight: 600; word-break: break-all; }
    .import__drop-sub { color: var(--color-text-muted); font-size: 0.75rem; }

    .import__tips { color: var(--color-text-muted); display: flex; flex-direction: column; gap: 0.375rem; font-size: 0.75rem; margin: 0; padding-left: 1rem; }
    .import__tips code { background: var(--color-surface-muted); border-radius: 4px; padding: 0 0.25rem; }

    .import__inline-error { color: var(--color-danger); font-size: 0.8125rem; margin: 0; }

    .import__progress { align-items: center; display: flex; flex-direction: column; gap: 0.75rem; padding: 1.5rem 0; }
    .import__progress-label { color: var(--color-text-primary); font-size: 0.875rem; font-weight: 600; margin: 0; }
    .import__progress-count { color: var(--color-text-muted); font-size: 0.75rem; margin: 0; }
    .import__bar { background: var(--color-surface-muted); border-radius: 999px; height: 0.5rem; overflow: hidden; width: 100%; }
    .import__bar-fill { background: var(--color-accent); border-radius: 999px; display: block; height: 100%; transition: width 0.3s ease; }
    .import__spinner {
      border: 3px solid var(--color-surface-muted);
      border-top-color: var(--color-accent);
      border-radius: 50%;
      height: 1.75rem; width: 1.75rem;
      animation: import-spin 0.8s linear infinite;
    }
    @keyframes import-spin { to { transform: rotate(360deg); } }

    .import__result { align-items: center; display: flex; flex-direction: column; gap: 0.5rem; padding: 1rem 0; text-align: center; }
    .import__result-icon { align-items: center; border-radius: 50%; display: flex; font-size: 1.25rem; font-weight: 700; height: 2.5rem; justify-content: center; width: 2.5rem; }
    .import__result-icon--ok { background: color-mix(in srgb, var(--color-accent) 18%, transparent); color: var(--color-accent); }
    .import__result-icon--err { background: color-mix(in srgb, var(--color-danger) 18%, transparent); color: var(--color-danger); }
    .import__result-title { color: var(--color-text-primary); font-size: 0.9375rem; font-weight: 700; margin: 0; }
    .import__result-detail { color: var(--color-text-secondary); font-size: 0.8125rem; margin: 0; }
    .import__hint { color: var(--color-text-muted); font-size: 0.75rem; margin: 0.25rem 0 0; }

    .import__footer { display: flex; gap: 0.5rem; justify-content: flex-end; }
    .import__btn { border-radius: 0.5rem; cursor: pointer; font-size: 0.8125rem; font-weight: 600; padding: 0.4375rem 1rem; }
    .import__btn:disabled { cursor: not-allowed; opacity: 0.55; }
    .import__btn--ghost { background: transparent; border: 1px solid var(--color-border-base); color: var(--color-text-primary); }
    .import__btn--primary { background: var(--color-accent); border: 0; color: #fff; }
  `],
})
export class ChatImportModalComponent {
  readonly open = input<boolean>(false);
  readonly dismiss = output<void>();
  /** Emitted when the import finished and at least one thread was created, so the parent can refresh. */
  readonly imported = output<void>();

  readonly titleId = `chat-import-${Math.random().toString(36).slice(2, 10)}`;

  /** Exposed to the template for the selection cap copy/limit. */
  readonly MAX_SELECT = MAX_SELECT;
  readonly readableSize = readableSize;

  readonly stage = signal<Stage>('select');
  readonly selectedName = signal<string>('');
  readonly selectedSize = signal<number>(0);
  readonly errorMsg = signal<string | null>(null);
  readonly progress = signal<ChatImportProgress | null>(null);
  readonly chunkIndex = signal(0);
  readonly chunkCount = signal(1);

  // Post-import picker state.
  readonly candidates = signal<Chat[]>([]);
  readonly candidatesLoading = signal<boolean>(false);
  readonly selectedIds = signal<Set<string>>(new Set());
  readonly preparedCount = signal<number>(0);
  readonly importedChatIds = signal<string[]>([]);

  private payloads: Blob[] = [];
  private payloadName = 'conversations.json';
  private importTotalConversations = 0;
  private importRunID = 0;
  /** Emits when the modal closes or resets so in-flight import HTTP/polling stops. */
  private readonly importAbort$ = new Subject<void>();

  private readonly chatService = inject(ChatService);
  private readonly jobService = inject(JobService);
  private readonly zipService = inject(ConversationZipService);
  private readonly chunkService = inject(ConversationImportChunkService);
  private readonly destroyRef = inject(DestroyRef);

  constructor() {
    // Reset whenever the modal is (re)opened; abort in-flight work when it closes.
    effect(() => {
      if (this.open()) {
        this.reset();
      } else {
        this.abortInFlightImport();
      }
    });
    this.destroyRef.onDestroy(() => this.abortInFlightImport());
  }

  private abortInFlightImport(): void {
    this.importRunID++;
    this.importAbort$.next();
  }

  progressLabel(): string {
    const p = this.progress();
    const parts = this.chunkCount();
    const part = this.chunkIndex();
    const partLabel = parts > 1 ? ` (part ${part} of ${parts})` : '';
    if (this.stage() === 'uploading') return `Uploading export${partLabel}…`;
    if (!p) return `Preparing import${partLabel}…`;
    switch (p.phase) {
      case 'parsing': return `Reading conversations${partLabel}…`;
      case 'importing': return `Importing threads${partLabel}…`;
      default: return `Working${partLabel}…`;
    }
  }

  readySubtitle(): string {
    const size = readableSize(this.selectedSize());
    const parts = this.chunkCount();
    if (parts > 1) {
      return `${size} · ready to import in ${parts} parts`;
    }
    return `${size} · ready to import`;
  }

  progressPercent(): number {
    const p = this.progress();
    if (!p || p.total <= 0) return 0;
    return Math.min(100, Math.round(((p.imported + p.skipped) / p.total) * 100));
  }

  async onFileSelected(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    // Allow re-selecting the same file later.
    input.value = '';
    if (!file) return;

    this.errorMsg.set(null);
    this.payloads = [];
    this.chunkCount.set(1);
    this.importTotalConversations = 0;
    this.selectedName.set('');
    this.selectedSize.set(0);

    const isZip = file.name.toLowerCase().endsWith('.zip') || file.type === 'application/zip' || file.type === 'application/x-zip-compressed';

    if (isZip) {
      if (file.size > MAX_ZIP_BYTES) {
        this.errorMsg.set('That .zip is very large. Please unzip it and upload conversations.json directly.');
        this.stage.set('select');
        return;
      }
      this.stage.set('extracting');
      try {
        const extracted = await this.zipService.extractConversationsJson(file);
        await this.preparePayload(extracted, 'conversations.json');
      } catch (err) {
        this.errorMsg.set(err instanceof Error ? err.message : 'Could not read that .zip file.');
        this.stage.set('select');
      }
      return;
    }

    if (!file.name.toLowerCase().endsWith('.json') && file.type !== 'application/json') {
      this.errorMsg.set('Unsupported file. Upload conversations.json or the export .zip.');
      this.stage.set('select');
      return;
    }
    const base = file.name.split('/').pop()?.toLowerCase() ?? '';
    if (/^conversations-\d+\.json$/.test(base)) {
      // Single shard is fine (threads aren't split across files). Prefer the zip when many exist.
      console.info(
        `Chat import: uploading numbered shard ${file.name}. Prefer dropping the full export .zip to merge all shards at once, or import each conversations-*.json one at a time.`,
      );
    }
    await this.preparePayload(file, file.name);
  }

  private async preparePayload(blob: Blob, name: string): Promise<void> {
    if (blob.size === 0) {
      this.errorMsg.set('That file is empty.');
      this.stage.set('select');
      return;
    }

    const needsSplit = blob.size > CHAT_IMPORT_CHUNK_BYTES;
    if (needsSplit) {
      this.stage.set('extracting');
    }

    try {
      const { chunks, totalConversations } = await this.chunkService.splitExport(blob);
      this.payloads = chunks;
      this.payloadName = name;
      this.chunkCount.set(chunks.length);
      this.importTotalConversations = totalConversations;
      this.selectedName.set(name);
      this.selectedSize.set(blob.size);
      this.stage.set('ready');
    } catch (err) {
      this.errorMsg.set(err instanceof Error ? err.message : 'Could not prepare that export.');
      this.stage.set('select');
    }
  }

  startImport(): void {
    if (this.payloads.length === 0 || this.stage() !== 'ready') return;
    this.errorMsg.set(null);
    const importRunID = ++this.importRunID;
    void this.runChunkedImport(importRunID);
  }

  private async runChunkedImport(importRunID: number): Promise<void> {
    let aggImported = 0;
    let aggSkipped = 0;
    const aggTotal = this.importTotalConversations;
    const importedIds: string[] = [];

    try {
      for (let i = 0; i < this.payloads.length; i++) {
        this.chunkIndex.set(i + 1);
        this.stage.set('uploading');

        const job = await firstValueFrom(
          this.chatService.importConversations(this.payloads[i], this.payloadName).pipe(
            takeUntil(this.importAbort$),
          ),
        );

        this.stage.set('running');
        const { finalJob, lastImported, lastSkipped } = await this.pollJobToCompletion(
          job.id,
          aggImported,
          aggSkipped,
          aggTotal,
          importRunID,
        );
        if (finalJob.status === 'failed') {
          this.errorMsg.set(finalJob.error || 'The import job failed.');
          this.stage.set('error');
          return;
        }

        // Prefer the terminal progress payload; fall back to the last polled tick so a missing /
        // unparseable final progress string cannot zero out a successful import in the UI.
        const chunkProgress = this.parseProgress(finalJob);
        const importedDelta = chunkProgress?.imported ?? lastImported;
        const skippedDelta = chunkProgress?.skipped ?? lastSkipped;
        aggImported += importedDelta;
        aggSkipped += skippedDelta;
        if (chunkProgress?.imported_ids?.length) {
          importedIds.push(...chunkProgress.imported_ids);
        }
        this.progress.set({
          phase: chunkProgress?.phase ?? 'complete',
          source: chunkProgress?.source,
          imported: aggImported,
          skipped: aggSkipped,
          total: aggTotal > 0 ? aggTotal : Math.max(aggImported + aggSkipped, chunkProgress?.total ?? 0),
          imported_ids: importedIds,
        });
      }

      this.importedChatIds.set(importedIds);
      // Always notify the thread list so the archive view refreshes even when every conversation
      // was a dedup skip (imported=0) or the picker has no candidates.
      this.imported.emit();
      if (aggImported > 0) {
        this.loadCandidates();
      } else {
        this.stage.set('done');
      }
    } catch (err) {
      if (err instanceof EmptyError) return;
      this.errorMsg.set(err instanceof Error ? err.message : 'Import failed.');
      this.stage.set('error');
    }
  }

  private async pollJobToCompletion(
    jobId: string,
    aggImported: number,
    aggSkipped: number,
    aggTotal: number,
    importRunID: number,
  ): Promise<{ finalJob: Job; lastImported: number; lastSkipped: number }> {
    let lastImported = 0;
    let lastSkipped = 0;
    for (;;) {
      if (importRunID !== this.importRunID) {
        throw new EmptyError();
      }
      const job = await firstValueFrom(this.jobService.getJob(jobId));
      if (importRunID !== this.importRunID) {
        throw new EmptyError();
      }

      const chunkProgress = this.parseProgress(job);
      if (chunkProgress) {
        lastImported = chunkProgress.imported;
        lastSkipped = chunkProgress.skipped;
        this.progress.set({
          ...chunkProgress,
          imported: aggImported + chunkProgress.imported,
          skipped: aggSkipped + chunkProgress.skipped,
          total: aggTotal > 0 ? aggTotal : aggImported + aggSkipped + chunkProgress.imported + chunkProgress.skipped,
        });
      }

      if (job.status === 'complete' || job.status === 'failed') {
        return { finalJob: job, lastImported, lastSkipped };
      }

      await firstValueFrom(timer(1500));
    }
  }

  private parseProgress(job: Job): ChatImportProgress | null {
    return parseChatImportProgress(job.progress);
  }


  /** Loads threads created in this import run as picker candidates. */
  private loadCandidates(): void {
    const ids = this.importedChatIds();
    if (ids.length === 0) {
      this.stage.set('done');
      return;
    }

    this.stage.set('picker');
    this.candidatesLoading.set(true);
    this.chatService.listChatsPage(1, CANDIDATE_LIMIT, { ids: ids.join(',') })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: page => {
          this.candidates.set(page.results ?? []);
          this.candidatesLoading.set(false);
        },
        error: () => {
          // Import already succeeded; if we can't load candidates just fall through to the summary.
          this.candidatesLoading.set(false);
          this.stage.set('done');
        },
      });
  }

  selectedCount(): number {
    return this.selectedIds().size;
  }

  toggleSelect(id: string): void {
    const next = new Set(this.selectedIds());
    if (next.has(id)) {
      next.delete(id);
    } else if (next.size < MAX_SELECT) {
      next.add(id);
    }
    this.selectedIds.set(next);
  }

  /**
   * Unarchives each selected thread, which triggers the backend rehydration job (summary + window
   * pointer + seeded memories). Threads land in the active list, ready to resume.
   */
  prepareSelected(): void {
    const ids = Array.from(this.selectedIds());
    if (ids.length === 0) {
      this.stage.set('done');
      return;
    }
    this.stage.set('preparing');
    forkJoin(ids.map(id => this.chatService.patchChat(id, { archived: false })))
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: () => {
          this.preparedCount.set(ids.length);
          this.imported.emit();
          this.stage.set('done');
        },
        error: () => {
          this.errorMsg.set('Some threads could not be prepared. You can restore them from the Archived tab.');
          this.stage.set('done');
        },
      });
  }

  skipPicker(): void {
    this.stage.set('done');
  }

  onDismiss(): void {
    this.dismiss.emit();
  }

  private reset(): void {
    this.abortInFlightImport();
    this.stage.set('select');
    this.selectedName.set('');
    this.selectedSize.set(0);
    this.errorMsg.set(null);
    this.progress.set(null);
    this.candidates.set([]);
    this.candidatesLoading.set(false);
    this.selectedIds.set(new Set());
    this.preparedCount.set(0);
    this.importedChatIds.set([]);
    this.payloads = [];
    this.chunkIndex.set(0);
    this.chunkCount.set(1);
    this.importTotalConversations = 0;
    this.payloadName = 'conversations.json';
  }
}

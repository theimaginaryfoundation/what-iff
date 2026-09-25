import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, ViewChild, computed, effect, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { firstValueFrom, switchMap, takeWhile, timer } from 'rxjs';

import {
  AccountActivityEntry,
  AccountExportService,
  AccountImportProgress,
  AccountImportResult,
  AccountImportSelection,
} from '../../core/services/account-export.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { AccountArchiveService, ArchiveContents } from './account-archive.service';
import { ChatImportModalComponent } from '../chat/components/chat-import-modal/chat-import-modal.component';
import { XIconComponent } from '../../shared/ui/icons/icons';
import { ExportDeliveryService } from '../../extensions/export-delivery.service';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';

type ExportPhase = 'idle' | 'queued' | 'building' | 'uploading' | 'complete' | 'failed';
type ImportPhase = 'idle' | 'inspecting' | 'review' | 'queued' | 'uploading' | 'validating' | 'importing' | 'complete' | 'failed';

const TERMINAL_JOB_STATES = ['complete', 'failed', 'cancelled'];

/**
 * The unified Import & Export ("your data") screen. Account export + restore-from-export, promoted
 * from /experimental, with durable structured result reporting and an itemized selection ledger for
 * restores (choose which personalities and threads to bring in; memories are a single toggle). An
 * active account-import job is restored from session storage when the page is recreated.
 */
@Component({
  selector: 'app-data-portability-page',
  standalone: true,
  imports: [ChatImportModalComponent, XIconComponent, TooltipDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './data-portability-page.component.html',
})
export class DataPortabilityPageComponent {
  private readonly accountExportService = inject(AccountExportService);
  private readonly archiveService = inject(AccountArchiveService);
  private readonly confirmationService = inject(ConfirmationService);
  /** Build swap-point: hosted overlays replace the public/local delivery copy. */
  readonly exportDelivery = inject(ExportDeliveryService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly router = inject(Router);

  @ViewChild('accountImportInput') accountImportInput?: ElementRef<HTMLInputElement>;

  // --- Export ---
  readonly exportPhase = signal<ExportPhase>('idle');
  readonly exportMessage = signal<string | null>(null);
  readonly exporting = signal(false);

  // --- Import (restore from a WhatIff export) ---
  readonly importPhase = signal<ImportPhase>('idle');
  readonly importMessage = signal<string | null>(null);
  readonly importing = signal(false);
  readonly importResult = signal<AccountImportResult | null>(null);
  readonly importWarnings = signal<string[]>([]);
  readonly importError = signal<string | null>(null);

  // Selection ledger — populated after inspecting a chosen ZIP, before the restore runs.
  readonly archive = signal<ArchiveContents | null>(null);
  readonly selectedPersonalityIds = signal<Set<string>>(new Set());
  readonly selectedConversationIds = signal<Set<string>>(new Set());
  readonly includeMemories = signal(true);
  private pendingFile: File | null = null;

  readonly nothingSelected = computed(
    () => this.selectedPersonalityIds().size === 0 && this.selectedConversationIds().size === 0 && !this.includeMemories(),
  );

  // --- Activity log ---
  readonly activity = signal<AccountActivityEntry[]>([]);
  private polledImportJobID: string | null = null;

  constructor() {
    this.loadActivity();
    effect(() => {
      const jobID = this.accountExportService.activeImportJobId();
      if (!jobID || jobID === this.polledImportJobID) return;

      this.polledImportJobID = jobID;
      this.importing.set(true);
      this.importPhase.set('queued');
      this.importMessage.set('Checking your account import…');
      this.pollAccountImport(jobID);
    });
  }

  /** (Re)loads the recent import/export activity log. Called on load and after each run completes. */
  loadActivity(): void {
    this.accountExportService
      .getActivity()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: entries => this.activity.set(entries),
        // A failed activity fetch is non-fatal — the log just stays as-is.
        error: () => undefined,
      });
  }

  // ================= Export =================

  async requestAccountExport(): Promise<void> {
    if (this.exporting()) return;
    const confirmed = await this.confirmationService.confirm({
      title: 'Export account data?',
      message: this.exportDelivery.copy.confirmation,
      confirmText: 'Confirm export',
      type: 'warning',
    });
    if (!confirmed) return;

    this.exporting.set(true);
    this.exportPhase.set('queued');
    this.exportMessage.set('Preparing your export request…');
    this.accountExportService
      .enqueue()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: job => this.pollAccountExport(job.id),
        error: error => {
          this.exporting.set(false);
          this.exportPhase.set('failed');
          this.exportMessage.set(this.errorMessage(error, 'Unable to request an account export.'));
        },
      });
  }

  // ================= Import: choose + inspect =================

  openAccountImportPicker(): void {
    if (!this.importing() && this.importPhase() !== 'inspecting') {
      this.accountImportInput?.nativeElement.click();
    }
  }

  /** After a ZIP is chosen we read it locally to build the selection ledger, before any upload. */
  async onArchiveFileChosen(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file || this.importing() || this.importPhase() === 'inspecting') return;

    this.resetImportState();
    this.importPhase.set('inspecting');
    this.importMessage.set('Reading export…');
    try {
      const contents = await this.archiveService.inspect(file);
      this.pendingFile = file;
      this.archive.set(contents);
      // Default to everything selected.
      this.selectedPersonalityIds.set(new Set(contents.personalities.map(p => p.id)));
      this.selectedConversationIds.set(new Set(contents.conversations.map(c => c.id)));
      this.includeMemories.set(contents.hasMemories);
      this.importPhase.set('review');
      this.importMessage.set(null);
    } catch (error) {
      this.resetImportState();
      this.importPhase.set('failed');
      this.importError.set(this.errorMessage(error, 'Could not read that export ZIP.'));
    }
  }

  togglePersonality(id: string, checked: boolean): void {
    this.selectedPersonalityIds.update(set => withToggled(set, id, checked));
  }

  toggleConversation(id: string, checked: boolean): void {
    this.selectedConversationIds.update(set => withToggled(set, id, checked));
  }

  setAllPersonalities(checked: boolean): void {
    const contents = this.archive();
    this.selectedPersonalityIds.set(checked && contents ? new Set(contents.personalities.map(p => p.id)) : new Set());
  }

  setAllConversations(checked: boolean): void {
    const contents = this.archive();
    this.selectedConversationIds.set(checked && contents ? new Set(contents.conversations.map(c => c.id)) : new Set());
  }

  cancelReview(): void {
    this.resetImportState();
    this.importPhase.set('idle');
  }

  // ================= Import: run the selected restore =================

  async startSelectedImport(): Promise<void> {
    const file = this.pendingFile;
    if (!file || this.importing() || this.nothingSelected()) return;

    const personalityIds = [...this.selectedPersonalityIds()];
    const conversationIds = [...this.selectedConversationIds()];
    const includeMemories = this.includeMemories();

    const parts = [
      `${personalityIds.length} personalit${personalityIds.length === 1 ? 'y' : 'ies'}`,
      `${conversationIds.length} thread${conversationIds.length === 1 ? '' : 's'}`,
    ];
    if (includeMemories) parts.push('memories');
    const confirmed = await this.confirmationService.confirm({
      title: 'Import selected data?',
      message: `This adds ${formatList(parts)} to this account. Existing matching items are skipped; nothing here is deleted.`,
      confirmText: 'Import selected',
      type: 'warning',
    });
    if (!confirmed) return;

    const selection: AccountImportSelection = {
      personality_ids: personalityIds,
      conversation_ids: conversationIds,
      include_memories: includeMemories,
    };

    this.importing.set(true);
    this.importPhase.set('uploading');
    this.importMessage.set('Uploading account data…');
    try {
      const job = await firstValueFrom(this.accountExportService.importAccount(file, selection));
      this.pendingFile = null;
      this.archive.set(null);
      this.accountExportService.trackActiveImport(job.id);
    } catch (error) {
      this.importing.set(false);
      this.importPhase.set('failed');
      this.importError.set(this.errorMessage(error, 'Unable to import that account export ZIP.'));
    }
  }

  // ================= polling =================

  private pollAccountExport(jobID: string): void {
    timer(0, 1500)
      .pipe(
        switchMap(() => this.accountExportService.getStatus(jobID)),
        takeWhile(job => !TERMINAL_JOB_STATES.includes(job.status), true),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: job => {
          const progress = this.parseExportProgress(job.progress);
          if (job.status === 'complete') {
            this.exporting.set(false);
            this.exportPhase.set('complete');
            this.exportMessage.set(progress?.message || this.exportDelivery.copy.completed);
            this.loadActivity();
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.exporting.set(false);
            this.exportPhase.set('failed');
            this.exportMessage.set(progress?.message || job.error || 'Your account export could not be completed.');
            this.loadActivity();
          } else {
            const phase = (progress?.phase as ExportPhase) ?? 'building';
            this.exportPhase.set(phase);
            this.exportMessage.set(progress?.message || this.exportPhaseLabel(phase));
          }
        },
        error: error => {
          this.exporting.set(false);
          this.exportPhase.set('failed');
          this.exportMessage.set(this.errorMessage(error, 'Unable to check account export status.'));
        },
      });
  }

  private pollAccountImport(jobID: string): void {
    timer(0, 1500)
      .pipe(
        switchMap(() => this.accountExportService.getImportStatus(jobID)),
        takeWhile(job => !TERMINAL_JOB_STATES.includes(job.status), true),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: job => {
          const progress = this.parseImportProgress(job.progress);
          if (job.status === 'complete') {
            this.accountExportService.clearActiveImport();
            this.importing.set(false);
            this.importPhase.set('complete');
            this.importResult.set(progress?.result ?? progress ?? {});
            this.importWarnings.set(progress?.warnings ?? progress?.result?.warnings ?? []);
            this.importMessage.set(progress?.message || 'Import complete.');
            this.loadActivity();
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.accountExportService.clearActiveImport();
            this.importing.set(false);
            this.importPhase.set('failed');
            this.importError.set(progress?.message || job.error || 'Your account import could not be completed.');
            this.loadActivity();
          } else {
            const phase = (progress?.phase as ImportPhase) ?? 'importing';
            this.importPhase.set(phase);
            this.importMessage.set(progress?.message || this.importPhaseLabel(phase));
          }
        },
        error: error => {
          this.importing.set(false);
          this.importPhase.set('failed');
          this.importError.set(this.errorMessage(error, 'Unable to check account import status.'));
        },
      });
  }

  private resetImportState(): void {
    this.importResult.set(null);
    this.importWarnings.set([]);
    this.importError.set(null);
    this.importMessage.set(null);
    this.archive.set(null);
    this.selectedPersonalityIds.set(new Set());
    this.selectedConversationIds.set(new Set());
    this.includeMemories.set(true);
    this.pendingFile = null;
  }

  private parseExportProgress(raw?: string): { phase?: string; message?: string } | null {
    if (!raw) return null;
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      return {
        phase: typeof parsed['phase'] === 'string' ? parsed['phase'] : undefined,
        message: typeof parsed['message'] === 'string' ? parsed['message'] : undefined,
      };
    } catch {
      return null;
    }
  }

  private parseImportProgress(raw?: string): AccountImportProgress | null {
    if (!raw) return null;
    try {
      return JSON.parse(raw) as AccountImportProgress;
    } catch {
      return null;
    }
  }

  private exportPhaseLabel(phase?: string): string {
    switch (phase) {
      case 'building':
        return 'Building your export…';
      case 'uploading':
        return 'Securing your export for delivery…';
      default:
        return 'Your export is queued…';
    }
  }

  private importPhaseLabel(phase?: string): string {
    switch (phase) {
      case 'validating':
        return 'Validating your account export…';
      case 'importing':
        return 'Importing account data…';
      default:
        return 'Your account import is queued…';
    }
  }

  /** Friendly label for an activity row. */
  activityLabel(entry: AccountActivityEntry): string {
    if (entry.category === 'chat_import') return 'ChatGPT / Claude import';
    switch (entry.action) {
      case 'exported':
        return 'Account export';
      case 'imported':
        return 'Account restore';
      case 'import_failed':
        return 'Account restore failed';
      default:
        return entry.action;
    }
  }

  /** True when an activity row represents a failure (for styling). */
  activityFailed(entry: AccountActivityEntry): boolean {
    return entry.action.includes('failed') || entry.metadata?.['success'] === false;
  }

  /** The human message with structured activity metadata rendered as a compact "key: value" summary. */
  activityDetail(entry: AccountActivityEntry): string {
    const parts = Object.entries(entry.metadata ?? {})
      .filter(([, v]) => typeof v === 'number' || typeof v === 'boolean' || typeof v === 'string')
      .map(([k, v]) => `${k.replace(/_/g, ' ')}: ${v}`);
    return parts.length ? `${entry.message} — ${parts.join(', ')}` : entry.message;
  }

  formatWhen(iso: string): string {
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  close(): void {
    void this.router.navigate(['/chat']);
  }

  private errorMessage(error: unknown, fallback: string): string {
    const err = error as { error?: { error?: string; message?: string }; message?: string };
    return err?.error?.error || err?.error?.message || err?.message || fallback;
  }
}

/** Returns a new Set with `id` added (checked) or removed (unchecked). */
function withToggled(set: Set<string>, id: string, checked: boolean): Set<string> {
  const next = new Set(set);
  if (checked) {
    next.add(id);
  } else {
    next.delete(id);
  }
  return next;
}

function formatList(items: string[]): string {
  if (items.length < 2) return items.join('');
  if (items.length === 2) return items.join(' and ');
  return `${items.slice(0, -1).join(', ')}, and ${items.at(-1)}`;
}

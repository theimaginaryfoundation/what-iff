import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, ViewChild, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { firstValueFrom, switchMap, takeWhile, timer } from 'rxjs';

import { AccountExportService, AccountImportProgress, AccountImportResult } from '../../core/services/account-export.service';
import { ConfirmationService } from '../../core/services/confirmation.service';

type ExportPhase = 'idle' | 'queued' | 'building' | 'uploading' | 'complete' | 'failed';
type ImportPhase = 'idle' | 'uploading' | 'validating' | 'importing' | 'complete' | 'failed';

const TERMINAL_JOB_STATES = ['complete', 'failed', 'cancelled'];

/**
 * The unified Import & Export ("your data") screen. Promotes the account
 * export/import that used to live behind /experimental into a first-class screen
 * reachable from settings and the fresh-user empty state, with durable,
 * structured result reporting rather than a one-line status string.
 *
 * Iterating: this first cut covers account export + restore-from-export. Still to
 * land — an itemized personalities/threads selection ledger on restore, and the
 * ChatGPT/Claude conversation import migrated off the sidebar popup onto here.
 */
@Component({
  selector: 'app-data-portability-page',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './data-portability-page.component.html',
})
export class DataPortabilityPageComponent {
  private readonly accountExportService = inject(AccountExportService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly destroyRef = inject(DestroyRef);

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

  async requestAccountExport(): Promise<void> {
    if (this.exporting()) return;
    const confirmed = await this.confirmationService.confirm({
      title: 'Export account data?',
      message:
        'We’ll prepare a ZIP of your conversations, personalities, and memories, then email a download link to your account address. The link expires after 24 hours.',
      confirmText: 'Export account data',
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

  openAccountImportPicker(): void {
    if (!this.importing()) {
      this.accountImportInput?.nativeElement.click();
    }
  }

  async importAccountFile(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file || this.importing()) return;

    const confirmed = await this.confirmationService.confirm({
      title: 'Import account data?',
      message:
        'This adds conversations, personalities, and memories from the WhatIff ZIP to this account. Existing matching data is skipped; nothing in this account is deleted.',
      confirmText: 'Import account data',
      type: 'warning',
    });
    if (!confirmed) return;

    this.resetImportResult();
    this.importing.set(true);
    this.importPhase.set('uploading');
    this.importMessage.set('Uploading account data…');
    try {
      const job = await firstValueFrom(this.accountExportService.importAccount(file));
      this.pollAccountImport(job.id);
    } catch (error) {
      this.importing.set(false);
      this.importPhase.set('failed');
      this.importError.set(this.errorMessage(error, 'Unable to import that account export ZIP.'));
    }
  }

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
            this.exportMessage.set(progress?.message || 'Your export is ready — check your email for the download link.');
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.exporting.set(false);
            this.exportPhase.set('failed');
            this.exportMessage.set(progress?.message || job.error || 'Your account export could not be completed.');
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
            this.importing.set(false);
            this.importPhase.set('complete');
            this.importResult.set(progress?.result ?? progress ?? {});
            this.importWarnings.set(progress?.warnings ?? progress?.result?.warnings ?? []);
            this.importMessage.set(progress?.message || 'Import complete.');
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.importing.set(false);
            this.importPhase.set('failed');
            this.importError.set(progress?.message || job.error || 'Your account import could not be completed.');
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

  private resetImportResult(): void {
    this.importResult.set(null);
    this.importWarnings.set([]);
    this.importError.set(null);
    this.importMessage.set(null);
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

  private errorMessage(error: unknown, fallback: string): string {
    const err = error as { error?: { error?: string; message?: string }; message?: string };
    return err?.error?.error || err?.error?.message || err?.message || fallback;
  }
}

import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, ViewChild, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { firstValueFrom, switchMap, takeWhile, timer } from 'rxjs';

import { AccountExportService, AccountImportProgress, AccountImportResult } from '../../core/services/account-export.service';
import { ConfirmationService } from '../../core/services/confirmation.service';

@Component({
  selector: 'app-experimental-page',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './experimental-page.component.html',
})
export class ExperimentalPageComponent {
  private readonly accountExportService = inject(AccountExportService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly destroyRef = inject(DestroyRef);

  @ViewChild('accountImportInput') accountImportInput?: ElementRef<HTMLInputElement>;

  readonly accountExporting = signal(false);
  readonly accountImporting = signal(false);
  readonly accountExportStatus = signal<string | null>(null);
  readonly accountImportStatus = signal<string | null>(null);

  async requestAccountExport(): Promise<void> {
    if (this.accountExporting()) return;
    const confirmed = await this.confirmationService.confirm({
      title: 'Export account data?',
      message:
        'We’ll prepare a ZIP of your conversations, personalities, and memories, then email a download link to your account address. The link expires after 24 hours.',
      confirmText: 'Export account data',
      type: 'warning',
    });
    if (!confirmed) return;

    this.accountExporting.set(true);
    this.accountExportStatus.set('Preparing your export request…');
    this.accountExportService
      .enqueue()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: job => this.pollAccountExport(job.id),
        error: error => {
          this.accountExporting.set(false);
          this.accountExportStatus.set(this.errorMessage(error, 'Unable to request an account export.'));
        },
      });
  }

  openAccountImportPicker(): void {
    if (!this.accountImporting()) {
      this.accountImportInput?.nativeElement.click();
    }
  }

  async importAccountFile(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file || this.accountImporting()) return;

    const confirmed = await this.confirmationService.confirm({
      title: 'Import account data?',
      message:
        'This adds conversations, personalities, and memories from the Whatiff ZIP to this account. Existing matching data is skipped; nothing in this account is deleted.',
      confirmText: 'Import account data',
      type: 'warning',
    });
    if (!confirmed) return;

    this.accountImporting.set(true);
    this.accountImportStatus.set('Uploading account data…');
    try {
      const job = await firstValueFrom(this.accountExportService.importAccount(file));
      this.pollAccountImport(job.id);
    } catch (error) {
      this.accountImportStatus.set(this.errorMessage(error, 'Unable to import that account export ZIP.'));
      this.accountImporting.set(false);
    }
  }

  private pollAccountExport(jobID: string): void {
    timer(0, 1500)
      .pipe(
        switchMap(() => this.accountExportService.getStatus(jobID)),
        takeWhile(job => !['complete', 'failed', 'cancelled'].includes(job.status), true),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: job => {
          const progress = this.parseAccountExportProgress(job.progress);
          if (job.status === 'complete') {
            this.accountExporting.set(false);
            this.accountExportStatus.set(progress?.message || 'Your export is ready. Check your email for the download link.');
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.accountExporting.set(false);
            this.accountExportStatus.set(progress?.message || job.error || 'Your account export could not be completed.');
          } else {
            this.accountExportStatus.set(progress?.message || this.accountExportPhaseLabel(progress?.phase));
          }
        },
        error: error => {
          this.accountExporting.set(false);
          this.accountExportStatus.set(this.errorMessage(error, 'Unable to check account export status.'));
        },
      });
  }

  private pollAccountImport(jobID: string): void {
    timer(0, 1500)
      .pipe(
        switchMap(() => this.accountExportService.getImportStatus(jobID)),
        takeWhile(job => !['complete', 'failed', 'cancelled'].includes(job.status), true),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: job => {
          const progress = this.parseAccountImportProgress(job.progress);
          if (job.status === 'complete') {
            this.accountImporting.set(false);
            this.accountImportStatus.set(this.formatAccountImportResult(progress?.result ?? progress ?? {}));
          } else if (job.status === 'failed' || job.status === 'cancelled') {
            this.accountImporting.set(false);
            this.accountImportStatus.set(progress?.message || job.error || 'Your account import could not be completed.');
          } else {
            this.accountImportStatus.set(progress?.message || this.accountImportPhaseLabel(progress?.phase));
          }
        },
        error: error => {
          this.accountImporting.set(false);
          this.accountImportStatus.set(this.errorMessage(error, 'Unable to check account import status.'));
        },
      });
  }

  private parseAccountExportProgress(raw?: string): { phase?: string; message?: string } | null {
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

  private parseAccountImportProgress(raw?: string): AccountImportProgress | null {
    if (!raw) return null;
    try {
      return JSON.parse(raw) as AccountImportProgress;
    } catch {
      return null;
    }
  }

  private accountImportPhaseLabel(phase?: string): string {
    switch (phase) {
      case 'validating':
        return 'Validating your account export…';
      case 'importing':
        return 'Importing account data…';
      default:
        return 'Your account import is queued…';
    }
  }

  private accountExportPhaseLabel(phase?: string): string {
    switch (phase) {
      case 'building':
        return 'Building your export…';
      case 'uploading':
        return 'Securing your export for delivery…';
      default:
        return 'Your export is queued…';
    }
  }

  private formatAccountImportResult(result: AccountImportResult): string {
    const conversations = result.conversations;
    const memories = result.memories;
    const personalities = result.personalities;
    const warnings = result.warnings ?? [];
    const parts = [
      `Conversations: ${conversations?.imported ?? 0} imported, ${conversations?.skipped ?? 0} skipped`,
      `Personalities: ${personalities?.created ?? 0} created, ${personalities?.skipped ?? 0} skipped`,
      `Memories: ${memories?.imported_count ?? 0} imported, ${memories?.duplicate_count ?? 0} duplicates skipped`,
    ];
    if ((memories?.invalid_record_count ?? 0) > 0) {
      parts.push(`${memories!.invalid_record_count} invalid memory records skipped`);
      const reasons = memories?.invalid_reasons;
      const labels = [
        { count: reasons?.malformed_json ?? 0, label: 'malformed JSON' },
        { count: reasons?.missing_id ?? 0, label: 'missing ID' },
        { count: reasons?.empty_content ?? 0, label: 'empty content' },
        { count: reasons?.missing_created_at ?? 0, label: 'missing creation time' },
        { count: reasons?.missing_chat_id ?? 0, label: 'missing chat ID' },
      ]
        .filter(reason => reason.count > 0)
        .map(reason => `${reason.count} ${reason.label}`);
      if (labels.length > 0) {
        parts.push(`Invalid memory details: ${labels.join(', ')}`);
      }
    }
    const missingReferences = (memories?.skipped_missing_chat_count ?? 0) + (memories?.skipped_missing_personality_count ?? 0);
    if (missingReferences > 0) {
      parts.push(`${missingReferences} memories skipped because their chat or personality was unavailable`);
    }
    if ((conversations?.errors?.length ?? 0) > 0) {
      parts.push(`${conversations!.errors!.length} conversations failed`);
    }
    return `${parts.join('. ')}.${warnings.length ? ` Warnings: ${warnings.join(' ')}` : ''}`;
  }

  private errorMessage(error: unknown, fallback: string): string {
    const err = error as { error?: { error?: string; message?: string }; message?: string };
    return err?.error?.error || err?.error?.message || err?.message || fallback;
  }
}

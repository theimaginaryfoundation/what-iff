import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { MemoryService } from '../../../core/services/memory.service';

@Component({
  selector: 'app-memory-export-button',
  standalone: true,
  template: `
    <button
      type="button"
      class="rounded-lg border border-border-base px-3 py-2 text-xs text-text-secondary hover:text-text-primary disabled:opacity-50"
      [disabled]="exporting()"
      (click)="export()">
      {{ exporting() ? 'Exporting…' : 'Export JSONL' }}
    </button>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryExportButtonComponent {
  private readonly memoryService = inject(MemoryService);
  readonly exporting = signal(false);

  export(): void {
    this.exporting.set(true);
    this.memoryService.exportMemories().subscribe({
      next: blob => {
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `memories-${Date.now()}.jsonl`;
        anchor.click();
        URL.revokeObjectURL(url);
        this.exporting.set(false);
      },
      error: () => this.exporting.set(false),
    });
  }
}

import { ChangeDetectionStrategy, Component, computed, inject, input, OnChanges, output, signal, SimpleChanges } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { RITUAL_BUNDLE_MAX_FILES, RITUAL_FILE_MAX_BYTES, Ritual, RitualFile } from '../../../core/models/ritual.model';
import { RitualService } from '../../../core/services/ritual.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';

/**
 * A skill's bundle files (ADR 0x035 §3): reference documents, scripts and assets under
 * `skills/<slug>/`. Lists them, adds one at a chosen path, removes one, and imports a whole bundle
 * into the skill. Only for an existing skill; a new skill gets files after it is created.
 */
@Component({
  selector: 'app-ritual-files-panel',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="files-panel" data-section="files">
      <div class="files-panel__head">
        <span class="files-panel__label">Files ({{ files().length }}/{{ maxFiles }})</span>
        <span class="files-panel__actions">
          <button type="button" class="files-panel__button" [disabled]="busy()" (click)="bundleInput.click()">Import bundle…</button>
          <input
            #bundleInput
            class="sr-only"
            type="file"
            accept=".zip,.md,application/zip,text/markdown"
            aria-label="Replace this skill from a SKILL.md bundle"
            (change)="onBundleChosen($event)" />
        </span>
      </div>
      <p class="files-panel__hint">
        Reference documents, scripts and assets the agent can read with its file tools at
        <code>skills/{{ slug() }}/…</code>. Instructions should point at them by path.
      </p>

      @if (loading()) {
        <p class="files-panel__hint">Loading files…</p>
      } @else if (files().length === 0) {
        <p class="files-panel__hint">No files yet.</p>
      } @else {
        <ul class="files-panel__list">
          @for (file of files(); track file.id) {
            <li class="files-panel__row">
              <code class="files-panel__path">{{ file.path }}</code>
              <span class="files-panel__type">{{ file.file_type }}</span>
              <button
                type="button"
                class="files-panel__button files-panel__button--danger"
                [attr.aria-label]="'Remove ' + file.path"
                [disabled]="busy()"
                (click)="remove(file)">
                Remove
              </button>
            </li>
          }
        </ul>
      }

      <div class="files-panel__add">
        <input
          class="files-panel__input"
          placeholder="references/notes.md"
          aria-label="Path inside the bundle"
          [ngModel]="path()"
          (ngModelChange)="path.set($event)" />
        <button type="button" class="files-panel__button" [disabled]="busy() || files().length >= maxFiles" (click)="fileInput.click()">
          Add file…
        </button>
        <input #fileInput class="sr-only" type="file" aria-label="File to add to the bundle" (change)="onFileChosen($event)" />
      </div>
      <p class="files-panel__hint">
        Use <code>references/</code> for documents the agent reads, <code>scripts/</code> for code it runs, <code>assets/</code> for templates and images.
        Up to 10 MB a file. Leave the path empty to file under <code>references/</code>.
      </p>

      @if (error(); as message) {
        <p class="files-panel__error" role="alert">{{ message }}</p>
      }
      @if (notice(); as message) {
        <p class="files-panel__hint" role="status">{{ message }}</p>
      }
    </div>
  `,
  styles: [
    `
      .files-panel {
        border: 1px solid var(--color-border-base);
        border-radius: 8px;
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 10px 12px;
      }

      .files-panel__head {
        align-items: center;
        display: flex;
        justify-content: space-between;
      }

      .files-panel__label {
        color: var(--color-text-secondary);
        font-size: 12px;
        font-weight: 600;
      }

      .files-panel__hint {
        color: var(--color-text-muted);
        font-size: 11px;
        line-height: 1.4;
        margin: 0;
      }

      .files-panel__error {
        color: var(--color-danger);
        font-size: 12px;
        margin: 0;
      }

      .files-panel__list {
        display: flex;
        flex-direction: column;
        gap: 4px;
        list-style: none;
        margin: 0;
        padding: 0;
      }

      .files-panel__row {
        align-items: center;
        display: grid;
        gap: 8px;
        grid-template-columns: minmax(0, 1fr) auto auto;
      }

      .files-panel__path {
        font-size: 12px;
        overflow-wrap: anywhere;
      }

      .files-panel__type {
        color: var(--color-text-muted);
        font-size: 11px;
      }

      .files-panel__add {
        display: flex;
        gap: 8px;
      }

      .files-panel__input {
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 6px;
        color: var(--color-text-primary);
        flex: 1;
        font-size: 12px;
        padding: 6px 8px;
      }

      .files-panel__button {
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 6px;
        color: var(--color-text-secondary);
        cursor: pointer;
        font-size: 12px;
        padding: 5px 10px;
      }

      .files-panel__button:disabled {
        cursor: not-allowed;
        opacity: 0.55;
      }

      .files-panel__button--danger {
        color: var(--color-danger);
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualFilesPanelComponent implements OnChanges {
  /** The skill whose bundle this is. */
  readonly ritual = input.required<Ritual>();
  /** Emitted after a bundle import replaced the skill's text, so the editor can reload it. */
  readonly imported = output<Ritual>();

  private readonly ritualService = inject(RitualService);

  readonly maxFiles = RITUAL_BUNDLE_MAX_FILES;
  readonly files = signal<RitualFile[]>([]);
  readonly loading = signal(false);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  readonly notice = signal<string | null>(null);
  readonly path = signal('');
  readonly slug = computed(() => this.ritual().slug || 'skill');

  ngOnChanges(changes: SimpleChanges): void {
    const change = changes['ritual'];
    if (change && change.previousValue?.id !== change.currentValue?.id) {
      void this.load();
    }
  }

  async load(): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      this.files.set(await firstValueFrom(this.ritualService.listSkillFiles(this.ritual().id)));
    } catch (err) {
      this.error.set(apiErrorMessage(err, 'Could not load the skill’s files.'));
    } finally {
      this.loading.set(false);
    }
  }

  async onFileChosen(event: Event): Promise<void> {
    const input = event.target;
    if (!(input instanceof HTMLInputElement)) return;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    if (file.size > RITUAL_FILE_MAX_BYTES) {
      this.error.set(`${file.name} is larger than 10 MB.`);
      return;
    }
    const path = this.path().trim() || `references/${file.name}`;
    await this.run(async () => {
      const stored = await firstValueFrom(this.ritualService.uploadSkillFile(this.ritual().id, file, path));
      this.files.set([...this.files().filter(f => f.id !== stored.id), stored].sort((a, b) => a.path.localeCompare(b.path)));
      this.path.set('');
      this.notice.set(`Added ${stored.path}.`);
    }, 'Could not add the file.');
  }

  async remove(file: RitualFile): Promise<void> {
    await this.run(async () => {
      await firstValueFrom(this.ritualService.deleteSkillFile(this.ritual().id, file.id));
      this.files.set(this.files().filter(f => f.id !== file.id));
      this.notice.set(`Removed ${file.path}.`);
    }, 'Could not remove the file.');
  }

  async onBundleChosen(event: Event): Promise<void> {
    const input = event.target;
    if (!(input instanceof HTMLInputElement)) return;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    await this.run(async () => {
      const result = await firstValueFrom(this.ritualService.importBundle(file, null, this.ritual().id));
      this.notice.set([`Replaced from bundle: ${result.files_imported} file(s).`, ...result.warnings].join(' '));
      this.imported.emit(result.ritual);
      await this.load();
    }, 'Could not import that bundle.');
  }

  private async run(action: () => Promise<void>, fallback: string): Promise<void> {
    this.busy.set(true);
    this.error.set(null);
    this.notice.set(null);
    try {
      await action();
    } catch (err) {
      this.error.set(apiErrorMessage(err, fallback));
    } finally {
      this.busy.set(false);
    }
  }
}

import { NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, Type, computed, effect, inject, input, signal, untracked } from '@angular/core';

import { FileAttachment } from '../app/core/models/file-attachment.model';
import { previewKindFor } from '../app/features/gallery/helpers/file-preview.helpers';
import { loadViewer } from '../app/features/file-workspace/viewers/registry';
import { DownloadIconComponent } from '../app/shared/ui';
import { SharedView, sharedContentUrl } from './share-api.service';
import { ShareFolderFiles, sharedAttachment } from './share-folder-files';

/** A file shared on its own (ADR 0x051 §6): its viewer, read-only, and a download. */
@Component({
  selector: 'share-file-view',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgComponentOutlet, DownloadIconComponent],
  styleUrl: './shared-view.scss',
  template: `
    <main class="shared-view">
      <header class="shared-view__bar">
        <h1 class="shared-view__name">{{ view().name }}</h1>
        <span class="shared-view__by">Shared with What Iff</span>
        <a class="shared-view__download" [href]="downloadUrl()" download>
          <ui-download-icon [size]="14" />
          Download
        </a>
      </header>
      <section class="shared-view__body">
        @if (noPreview()) {
          <p class="shared-view__note">This kind of file has no preview. Download it to open it.</p>
        } @else if (failed()) {
          <p class="shared-view__note" role="alert">The file could not be loaded. Reload to try again.</p>
        } @else if (!blob()) {
          <p class="shared-view__note" role="status">Loading…</p>
        } @else if (viewer(); as viewer) {
          <ng-container *ngComponentOutlet="viewer; inputs: { file: file(), blob: blob()! }" />
        }
      </section>
    </main>
  `,
})
export class SharedFileViewComponent {
  private readonly files = inject(ShareFolderFiles);

  readonly shareKey = input.required<string>();
  readonly view = input.required<SharedView>();

  readonly file = computed<FileAttachment>(() => {
    const v = this.view();
    return sharedAttachment({ path: v.name, file_type: v.file_type, size_bytes: v.size_bytes, content_version: v.content_version });
  });
  readonly downloadUrl = computed(() => sharedContentUrl(this.shareKey()));
  readonly blob = signal<Blob | null>(null);
  readonly viewer = signal<Type<unknown> | null>(null);
  readonly failed = signal(false);
  readonly noPreview = signal(false);
  private alive = true;

  constructor() {
    inject(DestroyRef).onDestroy(() => (this.alive = false));
    effect(() => {
      const key = this.shareKey();
      const v = this.view();
      untracked(() => void this.open(key, v));
    });
  }

  private async open(key: string, v: SharedView): Promise<void> {
    this.files.use(key, { path: v.name, file_type: v.file_type, size_bytes: v.size_bytes, content_version: v.content_version });
    // A kind with no viewer (or a diagram, which has none here) is only downloaded, by the link:
    // the browser streams it, and nothing is read before the visitor asks for it.
    const kind = previewKindFor(this.file());
    const load = kind === 'diagram' ? null : loadViewer(kind);
    if (!load) {
      this.noPreview.set(true);
      return;
    }
    try {
      const [blob, viewer] = await Promise.all([this.files.download(this.file()), load]);
      if (!this.alive) return;
      this.blob.set(blob);
      this.viewer.set(viewer);
    } catch {
      if (this.alive) this.failed.set(true);
    }
  }
}

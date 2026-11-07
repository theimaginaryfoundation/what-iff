import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';

import { FileAttachment } from '../../../core/models/file-attachment.model';

/** An image, from its own bytes (ADR 0x043 §2). The image editor is a later step. */
@Component({
  selector: 'app-image-viewer',
  standalone: true,
  template: `
    @if (safeUrl(); as url) {
      <img class="image-viewer__image" [src]="url" [alt]="file().name" />
    }
  `,
  styles: [
    `
      :host {
        align-items: center;
        display: flex;
        height: 100%;
        justify-content: center;
        min-height: 0;
        overflow: auto;
        padding: 1rem;
      }
      .image-viewer__image {
        max-height: 100%;
        max-width: 100%;
        object-fit: contain;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageViewerComponent {
  private readonly sanitizer = inject(DomSanitizer);

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  /** An object URL for the bytes, revoked when they change. */
  readonly objectUrl = signal<string | null>(null);
  /** Only ever an object URL made here from the file's own bytes, never a URL from data. */
  readonly safeUrl = computed<SafeResourceUrl | null>(() => {
    const url = this.objectUrl();
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  });

  constructor() {
    effect(() => {
      const blob = this.blob();
      untracked(() => {
        this.revoke();
        this.objectUrl.set(URL.createObjectURL(blob));
      });
    });
    inject(DestroyRef).onDestroy(() => this.revoke());
  }

  private revoke(): void {
    const url = this.objectUrl();
    if (url) {
      URL.revokeObjectURL(url);
      this.objectUrl.set(null);
    }
  }
}

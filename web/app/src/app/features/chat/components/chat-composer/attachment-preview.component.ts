import { AsyncPipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  OnInit,
  inject,
  input,
  signal,
} from '@angular/core';

import { PendingFileAttachment } from '../../../../core/models/file-attachment.model';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';

/** Fixed popover box (px) so placement can be computed before the image has loaded. */
const PREVIEW_WIDTH = 208;
const PREVIEW_HEIGHT = 156;
const GAP = 8;
const VIEWPORT_GUTTER = 8;

/**
 * Small floating preview of a pending image attachment, anchored to its composer chip. The parent
 * owns when it is shown (hover, keyboard focus, or tap) and renders it only while open.
 *
 * Fixed-positioned and clamped to the viewport so it can't overflow on narrow screens. It prefers
 * the space above the chip so it doesn't cover the composer's text box and send controls.
 */
@Component({
  selector: 'app-attachment-preview',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe],
  template: `
    @if (localSrc() ?? (serverSrc() | authImage | async); as src) {
      <img [src]="src" [alt]="'Preview of ' + name()" (error)="onImageError()" />
    } @else {
      <span class="attachment-preview__empty">Preview unavailable</span>
    }
  `,
  styles: [`
    :host {
      align-items: center;
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.75rem;
      box-shadow: 0 10px 30px rgb(0 0 0 / 0.22);
      box-sizing: border-box;
      display: flex;
      justify-content: center;
      overflow: hidden;
      padding: 0.25rem;
      pointer-events: none;
      position: fixed;
      z-index: 1200;
    }

    img {
      display: block;
      height: 100%;
      object-fit: contain;
      width: 100%;
    }

    .attachment-preview__empty {
      color: var(--color-text-muted);
      font-size: 0.75rem;
    }
  `],
  host: {
    role: 'tooltip',
    '[attr.id]': 'previewId()',
    '[style.left.px]': 'left()',
    '[style.top.px]': 'top()',
    '[style.width.px]': 'width()',
    '[style.height.px]': 'height',
  },
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AttachmentPreviewComponent implements OnInit {
  readonly item = input.required<PendingFileAttachment>();
  /** The chip the preview points at; used only for placement. */
  readonly anchor = input.required<HTMLElement>();
  readonly previewId = input.required<string>();
  /** Display name, used for the image's accessible name. */
  readonly name = input.required<string>();

  private readonly imageGallery = inject(ImageGalleryService);

  readonly height = PREVIEW_HEIGHT;
  readonly width = signal(PREVIEW_WIDTH);
  readonly left = signal(VIEWPORT_GUTTER);
  readonly top = signal(VIEWPORT_GUTTER);

  /**
   * A local object URL for a chip whose file we still hold: instant, no network, and shows exactly
   * what was picked. Dropped when the browser can't render the type (e.g. HEIC) so the server's
   * thumbnail is used instead.
   */
  readonly localSrc = signal<string | null>(null);
  private objectUrl: string | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.revoke());
  }

  ngOnInit(): void {
    const file = this.item().file;
    if (file && typeof URL.createObjectURL === 'function') {
      this.objectUrl = URL.createObjectURL(file);
      this.localSrc.set(this.objectUrl);
    }
    this.place();
  }

  /** Server thumbnail for an uploaded/referenced attachment; null while only a local file exists. */
  serverSrc(): string | null {
    const id = this.item().attachment?.id;
    return id ? this.imageGallery.getImageUrl(id, 'thumbnail') : null;
  }

  onImageError(): void {
    this.revoke();
    this.localSrc.set(null);
  }

  private revoke(): void {
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl);
      this.objectUrl = null;
    }
  }

  private place(): void {
    const view = this.anchor().ownerDocument.defaultView;
    const viewportW = view?.innerWidth || PREVIEW_WIDTH + 2 * VIEWPORT_GUTTER;
    const viewportH = view?.innerHeight || PREVIEW_HEIGHT + 2 * VIEWPORT_GUTTER;
    const rect = this.anchor().getBoundingClientRect();

    const width = Math.min(PREVIEW_WIDTH, viewportW - 2 * VIEWPORT_GUTTER);
    const centered = rect.left + rect.width / 2 - width / 2;
    const left = Math.max(VIEWPORT_GUTTER, Math.min(centered, viewportW - width - VIEWPORT_GUTTER));

    const above = rect.top - GAP - PREVIEW_HEIGHT;
    const top = above >= VIEWPORT_GUTTER ? above : rect.bottom + GAP;

    this.width.set(width);
    this.left.set(left);
    this.top.set(Math.max(VIEWPORT_GUTTER, Math.min(top, viewportH - PREVIEW_HEIGHT - VIEWPORT_GUTTER)));
  }
}

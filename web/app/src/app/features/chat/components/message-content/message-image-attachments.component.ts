import { CommonModule, DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';

import { FileAttachment } from '../../../../core/models/file-attachment.model';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { ImageIconComponent } from '../../../../shared/ui/icons/icons';

@Component({
  selector: 'app-message-image-attachments',
  standalone: true,
  imports: [CommonModule, AuthImagePipe, ImageIconComponent],
  template: `
    <div class="message-images" aria-label="Message images">
      @for (attachment of attachments(); track attachment.id) {
        <figure class="message-images__card">
          <div class="message-images__preview">
            @if (previewUrl(attachment) | authImage | async; as src) {
              <img [src]="src" [alt]="attachment.name" loading="lazy" />
            }
          </div>
          <figcaption class="message-images__footer">
            <span class="message-images__name">
              <ui-image-icon [size]="14" />
              <span>{{ attachment.name }}</span>
            </span>
            <button type="button" class="message-images__download" (click)="download(attachment)">
              Download
            </button>
          </figcaption>
        </figure>
      }
    </div>
  `,
  styles: [`
    .message-images {
      display: grid;
      gap: 0.75rem;
      margin-top: 0.75rem;
    }

    .message-images__card {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.875rem;
      margin: 0;
      overflow: hidden;
      max-width: min(28rem, 100%);
    }

    .message-images__preview {
      align-items: center;
      background: var(--color-surface-muted);
      display: flex;
      justify-content: center;
      max-height: 18rem;
      min-height: 8rem;
      overflow: hidden;
    }

    .message-images__preview img {
      display: block;
      max-height: 18rem;
      max-width: 100%;
      object-fit: contain;
      width: 100%;
    }

    .message-images__footer,
    .message-images__name {
      align-items: center;
      display: flex;
      min-width: 0;
    }

    .message-images__footer {
      gap: 0.625rem;
      justify-content: space-between;
      padding: 0.5rem 0.625rem;
    }

    .message-images__name {
      color: var(--color-text-secondary);
      flex: 1;
      font-size: 0.75rem;
      gap: 0.375rem;
    }

    .message-images__name span:last-child {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .message-images__download {
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-secondary);
      flex-shrink: 0;
      font-size: 0.6875rem;
      font-weight: 700;
      padding: 0.1875rem 0.5rem;
    }

    .message-images__download:hover,
    .message-images__download:focus-visible {
      color: var(--color-accent);
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MessageImageAttachmentsComponent {
  readonly attachments = input<readonly FileAttachment[]>([]);

  private readonly document = inject(DOCUMENT);
  private readonly http = inject(HttpClient);
  private readonly imageGallery = inject(ImageGalleryService);

  previewUrl(attachment: FileAttachment): string {
    return this.imageGallery.getImageUrl(attachment.id, 'thumbnail');
  }

  download(attachment: FileAttachment): void {
    this.http.get(this.imageGallery.getImageUrl(attachment.id, 'full'), { responseType: 'blob' }).subscribe(blob => {
      const url = URL.createObjectURL(blob);
      const anchor = this.document.createElement('a');
      anchor.href = url;
      anchor.download = attachment.name || `image-${attachment.id}`;
      anchor.click();
      URL.revokeObjectURL(url);
    });
  }
}

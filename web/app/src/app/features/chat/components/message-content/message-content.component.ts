import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment } from '../../../../core/models/file-attachment.model';
import { buildClipboardPayload } from '../../helpers/html-clipboard.helpers';
import { extractImages } from '../../helpers/message-content.helpers';
import { MessageImageAttachmentsComponent } from './message-image-attachments.component';

@Component({
  selector: 'app-message-content',
  standalone: true,
  imports: [MarkdownModule, MessageImageAttachmentsComponent],
  template: `
    <markdown
      class="message-content__markdown prose prose-sm max-w-none dark:prose-invert"
      [class.message-content__markdown--preserve-breaks]="preserveLineBreaks()"
      [data]="content()"
      (copy)="onCopy($event)"
    ></markdown>

    @if (images().length) {
      <div class="mt-3 grid gap-2 sm:grid-cols-2">
        @for (image of images(); track image.src) {
          <img class="max-h-72 rounded-xl border border-border-base object-cover" [src]="image.src" [alt]="image.alt || 'Message image'" />
        }
      </div>
    }

    @if (imageAttachments().length) {
      <app-message-image-attachments [attachments]="imageAttachments()" />
    }

    @if (nonImageAttachments().length) {
      <div class="mt-3 flex flex-wrap gap-2">
        @for (attachment of nonImageAttachments(); track attachment.id) {
          <span
            class="rounded-full border border-border-base bg-surface-muted px-3 py-1 text-xs text-text-secondary hover:text-text-primary"
          >
            {{ attachment.name }}
          </span>
        }
      </div>
    }
  `,
  styles: [`
    :host {
      display: block;
      min-width: 0;
      max-width: 100%;
    }

    :host ::ng-deep .message-content__markdown {
      max-width: 100%;
      min-width: 0;
      overflow-wrap: anywhere;
      word-break: break-word;
    }

    :host ::ng-deep .message-content__markdown p,
    :host ::ng-deep .message-content__markdown li,
    :host ::ng-deep .message-content__markdown blockquote {
      overflow-wrap: anywhere;
      word-break: break-word;
    }

    :host ::ng-deep .message-content__markdown--preserve-breaks p {
      white-space: pre-wrap;
    }

    :host ::ng-deep pre {
      overflow-x: hidden;
      max-width: 100%;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      word-break: break-word;
    }

    :host ::ng-deep code {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MessageContentComponent {
  private readonly document = inject(DOCUMENT);

  readonly content = input('');
  readonly attachments = input<readonly FileAttachment[]>([]);
  readonly preserveLineBreaks = input(false);
  readonly images = computed(() => extractImages(this.content()));
  readonly imageAttachments = computed(() => this.attachments().filter(isImageAttachment));
  readonly nonImageAttachments = computed(() => this.attachments().filter(attachment => !isImageAttachment(attachment)));

  onCopy(event: ClipboardEvent): void {
    const selection = this.document.defaultView?.getSelection();
    if (!selection || selection.isCollapsed) {
      return;
    }
    const clipboard = event.clipboardData;
    if (!clipboard) {
      return;
    }
    const payload = buildClipboardPayload(this.document, selection);
    if (!payload.plainText.trim()) {
      return;
    }
    event.preventDefault();
    clipboard.setData('text/plain', payload.plainText);
    if (payload.html) {
      clipboard.setData('text/html', payload.html);
    }
  }
}

function isImageAttachment(attachment: FileAttachment): boolean {
  return attachment.file_type.toLowerCase().startsWith('image/');
}

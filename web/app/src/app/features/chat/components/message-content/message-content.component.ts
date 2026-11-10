import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, computed, inject, input } from '@angular/core';
import { MarkdownModule } from 'ngx-markdown';

import { FileAttachment, isImageAttachment } from '../../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../../core/services/file-attachment.service';
import { saveBlobAsFile } from '../../../../core/utils/download.helpers';
import { renderLatexMath } from '../../../../core/utils/latex-math.helpers';
import { FileWorkspaceService } from '../../../file-workspace/file-workspace.service';
import { DownloadIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';
import { buildClipboardPayload } from '../../helpers/html-clipboard.helpers';
import { extractImages } from '../../helpers/message-content.helpers';
import { MessageImageAttachmentsComponent } from './message-image-attachments.component';

@Component({
  selector: 'app-message-content',
  standalone: true,
  imports: [MarkdownModule, MessageImageAttachmentsComponent, DownloadIconComponent, TooltipDirective],
  template: `
    <markdown
      class="message-content__markdown prose prose-sm max-w-none dark:prose-invert"
      [class.message-content__markdown--preserve-breaks]="preserveLineBreaks()"
      [data]="content()"
      (copy)="onCopy($event)"
      (ready)="onMarkdownReady()"
    ></markdown>

    @if (images().length) {
      <div class="mt-3 grid gap-2 sm:grid-cols-2">
        @for (image of images(); track image.src) {
          <img class="max-h-72 rounded-xl border border-border-base object-cover" [src]="image.src" [alt]="image.alt || 'Message image'" />
        }
      </div>
    }

    @if (imageAttachments().length) {
      <app-message-image-attachments [attachments]="imageAttachments()" [size]="imageSize()" />
    }

    @if (nonImageAttachments().length) {
      <div class="mt-3 flex flex-wrap gap-2">
        @for (attachment of nonImageAttachments(); track attachment.id) {
          <span class="message-attachment" data-testid="message-attachment">
            <button
              type="button"
              class="message-attachment__open"
              [attr.aria-label]="'Open ' + attachment.name"
              (click)="openAttachment(attachment)"
            >
              {{ attachment.name }}
            </button>
            <button
              type="button"
              class="message-attachment__download"
              [attr.aria-label]="'Download ' + attachment.name"
              uiTooltip="Download"
              (click)="downloadAttachment(attachment)"
            >
              <ui-download-icon [size]="12" />
            </button>
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

    .message-attachment {
      align-items: stretch;
      border: 1px solid var(--color-border-base);
      border-radius: 9999px;
      display: inline-flex;
      overflow: hidden;
    }

    .message-attachment__open,
    .message-attachment__download {
      background: var(--color-surface-muted);
      border: 0;
      color: var(--color-text-secondary);
      cursor: pointer;
      font-size: 0.75rem;
    }

    .message-attachment__open {
      padding: 0.25rem 0.5rem 0.25rem 0.75rem;
    }

    .message-attachment__download {
      align-items: center;
      border-left: 1px solid var(--color-border-base);
      display: inline-flex;
      padding: 0 0.5rem;
    }

    .message-attachment__open:hover,
    .message-attachment__download:hover {
      color: var(--color-text-primary);
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

    /* KaTeX positions glyphs itself; the wrapping rules above would split formulas mid-symbol. */
    :host ::ng-deep .katex {
      overflow-wrap: normal;
      word-break: normal;
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
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly files = inject(FileAttachmentService);
  private readonly workspace = inject(FileWorkspaceService);

  readonly content = input('');
  readonly attachments = input<readonly FileAttachment[]>([]);
  readonly preserveLineBreaks = input(false);
  /** Variant used for image attachments; user turns pass 'full' so the picture isn't an upscaled thumbnail. */
  readonly imageSize = input<'thumbnail' | 'full'>('thumbnail');
  readonly images = computed(() => extractImages(this.content()));
  readonly imageAttachments = computed(() => this.attachments().filter(isImageAttachment));
  readonly nonImageAttachments = computed(() => this.attachments().filter(attachment => !isImageAttachment(attachment)));

  onMarkdownReady(): void {
    void renderLatexMath(this.host.nativeElement);
  }

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

  /** Opens the file in the workspace beside the thread (ADR 0x043 §5), without leaving the message. */
  openAttachment(attachment: FileAttachment): void {
    this.workspace.requestOpen(attachment.id);
  }

  downloadAttachment(attachment: FileAttachment): void {
    this.files.downloadFileAttachment(attachment.id).subscribe(blob => saveBlobAsFile(blob, attachment.name || `attachment-${attachment.id}`));
  }
}

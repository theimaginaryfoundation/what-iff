import { inject } from '@angular/core';
import { Observable, firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { MarkupResult } from './image-markup.component';
import { markupStem } from './markup-strokes';

/**
 * The gallery folder masks and markup copies are filed in. They matter for a turn or two, so they
 * collect out of the way instead of cluttering the gallery.
 */
export const MARKUP_FOLDER = 'image_masks';
/** Where sketches go: the person's own drawings, worth finding again. */
export const SKETCH_FOLDER = 'sketches';

/** What uploads a file into the thread; FileAttachmentService.uploadChatFileAttachment. */
export type ChatUpload = (chatId: string, file: File, folder?: string, description?: string) => Observable<FileAttachment>;

/**
 * What makes an attachable copy of an existing file; ImageGalleryService.referenceImage. A file
 * belongs to one message, so the image a markup was drawn on (often already on a message) is
 * attached as a reference copy that shares its stored object, as the gallery picker does.
 */
export type ReferenceFile = (id: string) => Observable<FileAttachment>;

/**
 * Turns markup into the files for the next message (ADR 0x045 §6–7): new uploads for the mask, the
 * markup copy or the sketch, each described so the agent knows what it is, after the image they
 * were drawn on (attached as a reference copy, never changed). Returns them in the order to attach.
 */
export async function uploadMarkup(
  upload: ChatUpload,
  reference: ReferenceFile,
  chatId: string,
  source: FileAttachment | null,
  result: MarkupResult,
  now = new Date(),
): Promise<FileAttachment[]> {
  const attachments: FileAttachment[] = [];
  if (!source) {
    if (!result.markup) return attachments;
    const name = `sketch-${timestamp(now)}.png`;
    attachments.push(
      await send(
        upload,
        chatId,
        result.markup,
        name,
        'A rough sketch by the user to base an image on. Follow its composition; it is not the finished style.',
        SKETCH_FOLDER,
      ),
    );
    return attachments;
  }
  attachments.push(await firstValueFrom(reference(source.id)));
  const stem = markupStem(source.name);
  if (result.mask) {
    attachments.push(
      await send(
        upload,
        chatId,
        result.mask,
        `${stem}-mask.png`,
        `Mask for ${source.name}: white marks the area the user wants changed, black the area to keep.`,
      ),
    );
  }
  if (result.markup) {
    attachments.push(
      await send(
        upload,
        chatId,
        result.markup,
        `${stem}-markup.png`,
        `${source.name} with the user's markup drawn on it: the marks show where and what to change, and are instructions, not content.`,
      ),
    );
  }
  return attachments;
}

/** uploadMarkup wired to the app's services. Call it in an injection context (a field initializer). */
export function injectMarkupUploader(): (chatId: string, source: FileAttachment | null, result: MarkupResult) => Promise<FileAttachment[]> {
  const files = inject(FileAttachmentService);
  const gallery = inject(ImageGalleryService);
  return (chatId, source, result) =>
    uploadMarkup(
      (...args) => files.uploadChatFileAttachment(...args),
      id => gallery.referenceImage(id),
      chatId,
      source,
      result,
    );
}

function send(
  upload: ChatUpload,
  chatId: string,
  blob: Blob,
  name: string,
  description: string,
  folder = MARKUP_FOLDER,
): Promise<FileAttachment> {
  return firstValueFrom(upload(chatId, new File([blob], name, { type: 'image/png' }), folder, description));
}

/** A sortable local time for a sketch's name: 2026-11-11-1432. */
function timestamp(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}`;
}

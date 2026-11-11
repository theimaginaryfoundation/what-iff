import { Observable, firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { MarkupResult } from './image-markup.component';
import { markupStem } from './markup-strokes';

/** The gallery folder markup files are filed in, so they stay together and out of the way. */
export const MARKUP_FOLDER = 'markup';

/** What uploads a file into the thread; FileAttachmentService.uploadChatFileAttachment. */
export type ChatUpload = (chatId: string, file: File, folder?: string, description?: string) => Observable<FileAttachment>;

/**
 * Turns markup into the files for the next message (ADR 0x045 §6–7): new uploads for the mask, the
 * markup copy or the sketch, each described so the agent knows what it is, after the image they
 * were drawn on (which is attached as it is, never changed). Returns them in the order to attach.
 */
export async function uploadMarkup(
  upload: ChatUpload,
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
      ),
    );
    return attachments;
  }
  attachments.push(source);
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

function send(upload: ChatUpload, chatId: string, blob: Blob, name: string, description: string): Promise<FileAttachment> {
  return firstValueFrom(upload(chatId, new File([blob], name, { type: 'image/png' }), MARKUP_FOLDER, description));
}

/** A sortable local time for a sketch's name: 2026-11-11-1432. */
function timestamp(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}`;
}

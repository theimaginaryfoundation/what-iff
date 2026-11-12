import { inject } from '@angular/core';
import { Observable, firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ContextPanelService } from '../../chat/services/context-panel.service';
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
 * What deletes a file made here when a later step fails; FileAttachmentService.deleteFileAttachment.
 * A reference copy's stored object is the original's, which deleting the copy leaves in place.
 */
export type RemoveFile = (id: string) => Observable<unknown>;

/** The file operations uploadMarkup uses. */
export interface MarkupFileOps {
  upload: ChatUpload;
  reference: ReferenceFile;
  remove: RemoveFile;
}

export interface UploadMarkupOptions {
  /** Attach a reference copy of the image drawn on. False when it is already on the message. */
  attachSource?: boolean;
  now?: Date;
}

/**
 * Turns markup into the files for the next message (ADR 0x047 §6–7): new uploads for the mask, the
 * markup copy or the sketch, each described so the agent knows what it is, after the image they
 * were drawn on (attached as a reference copy, never changed). Returns them in the order to attach.
 *
 * All or nothing: when a step fails, the files already made are deleted before the error is
 * rethrown, so trying again does not leave duplicates in the thread.
 */
export async function uploadMarkup(
  ops: MarkupFileOps,
  chatId: string,
  source: FileAttachment | null,
  result: MarkupResult,
  { attachSource = true, now = new Date() }: UploadMarkupOptions = {},
): Promise<FileAttachment[]> {
  const attachments: FileAttachment[] = [];
  try {
    if (!source) {
      if (result.markup) {
        attachments.push(
          await send(
            ops.upload,
            chatId,
            result.markup,
            `sketch-${timestamp(now)}.png`,
            'A rough sketch by the user to base an image on. Follow its composition; it is not the finished style.',
            SKETCH_FOLDER,
          ),
        );
      }
      return attachments;
    }
    if (attachSource) attachments.push(await firstValueFrom(ops.reference(source.id)));
    const stem = markupStem(source.name);
    if (result.mask) {
      attachments.push(
        await send(
          ops.upload,
          chatId,
          result.mask,
          `${stem}-mask.png`,
          `Mask for ${source.name}: white marks the area the user wants changed, black the area to keep. Pass ${source.name} as the base image and this file as mask_image.`,
        ),
      );
    }
    if (result.markup) {
      attachments.push(
        await send(
          ops.upload,
          chatId,
          result.markup,
          `${stem}-markup.png`,
          `${source.name} with the user's markup drawn on it: the marks show where and what to change, and are instructions, not content.`,
        ),
      );
    }
    return attachments;
  } catch (error) {
    await Promise.allSettled(attachments.map(made => firstValueFrom(ops.remove(made.id))));
    throw error;
  }
}

/**
 * Whether pending (the files on the message being written) already holds source: the file itself,
 * or a reference copy of it from an earlier markup, which shares its stored object.
 */
export function holdsFile(pending: readonly FileAttachment[], source: FileAttachment): boolean {
  return pending.some(file => file.id === source.id || (!!source.s3_key && file.s3_key === source.s3_key));
}

/**
 * Markup wired to the app: uploads it (uploadMarkup) and adds the files to the message being
 * written, with the image drawn on unless the message already has it. Used by the file workspace
 * and the sketch canvas alike. Call it in an injection context (a field initializer).
 */
export function injectMarkupSender(): (chatId: string, source: FileAttachment | null, result: MarkupResult) => Promise<void> {
  const files = inject(FileAttachmentService);
  const gallery = inject(ImageGalleryService);
  const contextPanel = inject(ContextPanelService);
  const ops: MarkupFileOps = {
    upload: (...args) => files.uploadChatFileAttachment(...args),
    reference: id => gallery.referenceImage(id),
    remove: id => files.deleteFileAttachment(id),
  };
  return async (chatId, source, result) => {
    const onMessage = [...contextPanel.composerPending(), ...contextPanel.composerAttachments()];
    const attachSource = !!source && !holdsFile(onMessage, source);
    const attachments = await uploadMarkup(ops, chatId, source, result, { attachSource });
    for (const attachment of attachments) contextPanel.requestComposerAttachment(attachment);
    contextPanel.refreshFiles();
  };
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

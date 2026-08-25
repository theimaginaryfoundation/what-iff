export interface FileAttachment {
  id: string;
  user_id: string;
  file_id?: string;
  name: string;
  file_type: string;
  description?: string | null;
  file_content?: string;
  /** S3/local storage object key. Present on gallery uploads and reference attachments.
   *  Sent back to the backend so the agent can resolve image bytes via the correct key. */
  s3_key?: string;
  chat_message_id?: string;
  personality_id?: string;
  personalities?: FileAttachmentPersonalityRef[];
  created_at: string;
}

export interface FileAttachmentPersonalityRef {
  id: string;
  name: string;
}

export interface PendingFileAttachment {
  /** Stable client id for UI tracking and removal before upload completes. */
  clientKey?: string;
  file?: File;
  attachment?: FileAttachment;
  isUploading: boolean;
  uploadError?: string;
}

/** Generates a client-side key safe for browser, SSR, and test environments. */
export function newPendingAttachmentClientKey(): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) {
    return uuid;
  }
  return `pending-${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;
}

/**
 * Returns a stable key for pending attachment chips (track + remove).
 * Prefer creating items via `createPendingFileAttachment` so `clientKey` is always set.
 */
export function pendingAttachmentKey(item: PendingFileAttachment): string {
  if (item.clientKey) {
    return item.clientKey;
  }
  if (item.attachment?.id) {
    return `attachment:${item.attachment.id}`;
  }
  if (item.file) {
    return `file:${item.file.name}:${item.file.size}:${item.file.lastModified}`;
  }
  console.error('pendingAttachmentKey: pending item has no clientKey, attachment, or file', item);
  throw new Error('Pending attachment is missing a stable identifier');
}

export function createPendingFileAttachment(
  partial: Omit<PendingFileAttachment, 'clientKey'> & { clientKey?: string },
): PendingFileAttachment {
  return {
    ...partial,
    clientKey: partial.clientKey ?? newPendingAttachmentClientKey(),
  };
}

export interface FileAttachmentFilters {
  name?: string;
  file_type?: string;
  chat_message_id?: string;
  personality_id?: string;
  /**
   * When true with personality_id, restricts results to RAG doc files with a
   * direct personality_id FK. Expression images are excluded so they don't
   * count against the upload slot limit.
   */
  docs_only?: boolean;
  min_date?: string;
  max_date?: string;
}


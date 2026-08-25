import { Injectable } from '@angular/core';

/** Per-upload cap — mirrors the backend's ~60 MB import limit. */
export const CHAT_IMPORT_CHUNK_BYTES = 60 * 1024 * 1024;

/** Sanity cap for parsing a full export in-browser before chunking. */
export const MAX_EXPORT_BYTES = 500 * 1024 * 1024;

type ExportShape = 'array' | 'openai-wrapped';

interface ParsedExport {
  shape: ExportShape;
  conversations: unknown[];
}

export interface SplitExportOptions {
  /** Override per-chunk byte cap (tests); production uses CHAT_IMPORT_CHUNK_BYTES. */
  maxChunkBytes?: number;
}

/** Splits a ChatGPT/Claude conversations.json export into <=60 MB chunks at conversation boundaries. */
@Injectable({ providedIn: 'root' })
export class ConversationImportChunkService {
  /**
   * Returns upload-ready blobs. Files within the chunk cap pass through as a single element.
   * Always reports {@link totalConversations} (including the under-cap pass-through path) so the
   * modal can show accurate totals before the backend's first progress tick.
   */
  async splitExport(
    blob: Blob,
    options?: SplitExportOptions,
  ): Promise<{ chunks: Blob[]; totalConversations: number }> {
    const maxChunkBytes = options?.maxChunkBytes ?? CHAT_IMPORT_CHUNK_BYTES;
    if (blob.size <= maxChunkBytes) {
      let totalConversations = 0;
      try {
        totalConversations = parseConversationsExport(await blob.text()).conversations.length;
      } catch {
        // Malformed JSON: still hand the blob to the backend so it can return a proper error.
      }
      return { chunks: [blob], totalConversations };
    }
    if (blob.size > MAX_EXPORT_BYTES) {
      throw new Error(
        `That export is ${formatMb(blob.size)}. Please split conversations.json manually — imports above ${formatMb(MAX_EXPORT_BYTES)} are not supported in the browser.`,
      );
    }

    const parsed = parseConversationsExport(await blob.text());
    if (parsed.conversations.length === 0) {
      throw new Error('That export contains no conversations.');
    }

    return {
      chunks: packConversations(parsed, maxChunkBytes),
      totalConversations: parsed.conversations.length,
    };
  }
}

export function parseConversationsExport(text: string): ParsedExport {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error('Could not read conversations.json — invalid JSON.');
  }

  if (Array.isArray(data)) {
    return { shape: 'array', conversations: data };
  }
  if (data && typeof data === 'object') {
    const conversations = (data as Record<string, unknown>)['conversations'];
    if (Array.isArray(conversations)) {
      return { shape: 'openai-wrapped', conversations };
    }
  }
  throw new Error('Unrecognized export format. Upload conversations.json from a ChatGPT or Claude export.');
}

function serializeChunk(shape: ExportShape, conversations: unknown[]): string {
  if (shape === 'array') {
    return JSON.stringify(conversations);
  }
  return JSON.stringify({ conversations });
}

function byteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

function packConversations(parsed: ParsedExport, maxChunkBytes: number): Blob[] {
  const chunks: Blob[] = [];
  let batch: unknown[] = [];

  for (const conv of parsed.conversations) {
    const single = serializeChunk(parsed.shape, [conv]);
    if (byteLength(single) > maxChunkBytes) {
      throw new Error(
        'One conversation in that export is larger than 60MB and cannot be imported. Try exporting fewer chats or trimming very large threads first.',
      );
    }

    const candidate = serializeChunk(parsed.shape, [...batch, conv]);
    if (byteLength(candidate) > maxChunkBytes && batch.length > 0) {
      chunks.push(new Blob([serializeChunk(parsed.shape, batch)], { type: 'application/json' }));
      batch = [conv];
    } else {
      batch.push(conv);
    }
  }

  if (batch.length > 0) {
    chunks.push(new Blob([serializeChunk(parsed.shape, batch)], { type: 'application/json' }));
  }
  return chunks;
}

function formatMb(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(0)}MB`;
}

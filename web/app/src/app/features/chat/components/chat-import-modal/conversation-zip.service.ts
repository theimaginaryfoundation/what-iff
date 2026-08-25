import { Injectable } from '@angular/core';

/**
 * Basename match for ChatGPT conversation export files: the canonical
 * `conversations.json` and size-split shards `conversations-000.json`, … .
 * Deliberately narrow so we don't pick up unrelated `conversations-*.json` notes.
 */
export const CONVERSATIONS_EXPORT_BASENAME_RE = /^conversations(?:-\d+)?\.json$/i;

/** @deprecated Prefer {@link listConversationExportPaths}; kept for older call sites/tests. */
export const SPLIT_CONVERSATIONS_RE = /(^|\/)conversations-\d+\.json$/i;

/** Lists export conversation JSON paths (canonical + numbered shards), sorted alphabetically. */
export function listConversationExportPaths(names: string[]): string[] {
  return names
    .filter(n => {
      const base = n.split('/').pop() ?? '';
      return CONVERSATIONS_EXPORT_BASENAME_RE.test(base);
    })
    .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base', numeric: true }));
}

/** @deprecated Use {@link listConversationExportPaths}. */
export function findSplitConversationParts(names: string[]): string[] {
  return listConversationExportPaths(names).filter(n => {
    const base = n.split('/').pop() ?? '';
    return /^conversations-\d+\.json$/i.test(base);
  });
}

/** Stable dedup key for a ChatGPT conversation object (prefers conversation_id, then id). */
export function conversationDedupKey(conv: unknown): string | null {
  if (!conv || typeof conv !== 'object') return null;
  const o = conv as Record<string, unknown>;
  for (const k of ['conversation_id', 'id'] as const) {
    const v = o[k];
    if (typeof v === 'string' && v.trim()) return v.trim();
  }
  return null;
}

/** Epoch seconds from create_time / update_time (OpenAI float or int); missing → NaN. */
export function conversationSortTime(conv: unknown): number {
  if (!conv || typeof conv !== 'object') return Number.NaN;
  const o = conv as Record<string, unknown>;
  for (const k of ['create_time', 'update_time'] as const) {
    const v = o[k];
    if (typeof v === 'number' && Number.isFinite(v)) return v;
    if (typeof v === 'string' && v.trim()) {
      const n = Number(v);
      if (Number.isFinite(n)) return n;
    }
  }
  return Number.NaN;
}

/**
 * Merges conversation arrays from one or more shard parses: dedupe by id, then sort by create_time.
 * Later shards win on duplicate ids (alphabetical path order when callers sort paths first).
 */
export function mergeConversationShards(shards: unknown[][]): unknown[] {
  const byId = new Map<string, unknown>();
  const noId: unknown[] = [];

  for (const shard of shards) {
    for (const conv of shard) {
      const key = conversationDedupKey(conv);
      if (key) {
        byId.set(key, conv);
      } else {
        noId.push(conv);
      }
    }
  }

  const merged = [...byId.values(), ...noId];
  merged.sort((a, b) => {
    const ta = conversationSortTime(a);
    const tb = conversationSortTime(b);
    const aMissing = Number.isNaN(ta);
    const bMissing = Number.isNaN(tb);
    if (aMissing && bMissing) return 0;
    if (aMissing) return 1;
    if (bMissing) return -1;
    return ta - tb;
  });
  return merged;
}

function parseShardRoot(text: string, label: string): unknown[] {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error(`Could not parse ${label} — invalid JSON.`);
  }
  if (Array.isArray(data)) {
    return data;
  }
  if (data && typeof data === 'object') {
    const conversations = (data as Record<string, unknown>)['conversations'];
    if (Array.isArray(conversations)) {
      return conversations;
    }
  }
  throw new Error(`${label} is not a conversations export (expected a JSON array).`);
}

/**
 * Extracts and merges ChatGPT/Claude conversation export JSON from a `.zip` client-side.
 * JSZip is imported lazily. Callers must size-guard (see MAX_ZIP_BYTES in the import modal).
 *
 * Large ChatGPT exports may ship numbered shards (`conversations-000.json`, …). Threads are not
 * split across files, so we concatenate shard arrays, dedupe by conversation id, and sort by
 * create_time into one `conversations.json` blob for the existing import pipeline.
 */
@Injectable({ providedIn: 'root' })
export class ConversationZipService {
  /** Loads JSZip, finds all conversations*.json export files, merges them, returns one Blob. */
  async extractConversationsJson(file: File): Promise<Blob> {
    const { default: JSZip } = await import('jszip');
    const zip = await JSZip.loadAsync(file);
    const names = Object.keys(zip.files).filter(n => !zip.files[n].dir);
    const exportPaths = listConversationExportPaths(names);

    if (exportPaths.length === 0) {
      throw new Error(
        'No conversations.json (or conversations-000.json shards) found in that .zip.',
      );
    }

    const shards: unknown[][] = [];
    const errors: string[] = [];

    for (const path of exportPaths) {
      const entry = zip.files[path];
      if (!entry) continue;
      const base = path.split('/').pop() ?? path;
      try {
        const text = await entry.async('string');
        shards.push(parseShardRoot(text, base));
      } catch (err) {
        const msg = err instanceof Error ? err.message : `Could not read ${base}`;
        errors.push(msg);
      }
    }

    if (shards.length === 0) {
      throw new Error(
        errors.length > 0
          ? `Could not read any conversation files in that .zip. ${errors[0]}`
          : 'No conversations.json found in that .zip.',
      );
    }

    const merged = mergeConversationShards(shards);
    if (merged.length === 0) {
      throw new Error('That export contains no conversations.');
    }

    return new Blob([JSON.stringify(merged)], { type: 'application/json' });
  }
}

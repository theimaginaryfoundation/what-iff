import { Injectable } from '@angular/core';
import type JSZip from 'jszip';

export interface ArchivePersonality {
  id: string;
  name: string;
}

export interface ArchiveConversation {
  id: string;
  title: string;
}

export interface ArchiveContents {
  personalities: ArchivePersonality[];
  conversations: ArchiveConversation[];
  /** Whether the export bundles memories (a nested memories.zip). Memories are all-or-nothing. */
  hasMemories: boolean;
}

/**
 * Reads a WhatIff account-export ZIP in the browser (JSZip) and enumerates its restorable items so
 * the restore screen can offer a selection ledger. The ids it returns are the SAME source ids the
 * server filters on: a personality's `whatiff_personality_id` (falling back to the archive directory
 * name) and a conversation's `uuid`.
 */
@Injectable({ providedIn: 'root' })
export class AccountArchiveService {
  private static readonly PERSONALITY_ENTRY = /^personalities\/([^/]+)\/personality\.json$/i;

  async inspect(file: File): Promise<ArchiveContents> {
    const { default: JSZipCtor } = await import('jszip');
    const zip = await JSZipCtor.loadAsync(file);
    const names = Object.keys(zip.files).filter(n => !zip.files[n].dir);

    if (!names.includes('manifest.json')) {
      throw new Error('That file does not look like a WhatIff export (no manifest.json).');
    }

    const personalities = await this.readPersonalities(zip, names);
    const conversations = await this.readConversations(zip);
    const hasMemories = names.includes('memories.zip');

    if (personalities.length === 0 && conversations.length === 0 && !hasMemories) {
      throw new Error('That export has no conversations, personalities, or memories to import.');
    }

    return { personalities, conversations, hasMemories };
  }

  private async readPersonalities(zip: JSZip, names: string[]): Promise<ArchivePersonality[]> {
    const out: ArchivePersonality[] = [];
    for (const path of names) {
      const match = AccountArchiveService.PERSONALITY_ENTRY.exec(path);
      if (!match) continue;
      try {
        const raw = await zip.files[path].async('string');
        const pf = JSON.parse(raw) as { whatiff_personality_id?: string; name?: string };
        const id = (pf.whatiff_personality_id ?? match[1] ?? '').trim();
        const name = (pf.name ?? '').trim();
        if (id && name) {
          out.push({ id, name });
        }
      } catch {
        // Skip unreadable/malformed personality entries — they simply aren't offered.
      }
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }

  private async readConversations(zip: JSZip): Promise<ArchiveConversation[]> {
    const entry = zip.files['conversations.json'];
    if (!entry) return [];
    let parsed: unknown;
    try {
      parsed = JSON.parse(await entry.async('string'));
    } catch {
      return [];
    }
    if (!Array.isArray(parsed)) return [];
    const out: ArchiveConversation[] = [];
    for (const raw of parsed) {
      const c = raw as { uuid?: string; name?: string };
      const id = (c.uuid ?? '').trim();
      if (!id) continue;
      out.push({ id, title: (c.name ?? '').trim() || 'Untitled conversation' });
    }
    return out;
  }
}

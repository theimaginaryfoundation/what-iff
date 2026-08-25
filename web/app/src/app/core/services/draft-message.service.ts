import { Injectable } from '@angular/core';
import { FileAttachment } from '../models/file-attachment.model';
import { Ritual } from '../models/ritual.model';

// Minimal metadata for attachments to reduce storage usage
export interface DraftAttachment {
  id: string;
  name: string;
  file_type: string;
}

// Minimal metadata for rituals to reduce storage usage
export interface DraftRitual {
  id: string;
  name: string;
}

export interface DraftMessage {
  chatId: string;
  message: string;
  timestamp: number;
  attachments?: DraftAttachment[];
  rituals?: DraftRitual[];
}

@Injectable({
  providedIn: 'root'
})
export class DraftMessageService {
  private readonly STORAGE_KEY_PREFIX = 'draftMessage_';

  /**
   * Saves a draft message for a specific chat
   * Only stores minimal metadata (IDs and display info) to reduce storage usage
   */
  saveDraft(
    chatId: string,
    message: string,
    attachments?: FileAttachment[],
    rituals?: Ritual[]
  ): void {
    // Extract only minimal metadata to reduce storage footprint
    const draftAttachments: DraftAttachment[] | undefined =
      attachments && attachments.length > 0
        ? attachments
            .filter(a => a.id && a.name && a.file_type) // Only include valid attachments
            .map(a => ({
              id: a.id!,
              name: a.name,
              file_type: a.file_type
            }))
        : undefined;

    const draftRituals: DraftRitual[] | undefined =
      rituals && rituals.length > 0
        ? rituals
            .filter(r => r.id && r.name) // Only include valid rituals
            .map(r => ({
              id: r.id,
              name: r.name
            }))
        : undefined;

    const draft: DraftMessage = {
      chatId,
      message,
      timestamp: Date.now(),
      attachments: draftAttachments && draftAttachments.length > 0 ? draftAttachments : undefined,
      rituals: draftRituals && draftRituals.length > 0 ? draftRituals : undefined
    };

    const storageKey = this.getStorageKey(chatId);

    try {
      localStorage.setItem(storageKey, JSON.stringify(draft));
    } catch (error) {
      // localStorage can throw QuotaExceededError when storage is full
      // or SecurityError in private browsing mode
      console.error('Failed to save draft message:', error);

      // Attempt to clear old drafts to make space
      if (error instanceof Error && error.name === 'QuotaExceededError') {
        console.warn('Storage quota exceeded. Attempting to clear old drafts...');
        this.clearOldDrafts();

        // Retry once after clearing
        try {
          localStorage.setItem(storageKey, JSON.stringify(draft));
        } catch (retryError) {
          console.error('Failed to save draft after clearing old drafts:', retryError);
          // Silently fail - user's work is still in UI, just not persisted
        }
      }
    }
  }

  /**
   * Gets the draft message for a specific chat
   * Validates the parsed data to protect against corrupted/tampered localStorage
   */
  getDraft(chatId: string): DraftMessage | null {
    try {
      const storageKey = this.getStorageKey(chatId);
      const draftJson = localStorage.getItem(storageKey);
      if (!draftJson) {
        return null;
      }

      const draft = JSON.parse(draftJson);

      // Validate schema to protect against corrupted/tampered data
      if (!this.isValidDraft(draft)) {
        console.warn('Invalid draft schema detected, clearing corrupted draft');
        this.clearDraft(chatId);
        return null;
      }

      // Return null if the draft is older than 24 hours
      const MAX_AGE_MS = 24 * 60 * 60 * 1000;
      if (Date.now() - draft.timestamp > MAX_AGE_MS) {
        this.clearDraft(chatId);
        return null;
      }

      return draft;
    } catch (error) {
      console.error('Failed to parse draft message:', error);
      this.clearDraft(chatId);
      return null;
    }
  }

  /**
   * Validates that a parsed draft object conforms to the expected schema
   */
  private isValidDraft(draft: any): draft is DraftMessage {
    // Check required fields
    if (typeof draft !== 'object' || draft === null) {
      return false;
    }

    if (typeof draft.chatId !== 'string' || !draft.chatId) {
      return false;
    }

    if (typeof draft.message !== 'string') {
      return false;
    }

    if (typeof draft.timestamp !== 'number' || draft.timestamp <= 0) {
      return false;
    }

    // Validate attachments if present
    if (draft.attachments !== undefined) {
      if (!Array.isArray(draft.attachments)) {
        return false;
      }

      for (const attachment of draft.attachments) {
        if (typeof attachment !== 'object' || attachment === null) {
          return false;
        }
        if (typeof attachment.id !== 'string' || !attachment.id) {
          return false;
        }
        if (typeof attachment.name !== 'string' || !attachment.name) {
          return false;
        }
        if (typeof attachment.file_type !== 'string' || !attachment.file_type) {
          return false;
        }
      }
    }

    // Validate rituals if present
    if (draft.rituals !== undefined) {
      if (!Array.isArray(draft.rituals)) {
        return false;
      }

      for (const ritual of draft.rituals) {
        if (typeof ritual !== 'object' || ritual === null) {
          return false;
        }
        if (typeof ritual.id !== 'string' || !ritual.id) {
          return false;
        }
        if (typeof ritual.name !== 'string' || !ritual.name) {
          return false;
        }
      }
    }

    return true;
  }

  /**
   * Clears the draft message for a specific chat, or all drafts if no chatId is provided
   */
  clearDraft(chatId?: string): void {
    if (chatId) {
      const storageKey = this.getStorageKey(chatId);
      localStorage.removeItem(storageKey);
    } else {
      // Clear all drafts (for backwards compatibility or cleanup)
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key && key.startsWith(this.STORAGE_KEY_PREFIX)) {
          keysToRemove.push(key);
        }
      }
      keysToRemove.forEach(key => localStorage.removeItem(key));
    }
  }

  /**
   * Clear all cached draft data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.clearDraft(); // Clear all drafts
  }

  /**
   * Checks if a draft exists for a specific chat
   */
  hasDraft(chatId: string): boolean {
    return this.getDraft(chatId) !== null;
  }

  /**
   * Gets the storage key for a specific chat
   */
  private getStorageKey(chatId: string): string {
    return `${this.STORAGE_KEY_PREFIX}${chatId}`;
  }

  /**
   * Clears drafts older than 7 days to free up storage space
   */
  private clearOldDrafts(): void {
    const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;
    const keysToRemove: string[] = [];

    try {
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key && key.startsWith(this.STORAGE_KEY_PREFIX)) {
          try {
            const draftJson = localStorage.getItem(key);
            if (draftJson) {
              const draft = JSON.parse(draftJson);
              if (Date.now() - draft.timestamp > SEVEN_DAYS_MS) {
                keysToRemove.push(key);
              }
            }
          } catch (parseError) {
            // If we can't parse it, it's corrupted - remove it
            keysToRemove.push(key);
          }
        }
      }

      keysToRemove.forEach(key => {
        try {
          localStorage.removeItem(key);
        } catch (removeError) {
          // Ignore errors when removing individual items
        }
      });
    } catch (error) {
      console.error('Failed to clear old drafts:', error);
    }
  }
}


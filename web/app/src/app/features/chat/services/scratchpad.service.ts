import { Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { ChatService } from '../../../core/services/chat.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';

const SAVE_DEBOUNCE_MS = 500;

@Injectable({ providedIn: 'root' })
export class ScratchpadService {
  private readonly chatService = inject(ChatService);

  readonly loading = signal(false);
  readonly status = signal<'idle' | 'saving' | 'saved' | 'error'>('idle');
  readonly error = signal<string | null>(null);
  readonly value = signal('');
  readonly summary = signal('');

  private activeChatId: string | null = null;
  private canSave = false;
  private dirty = false;
  private saveTimer: ReturnType<typeof setTimeout> | null = null;

  async load(chatId: string | null, canSave: boolean): Promise<void> {
    this.cancelPending();
    this.activeChatId = chatId;
    this.canSave = canSave;
    this.status.set('idle');
    this.error.set(null);
    if (!chatId) {
      this.value.set('');
      this.summary.set('');
      return;
    }

    this.loading.set(true);
    try {
      const context = await firstValueFrom(this.chatService.getChatContext(chatId));
      if (this.activeChatId !== chatId) return;
      this.value.set(context.active_scratchpad ?? '');
      this.summary.set(context.summary ?? '');
      this.dirty = false;
    } catch (error) {
      if (this.activeChatId !== chatId) return;
      this.error.set(apiErrorMessage(error, 'Failed to load scratchpad'));
    } finally {
      if (this.activeChatId === chatId) {
        this.loading.set(false);
      }
    }
  }

  updateDraft(next: string): void {
    this.value.set(next);
    this.dirty = true;
    this.status.set('idle');
    this.error.set(null);
    this.scheduleSave();
  }

  /**
   * Re-pull server context after a background checkpoint updated the scratchpad/summary.
   * Skips when the user has unsaved local edits or a save is in flight so we never
   * clobber in-progress input.
   */
  async refresh(): Promise<void> {
    if (!this.activeChatId || this.dirty || this.status() === 'saving') return;
    await this.load(this.activeChatId, this.canSave);
  }

  async saveNow(): Promise<void> {
    this.cancelPending();
    await this.persist();
  }

  dispose(): void {
    this.cancelPending();
  }

  private scheduleSave(): void {
    this.cancelPending();
    this.saveTimer = setTimeout(() => {
      this.saveTimer = null;
      void this.persist();
    }, SAVE_DEBOUNCE_MS);
  }

  private async persist(): Promise<void> {
    const chatId = this.activeChatId;
    if (!chatId || !this.canSave) return;

    this.status.set('saving');
    this.error.set(null);
    try {
      await firstValueFrom(this.chatService.patchChatContext(chatId, { active_scratchpad: this.value() }));
      if (this.activeChatId !== chatId) return;
      this.dirty = false;
      this.status.set('saved');
    } catch (error) {
      if (this.activeChatId !== chatId) return;
      this.status.set('error');
      this.error.set(apiErrorMessage(error, 'Failed to save scratchpad'));
    }
  }

  private cancelPending(): void {
    if (this.saveTimer) {
      clearTimeout(this.saveTimer);
      this.saveTimer = null;
    }
  }
}

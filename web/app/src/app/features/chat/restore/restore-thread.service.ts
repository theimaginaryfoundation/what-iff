import { Injectable, inject, signal } from '@angular/core';
import { Chat } from '../../../core/models/chat.model';
import { ThreadListService } from '../../../core/services/thread-list.service';

/** Explicit confirmation shared by single, bulk and in-thread unarchive actions. */
@Injectable({ providedIn: 'root' })
export class RestoreThreadService {
  private readonly threads = inject(ThreadListService);
  readonly targets = signal<readonly Chat[]>([]);
  readonly busy = signal(false);
  readonly error = signal('');
  readonly guidance = signal('');
  private generation = 0;

  request(chats: readonly Chat[]): void {
    if (this.targets().length || this.busy()) return;
    const targets = [...new Map(chats.filter(chat => chat.archived).map(chat => [chat.id, chat])).values()];
    if (!targets.length) return;
    this.targets.set(targets);
    this.guidance.set(targets.length === 1 ? targets[0].restore_guidance ?? '' : '');
    this.error.set('');
  }

  cancel(): void { if (!this.busy()) this.reset(); }

  /** Also called when the authenticated layout is destroyed, so drafts cannot cross accounts. */
  reset(): void {
    this.generation++;
    this.targets.set([]);
    this.guidance.set('');
    this.error.set('');
    this.busy.set(false);
  }

  async confirm(): Promise<void> {
    if (this.busy() || !this.targets().length) return;
    if ([...this.guidance()].length > 4000) { this.error.set('Use at most 4,000 characters.'); return; }
    const generation = this.generation;
    const guidance = this.guidance().trim();
    this.busy.set(true);
    this.error.set('');
    const failed: Chat[] = [];
    for (const chat of this.targets()) {
      if (generation !== this.generation) return;
      const restored = await this.threads.setThreadArchived(chat, false, guidance);
      if (generation !== this.generation) return;
      if (restored) this.threads.setAllSelected([chat.id], false);
      else failed.push(chat);
    }
    if (generation !== this.generation) return;
    this.busy.set(false);
    if (!failed.length) { this.reset(); return; }
    this.targets.set(failed);
    this.error.set(this.threads.error() || `Could not unarchive ${failed.length} thread${failed.length === 1 ? '' : 's'}. You can retry or cancel.`);
  }
}

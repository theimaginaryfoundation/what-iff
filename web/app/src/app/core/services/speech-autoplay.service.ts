import { DOCUMENT } from '@angular/common';
import { Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { SpeechPlayerService } from './speech-player.service';
import { SpeechService } from './speech.service';

/** A chat's autoplay: its personality's default, and the chat's own override (null follows it). */
export interface ChatAutoplay {
  personality: boolean;
  override: boolean | null;
}

/**
 * Reads replies aloud as they arrive in chats that ask for it (ADR 0x052 §9).
 *
 * Whether a reply should be read was decided on the server when its turn started: its track is
 * pending. This client claims a pending track only when it can play it right then (the chat is open
 * and in view, sound is unlocked by an earlier tap, dictation isn't recording, nothing else is
 * playing), and the server lets exactly one client have it. It looks when the turn it is watching
 * carries a track, when a chat opens, and when the tab comes back into view: a reply that landed
 * while the person was away plays when they return. Of several pending, the newest plays and the
 * rest are skipped. Before any tap, browsers block sound, so the reply waits as "Tap to listen".
 */
@Injectable({ providedIn: 'root' })
export class SpeechAutoplayService {
  private readonly player = inject(SpeechPlayerService);
  private readonly speech = inject(SpeechService);
  private readonly document = inject(DOCUMENT);

  /** The chat on screen, or null. */
  readonly openChat = signal<string | null>(null);
  /** A pending reply that sound isn't unlocked for yet: tapping plays it. */
  readonly tapToListen = signal<{ trackId: string; chatId: string } | null>(null);
  private readonly settings = signal<Readonly<Record<string, ChatAutoplay>>>({});
  /** Tracks this client has handled, so a turn's polls don't claim one twice. */
  private readonly handled = new Set<string>();

  constructor() {
    const unlock = () => this.player.unlock();
    this.document.addEventListener('pointerdown', unlock, { capture: true, once: true });
    this.document.addEventListener('keydown', unlock, { capture: true, once: true });
    this.document.addEventListener('visibilitychange', () => {
      const chatId = this.openChat();
      if (this.document.visibilityState === 'visible' && chatId) void this.checkPending(chatId);
    });
  }

  /** The chat now on screen; null when none. Looks for replies waiting to be read. */
  setOpenChat(chatId: string | null): void {
    if (this.openChat() === chatId) return;
    this.openChat.set(chatId);
    this.tapToListen.set(null);
    if (chatId) void this.checkPending(chatId);
  }

  setChatAutoplay(chatId: string, value: ChatAutoplay): void {
    this.settings.update(s => ({ ...s, [chatId]: value }));
  }

  chatAutoplay(chatId: string | null | undefined): ChatAutoplay | null {
    return chatId ? (this.settings()[chatId] ?? null) : null;
  }

  /** Whether the chat reads replies aloud as they arrive. */
  effective(chatId: string | null | undefined): boolean {
    const s = this.chatAutoplay(chatId);
    return !!s && (s.override ?? s.personality);
  }

  /** Overrides the chat's autoplay; null follows the personality again. */
  async setOverride(chatId: string, override: boolean | null): Promise<void> {
    const before = this.chatAutoplay(chatId);
    if (before) this.setChatAutoplay(chatId, { ...before, override });
    try {
      await firstValueFrom(this.speech.setChatAutoplay(chatId, override));
    } catch (err) {
      if (before) this.setChatAutoplay(chatId, before);
      throw err;
    }
  }

  /** The turn this client watches carries a track: the reply can be heard as it streams. */
  onTurnTrack(chatId: string, trackId: string): void {
    if (this.handled.has(trackId)) return;
    this.handled.add(trackId);
    void this.tryClaim(trackId, chatId);
  }

  /** Plays the newest reply waiting in the chat, and skips the older ones. */
  async checkPending(chatId: string): Promise<void> {
    let pending;
    try {
      pending = await firstValueFrom(this.speech.pending(chatId));
    } catch {
      return;
    }
    const [newest, ...older] = pending;
    for (const t of older) {
      this.handled.add(t.id);
      void firstValueFrom(this.speech.skip(t.id)).catch(() => undefined);
    }
    if (!newest || this.handled.has(newest.id)) return;
    this.handled.add(newest.id);
    await this.tryClaim(newest.id, chatId);
  }

  /** Plays the reply waiting for a tap. */
  async tapToPlay(): Promise<void> {
    const waiting = this.tapToListen();
    if (!waiting) return;
    this.tapToListen.set(null);
    this.player.unlock();
    await this.claimAndPlay(waiting.trackId);
  }

  private canPlayNow(chatId: string): boolean {
    return (
      this.document.visibilityState === 'visible' &&
      this.openChat() === chatId &&
      this.player.unlocked() &&
      !this.player.recording() &&
      !this.player.playing() &&
      !this.player.loading()
    );
  }

  private async tryClaim(trackId: string, chatId: string): Promise<void> {
    if (this.canPlayNow(chatId)) {
      await this.claimAndPlay(trackId);
      return;
    }
    if (this.document.visibilityState === 'visible' && this.openChat() === chatId && !this.player.unlocked()) {
      this.tapToListen.set({ trackId, chatId });
      return;
    }
    // Not now (away, recording, or something playing): leave it pending, to be looked for again
    // when the person comes back.
    this.handled.delete(trackId);
  }

  private async claimAndPlay(trackId: string): Promise<void> {
    try {
      const { claimed, track } = await firstValueFrom(this.speech.claim(trackId));
      if (claimed) await this.player.playTrack(track);
    } catch {
      // Not playing it is the safe failure; the reply keeps its Listen button.
    }
  }
}

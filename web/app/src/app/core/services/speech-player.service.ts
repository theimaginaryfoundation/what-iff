import { HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject, signal } from '@angular/core';
import { NavigationStart, Router } from '@angular/router';
import { filter, firstValueFrom } from 'rxjs';

import { SpeechTrack } from '../models/speech.model';
import { apiErrorMessage } from '../utils/api-error.helpers';
import { SpeechService } from './speech.service';

/** How often a track still being made is asked for again. */
const TRACK_POLL_MS = 1000;
/**
 * How long a track may go without a new segment before the player gives up: past two of the
 * server's 90-second worker leases, so a worker that went away has been replaced by then.
 */
const TRACK_STALL_MS = 200_000;
/** How many times in a row a segment may fail to load while its track is still open. */
const MAX_SEGMENT_RETRIES = 3;

/**
 * Plays replies aloud (ADR 0x052 §7): one player for the whole app, so starting a reply stops
 * whatever was playing. A reply's track is made on the server segment by segment; the player
 * decodes each segment as it arrives and schedules it right after the one before with Web Audio,
 * so there is no gap between them, and asks for the track again while more are coming.
 *
 * Leaving the page stops playback, as does starting dictation.
 */
@Injectable({ providedIn: 'root' })
export class SpeechPlayerService {
  private readonly speech = inject(SpeechService);

  /** The reply playing, or null. */
  readonly playing = signal<string | null>(null);
  /** The reply whose first segment is on its way, or null. */
  readonly loading = signal<string | null>(null);
  /** Dictation is recording: nothing starts playing on its own meanwhile. */
  readonly recording = signal(false);
  /** Whether a gesture has let this page play sound yet (browsers block it before one). */
  readonly unlocked = signal(false);
  /** Why the last reply couldn't be read, with the reply, or null. */
  readonly error = signal<{ messageId: string; text: string } | null>(null);

  /** Which chats' personalities have a voice, as the chat page learns them. */
  private readonly chatVoices = signal<Readonly<Record<string, boolean>>>({});

  /** Stands in for the browser's AudioContext in tests. */
  createContext: () => AudioContext = () => new AudioContext();
  /** Waits between polls; tests make it immediate. */
  wait: (ms: number) => Promise<void> = ms => new Promise(resolve => setTimeout(resolve, ms));

  private context: AudioContext | null = null;
  private sources: AudioBufferSourceNode[] = [];
  /** Bumped by every play and stop, so a run that was superseded ends quietly. */
  private session = 0;

  constructor() {
    // Leaving the page stops playback. A stand-in router in a test may have no events.
    inject(Router, { optional: true })
      ?.events?.pipe(filter(e => e instanceof NavigationStart))
      .subscribe(() => this.stop());
  }

  setChatVoice(chatId: string, hasVoice: boolean): void {
    this.chatVoices.update(v => (v[chatId] === hasVoice ? v : { ...v, [chatId]: hasVoice }));
  }

  chatHasVoice(chatId: string | null | undefined): boolean {
    return !!chatId && !!this.chatVoices()[chatId];
  }

  /**
   * Makes sure audio can start. Browsers only let a page start sound after a gesture, so this is
   * called from one (a click on Listen, Send or the microphone).
   */
  unlock(): AudioContext {
    if (!this.context || this.context.state === 'closed') {
      this.context = this.createContext();
    }
    if (this.context.state === 'suspended') {
      void this.context.resume();
    }
    this.unlocked.set(true);
    return this.context;
  }

  /**
   * Whether sound can start now without a gesture: unlocked by an earlier one, and not suspended
   * since (iOS suspends audio after an interruption or a while in the background, and only a gesture
   * resumes it).
   */
  soundReady(): boolean {
    return this.unlocked() && this.context?.state === 'running';
  }

  /** Plays a reply aloud from the start, stopping anything else. */
  async play(messageId: string): Promise<void> {
    this.stop();
    const session = this.session;
    const context = this.unlock();
    this.error.set(null);
    this.loading.set(messageId);
    try {
      const track = await firstValueFrom(this.speech.listen(messageId));
      await this.follow(session, context, messageId, track);
    } catch (err) {
      if (session !== this.session) return;
      this.stop();
      this.error.set({ messageId, text: playFailure(err) });
    }
  }

  /**
   * Plays a track this client claimed, from its first segment, following it while it is still
   * being made: a reply read as it streams (ADR 0x052 §8). Until the track knows its reply, it is
   * keyed by `track:<id>`.
   */
  async playTrack(track: SpeechTrack): Promise<void> {
    this.stop();
    const session = this.session;
    const context = this.unlock();
    const key = track.message_id ?? `track:${track.id}`;
    this.error.set(null);
    this.loading.set(key);
    try {
      await this.follow(session, context, key, track);
    } catch (err) {
      if (session !== this.session) return;
      this.stop();
      // A track that's gone was a turn that failed: there was nothing to read, which isn't an error.
      if (err instanceof HttpErrorResponse && err.status === 404) return;
      this.error.set({ messageId: key, text: playFailure(err) });
    }
  }

  /**
   * Schedules the track's segments back to back as they arrive, asking for the track again while it
   * is open. A segment the server withdrew (a round read as it streamed that turned out to be
   * narration) is cancelled if it hasn't started, and the track goes on from there.
   */
  private async follow(session: number, context: AudioContext, key: string, track: SpeechTrack): Promise<void> {
    const scheduled: Scheduled[] = [];
    let startAt = context.currentTime + 0.05;
    let waited = 0;
    let fetchFailures = 0;
    while (session === this.session) {
      const withdrawn = scheduled.findIndex(s => track.segments[s.seq]?.id !== s.id);
      if (withdrawn >= 0) {
        // A round read out turned out not to be the last: its audio stops, playing or not, and
        // what replaces it follows what is kept.
        for (const s of scheduled.splice(withdrawn)) {
          try {
            s.source.stop();
          } catch {
            // Already ended.
          }
        }
        startAt = Math.max(context.currentTime, scheduled.at(-1)?.end ?? context.currentTime);
      }
      if (track.message_id && key !== track.message_id) {
        key = track.message_id;
        if (this.loading()) this.loading.set(key);
        if (this.playing()) this.playing.set(key);
      }
      const next = scheduled.length;
      if (next < track.segments.length) {
        const seg = track.segments[next];
        let data: ArrayBuffer;
        try {
          data = await firstValueFrom(this.speech.segment(track.id, seg.seq));
        } catch (err) {
          // The segment may have been withdrawn since this copy of the track was read: read the
          // track again rather than give up, unless that keeps failing.
          if (session !== this.session) return;
          if (track.status !== 'open' || ++fetchFailures > MAX_SEGMENT_RETRIES) throw err;
          await this.wait(TRACK_POLL_MS);
          if (session !== this.session) return;
          track = await firstValueFrom(this.speech.track(track.id));
          continue;
        }
        if (session !== this.session) return;
        const buffer = await context.decodeAudioData(data);
        if (session !== this.session) return;
        startAt = Math.max(startAt, context.currentTime);
        const source = this.schedule(context, buffer, startAt);
        scheduled.push({ id: seg.id, seq: seg.seq, source, start: startAt, end: startAt + buffer.duration });
        startAt += buffer.duration;
        waited = 0;
        fetchFailures = 0;
        this.loading.set(null);
        this.playing.set(key);
        continue;
      }
      if (track.status !== 'open') break;
      if (waited >= TRACK_STALL_MS) {
        throw new Error('stalled');
      }
      await this.wait(TRACK_POLL_MS);
      waited += TRACK_POLL_MS;
      if (session !== this.session) return;
      track = await firstValueFrom(this.speech.track(track.id));
    }
    if (session !== this.session) return;
    this.loading.set(null);
    if (track.status === 'stopped' || track.status === 'failed') {
      this.error.set({ messageId: key, text: trackFailure(track) });
    }
    // Done when the last scheduled segment ends.
    const remaining = Math.max(0, startAt - context.currentTime);
    await this.wait(remaining * 1000);
    if (session === this.session) {
      this.playing.set(null);
      this.sources = [];
    }
  }

  /** Plays one piece of audio, such as a voice preview, stopping anything else. */
  async playAudio(data: ArrayBuffer): Promise<void> {
    this.stop();
    const session = this.session;
    const context = this.unlock();
    const buffer = await context.decodeAudioData(data);
    if (session !== this.session) return;
    this.schedule(context, buffer, context.currentTime);
  }

  /** Stops whatever is playing. */
  stop(): void {
    this.session++;
    for (const s of this.sources) {
      try {
        s.stop();
      } catch {
        // Already stopped.
      }
    }
    this.sources = [];
    this.playing.set(null);
    this.loading.set(null);
  }

  private schedule(context: AudioContext, buffer: AudioBuffer, at: number): AudioBufferSourceNode {
    const source = context.createBufferSource();
    source.buffer = buffer;
    source.connect(context.destination);
    source.start(at);
    this.sources.push(source);
    return source;
  }
}

/** One segment scheduled to play. */
interface Scheduled {
  id: string;
  seq: number;
  source: AudioBufferSourceNode;
  start: number;
  end: number;
}

function trackFailure(track: SpeechTrack): string {
  if (track.status === 'stopped' || track.error_code === 'quota_exceeded') {
    return "You're out of credits for voice replies.";
  }
  if (track.error_code === 'provider_not_configured') {
    return "The voice's provider has no key.";
  }
  return "Couldn't read the rest of this reply aloud.";
}

function playFailure(err: unknown): string {
  if (err instanceof Error && err.message === 'stalled') {
    return 'Reading aloud stopped responding. Try again.';
  }
  if (err instanceof HttpErrorResponse && err.status === 409) {
    return apiErrorMessage(err, "This personality's voice isn't available.");
  }
  return apiErrorMessage(err, "Couldn't read this reply aloud.");
}

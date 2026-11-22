import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject, signal } from '@angular/core';
import { catchError, firstValueFrom, map, Observable, throwError } from 'rxjs';

import { environment } from '../../../environments/environment';
import { Personality } from '../models/personality.model';
import { DictationClip, DictationModel, PersonalityVoice, SpeechTrack } from '../models/speech.model';

/**
 * Voice replies and dictation (ADR 0x052). The dictation model is read once and
 * kept, so every composer knows whether it has a microphone without asking
 * again; settings refresh it after a change.
 */
@Injectable({ providedIn: 'root' })
export class SpeechService {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = `${environment.apiUrl}/speech`;

  /** The account's dictation model, or null until read. */
  readonly dictation = signal<DictationModel | null>(null);
  private loading: Promise<DictationModel> | null = null;
  /** Bumped on logout, so a read still in flight for the last account is dropped. */
  private generation = 0;

  /** Forgets the account's dictation model, on logout. */
  clearCache(): void {
    this.generation++;
    this.loading = null;
    this.dictation.set(null);
  }

  /** Reads the dictation model, once unless asked to read it again. */
  loadDictation(refresh = false): Promise<DictationModel> {
    if (!refresh && this.dictation()) {
      return Promise.resolve(this.dictation()!);
    }
    if (!refresh && this.loading) {
      return this.loading;
    }
    const generation = this.generation;
    const loading = firstValueFrom(this.http.get<DictationModel>(`${this.baseUrl}/dictation`))
      .catch((): DictationModel => ({ available: false })) // no microphone rather than a broken one; a refresh tries again
      .then(m => {
        if (generation === this.generation) {
          this.dictation.set(m);
        }
        return m;
      })
      .finally(() => {
        if (this.loading === loading) {
          this.loading = null;
        }
      });
    this.loading = loading;
    return loading;
  }

  /** Transcribes one clip (16 kHz mono PCM WAV); the server never stores it. */
  transcribe(clip: DictationClip, chatId: string | null, language?: string): Observable<string> {
    const form = new FormData();
    form.append('audio', clip.audio, 'clip.wav');
    form.append('format', 'wav');
    if (chatId) {
      form.append('chat_id', chatId);
    }
    if (language) {
      form.append('language', language);
    }
    return this.http.post<{ text: string }>(`${this.baseUrl}/transcriptions`, form).pipe(map(r => r.text));
  }

  /** The reply's track in its personality's voice, started if it isn't there yet. */
  listen(messageId: string): Observable<SpeechTrack> {
    return this.http.post<SpeechTrack>(`${this.baseUrl}/messages/${messageId}`, {});
  }

  track(trackId: string): Observable<SpeechTrack> {
    return this.http.get<SpeechTrack>(`${this.baseUrl}/tracks/${trackId}`);
  }

  /** One segment's audio, to decode. */
  segment(trackId: string, seq: number): Observable<ArrayBuffer> {
    return this.http
      .get(`${this.baseUrl}/tracks/${trackId}/segments/${seq}`, { responseType: 'arraybuffer' })
      .pipe(catchError(decodeErrorBody));
  }

  /** Text read aloud in a voice not yet saved; billed, not stored. */
  preview(voice: PersonalityVoice, text: string): Observable<ArrayBuffer> {
    return this.http.post(`${this.baseUrl}/preview`, { ...voice, text }, { responseType: 'arraybuffer' }).pipe(catchError(decodeErrorBody));
  }

  setVoice(personalityId: string, voice: PersonalityVoice): Observable<Personality> {
    return this.http.put<Personality>(`${environment.apiUrl}/personality/${personalityId}/voice`, voice);
  }

  clearVoice(personalityId: string): Observable<Personality> {
    return this.http.delete<Personality>(`${environment.apiUrl}/personality/${personalityId}/voice`);
  }
}

/**
 * An audio request's error, with its body read as the JSON envelope it is: asked for as an
 * ArrayBuffer, the body would otherwise hide the server's code and message (quota, too long,
 * voice unavailable) behind a generic one.
 */
export function decodeErrorBody(err: unknown): Observable<never> {
  // A tag check, not instanceof: a buffer from another realm (a worker, a test DOM) fails that.
  if (err instanceof HttpErrorResponse && Object.prototype.toString.call(err.error) === '[object ArrayBuffer]') {
    let body: unknown = null;
    try {
      body = JSON.parse(new TextDecoder().decode(new Uint8Array(err.error as ArrayBuffer)));
    } catch {
      // Not JSON: keep the status alone.
    }
    return throwError(
      () =>
        new HttpErrorResponse({
          error: body,
          headers: err.headers,
          status: err.status,
          statusText: err.statusText,
          url: err.url ?? undefined,
        }),
    );
  }
  return throwError(() => err);
}

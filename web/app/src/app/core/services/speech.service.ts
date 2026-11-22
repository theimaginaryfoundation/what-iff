import { HttpClient } from '@angular/common/http';
import { Injectable, inject, signal } from '@angular/core';
import { Observable, firstValueFrom, map } from 'rxjs';

import { environment } from '../../../environments/environment';
import { DictationClip, DictationModel } from '../models/speech.model';

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
}

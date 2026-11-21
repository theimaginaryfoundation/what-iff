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

  /** Reads the dictation model, once unless asked to read it again. */
  loadDictation(refresh = false): Promise<DictationModel> {
    if (!refresh && this.dictation()) {
      return Promise.resolve(this.dictation()!);
    }
    if (!refresh && this.loading) {
      return this.loading;
    }
    this.loading = firstValueFrom(this.http.get<DictationModel>(`${this.baseUrl}/dictation`))
      .then(m => {
        this.dictation.set(m);
        return m;
      })
      .catch(() => {
        // No microphone rather than a broken one; a later refresh tries again.
        const none: DictationModel = { available: false };
        this.dictation.set(none);
        return none;
      })
      .finally(() => {
        this.loading = null;
      });
    return this.loading;
  }

  /** Transcribes one clip; the server never stores it. */
  transcribe(clip: DictationClip, chatId: string | null, language?: string): Observable<string> {
    const form = new FormData();
    form.append('audio', clip.audio, `clip.${clip.format === 'mp4' ? 'm4a' : clip.format}`);
    form.append('format', clip.format);
    form.append('duration_seconds', clip.seconds.toFixed(2));
    if (chatId) {
      form.append('chat_id', chatId);
    }
    if (language) {
      form.append('language', language);
    }
    return this.http.post<{ text: string }>(`${this.baseUrl}/transcriptions`, form).pipe(map(r => r.text));
  }
}

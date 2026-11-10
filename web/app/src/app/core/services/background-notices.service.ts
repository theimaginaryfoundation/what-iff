import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { BackgroundNotice } from '../models/utility-model.model';

/** A chat's background work that failed or fell back (ADR 0x027 section 6). */
@Injectable({ providedIn: 'root' })
export class BackgroundNoticesService {
  private readonly http = inject(HttpClient);

  list(chatId: string): Observable<BackgroundNotice[]> {
    return this.http.get<BackgroundNotice[]>(`${environment.apiUrl}/chat/${chatId}/background-notices`);
  }

  /** Hides a notice until its cause changes. */
  dismiss(chatId: string, noticeId: string): Observable<void> {
    return this.http.post<void>(`${environment.apiUrl}/chat/${chatId}/background-notices/${noticeId}/dismiss`, null);
  }
}

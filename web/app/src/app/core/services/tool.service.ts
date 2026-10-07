import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '@environments/environment';

export interface ToolMeta {
  name: string;
  description: string;
  /** What the tool can do and which options it takes; shown as its tooltip. */
  guide?: string;
  /**
   * What a sandboxed thread does with the tool: `never` (refused there, whatever the toggle
   * says), `default_off` (a new sandbox starts with it off) or `allowed`.
   */
  sandbox?: 'never' | 'default_off' | 'allowed';
}

@Injectable({
  providedIn: 'root',
})
export class ToolService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/tools`;

  listTools(): Observable<ToolMeta[]> {
    return this.http.get<ToolMeta[]>(this.apiUrl);
  }
}

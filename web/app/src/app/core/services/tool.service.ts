import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '@environments/environment';

export interface ToolMeta {
  name: string;
  description: string;
}

@Injectable({
  providedIn: 'root'
})
export class ToolService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/tools`;

  listTools(): Observable<ToolMeta[]> {
    return this.http.get<ToolMeta[]>(this.apiUrl);
  }
}

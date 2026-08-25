import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../../../environments/environment';
import { FileAttachment } from '../models/file-attachment.model';
import { PaginatedResponse } from '../models/common.model';

@Injectable({
  providedIn: 'root'
})
export class ImageGalleryService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}`;

  listImages(
    page: number = 1,
    limit: number = 20,
    filters?: { name?: string; personalityId?: string; globalOnly?: boolean },
  ): Observable<PaginatedResponse<FileAttachment>> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());
    if (filters?.name?.trim()) {
      params = params.set('name', filters.name.trim());
    }
    if (filters?.personalityId?.trim()) {
      params = params.set('personality_id', filters.personalityId.trim());
    }
    if (filters?.globalOnly) {
      params = params.set('global_only', 'true');
    }

    return this.http.get<PaginatedResponse<FileAttachment>>(`${this.apiUrl}/image-gallery`, { params });
  }

  /** Returns the URL to stream image bytes from the backend. */
  getImageUrl(id: string, size: 'thumbnail' | 'full' = 'thumbnail'): string {
    return `${this.apiUrl}/image-gallery/${id}?size=${size}`;
  }

  deleteImage(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/image-gallery/${id}`);
  }

  importImage(file: File, metadata?: { title?: string; description?: string }): Observable<FileAttachment> {
    const formData = new FormData();
    formData.append('attachment', file);
    if (metadata?.title?.trim()) {
      formData.append('title', metadata.title.trim());
    }
    if (metadata?.description?.trim()) {
      formData.append('description', metadata.description.trim());
    }
    return this.http.post<FileAttachment>(`${this.apiUrl}/image-gallery/import`, formData);
  }

  renameImage(id: string, name: string): Observable<FileAttachment> {
    return this.http.patch<FileAttachment>(`${this.apiUrl}/image-gallery/${id}`, { name });
  }

  /** Creates a lightweight reference attachment for attaching a gallery image to a chat. */
  referenceImage(id: string): Observable<FileAttachment> {
    return this.http.post<FileAttachment>(`${this.apiUrl}/image-gallery/${id}/reference`, {});
  }
}

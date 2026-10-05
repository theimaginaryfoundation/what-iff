import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, map } from 'rxjs';
import { environment } from '../../../environments/environment';
import { FileAttachment } from '../models/file-attachment.model';
import { PaginatedResponse } from '../models/common.model';
import { GalleryFolder } from '../../features/gallery/helpers/gallery-folder.helpers';

@Injectable({
  providedIn: 'root'
})
export class ImageGalleryService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}`;

  listImages(
    page: number = 1,
    limit: number = 20,
    filters?: { name?: string; personalityId?: string; globalOnly?: boolean; folder?: string },
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
    // An empty folder is the top level, so a defined value is always sent; leave it undefined
    // to list every image.
    if (filters?.folder !== undefined) {
      params = params.set('folder', filters.folder);
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

  importImage(file: File, metadata?: { title?: string; description?: string; folder?: string }): Observable<FileAttachment> {
    const formData = new FormData();
    formData.append('attachment', file);
    if (metadata?.title?.trim()) {
      formData.append('title', metadata.title.trim());
    }
    if (metadata?.description?.trim()) {
      formData.append('description', metadata.description.trim());
    }
    if (metadata?.folder?.trim()) {
      formData.append('folder', metadata.folder.trim());
    }
    return this.http.post<FileAttachment>(`${this.apiUrl}/image-gallery/import`, formData);
  }

  renameImage(id: string, name: string): Observable<FileAttachment> {
    return this.http.patch<FileAttachment>(`${this.apiUrl}/image-gallery/${id}`, { name });
  }

  /** Folders that hold gallery images, with the number of images directly in each. */
  listFolders(): Observable<GalleryFolder[]> {
    return this.http
      .get<{ folders: GalleryFolder[] }>(`${this.apiUrl}/image-gallery/folders`)
      .pipe(map(response => response.folders ?? []));
  }

  /** Files images in a folder ("" is the top level); resolves with how many were moved. */
  moveImages(ids: readonly string[], folder: string): Observable<number> {
    return this.http
      .post<{ moved: number }>(`${this.apiUrl}/image-gallery/move`, { ids, folder })
      .pipe(map(response => response.moved));
  }

  /** Renames a folder, carrying everything beneath it; resolves with how many images moved. */
  moveFolder(from: string, to: string): Observable<number> {
    return this.http
      .post<{ moved: number }>(`${this.apiUrl}/image-gallery/folders/move`, { from, to })
      .pipe(map(response => response.moved));
  }

  /** Creates a lightweight reference attachment for attaching a gallery image to a chat. */
  referenceImage(id: string): Observable<FileAttachment> {
    return this.http.post<FileAttachment>(`${this.apiUrl}/image-gallery/${id}/reference`, {});
  }
}

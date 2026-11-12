import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, map, switchMap } from 'rxjs';
import { environment } from '../../../environments/environment';
import { FileAttachment } from '../models/file-attachment.model';
import { PaginatedResponse } from '../models/common.model';
import { GalleryFolder } from '../../features/gallery/helpers/gallery-folder.helpers';
import { DiagramUploads, titleFor } from '../../features/diagrams/diagram-uploads';

/** Which gallery files to list: images (the server's default), every other file, or both. */
export type GalleryKind = 'images' | 'files' | 'all';

/** The most ids one move or bulk delete request takes (the server's cap); send more in chunks. */
export const GALLERY_BATCH_LIMIT = 500;

/** What POST /image-gallery/delete did with each id. */
export interface GalleryDeleteResult {
  deleted: string[];
  /** Ids not deleted, with a reason safe to show ("not found", "could not delete the stored image", …). */
  failed: { id: string; error: string }[];
}

@Injectable({
  providedIn: 'root',
})
export class ImageGalleryService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}`;
  private diagramUploads = inject(DiagramUploads);

  listImages(
    page: number = 1,
    limit: number = 20,
    filters?: { name?: string; personalityId?: string; globalOnly?: boolean; folder?: string; kind?: GalleryKind },
  ): Observable<PaginatedResponse<FileAttachment>> {
    let params = new HttpParams().set('page', page.toString()).set('limit', limit.toString());
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
    if (filters?.kind) {
      params = params.set('kind', filters.kind);
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

  /**
   * Deletes up to {@link GALLERY_BATCH_LIMIT} gallery files of any kind, each with its own route's
   * cleanup. Resolves with which went and which did not (and why); ids that are not the caller's
   * come back as failed, "not found".
   */
  deleteFiles(ids: readonly string[]): Observable<GalleryDeleteResult> {
    return this.http.post<GalleryDeleteResult>(`${this.apiUrl}/image-gallery/delete`, { ids });
  }

  /** Imports any file the chat upload takes; a Visio drawing arrives as the .drawio diagram it converts to (ADR 0x049 §7). */
  importImage(file: File, metadata?: { title?: string; description?: string; folder?: string }): Observable<FileAttachment> {
    return this.diagramUploads
      .prepare(file)
      .pipe(switchMap(prepared => this.postImport(prepared, metadata && { ...metadata, title: titleFor(metadata.title, prepared) })));
  }

  private postImport(file: File, metadata?: { title?: string; description?: string; folder?: string }): Observable<FileAttachment> {
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

  /** Folders that hold gallery files of this kind (images by default), with how many are directly in each. */
  listFolders(kind?: GalleryKind): Observable<GalleryFolder[]> {
    const params = kind ? new HttpParams().set('kind', kind) : undefined;
    return this.http
      .get<{ folders: GalleryFolder[] }>(`${this.apiUrl}/image-gallery/folders`, { params })
      .pipe(map(response => response.folders ?? []));
  }

  /** One file's metadata, any kind, for opening the viewer from a link. */
  getFileInfo(id: string): Observable<FileAttachment> {
    return this.http.get<FileAttachment>(`${this.apiUrl}/image-gallery/${id}/info`);
  }

  /** Files images or other files in a folder ("" is the top level); resolves with how many were moved. */
  moveImages(ids: readonly string[], folder: string): Observable<number> {
    return this.http.post<{ moved: number }>(`${this.apiUrl}/image-gallery/move`, { ids, folder }).pipe(map(response => response.moved));
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

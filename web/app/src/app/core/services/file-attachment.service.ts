import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../../../environments/environment';
import { FileAttachment, FileAttachmentFilters } from '../models/file-attachment.model';
import { PaginatedResponse } from '../models/common.model';

@Injectable({
  providedIn: 'root'
})
export class FileAttachmentService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}`;

  /**
   * Upload a file attachment to a specific chat
   * @param chatId The ID of the chat to attach the file to
   * @param file The file to upload
   * @returns Observable of the created file attachment
   */
  uploadChatFileAttachment(chatId: string, file: File): Observable<FileAttachment> {
    const formData = new FormData();
    formData.append('attachment', file);

    return this.http.post<FileAttachment>(`${this.apiUrl}/chat/${chatId}/file-attachment`, formData);
  }

  /**
   * List file attachments with optional filtering
   * @param page Page number (default 1)
   * @param limit Number of items per page (default 10)
   * @param filters Optional filters to apply
   * @returns Observable of paginated file attachments
   */
  listFileAttachments(page: number = 1, limit: number = 10, filters?: FileAttachmentFilters): Observable<PaginatedResponse<FileAttachment>> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());

    if (filters) {
      if (filters.name) params = params.set('name', filters.name);
      if (filters.file_type) params = params.set('file_type', filters.file_type);
      if (filters.chat_message_id) params = params.set('chat_message_id', filters.chat_message_id);
      if (filters.personality_id) params = params.set('personality_id', filters.personality_id);
      if (filters.docs_only) params = params.set('docs_only', 'true');
      if (filters.min_date) params = params.set('min_date', filters.min_date);
      if (filters.max_date) params = params.set('max_date', filters.max_date);
    }

    return this.http.get<PaginatedResponse<FileAttachment>>(`${this.apiUrl}/file-attachment`, { params });
  }

  /**
   * Upload a file attachment to a specific personality
   * @param personalityId The ID of the personality to attach the file to
   * @param file The file to upload
   * @returns Observable of the created file attachment
   */
  uploadPersonalityFileAttachment(
    personalityId: string,
    file: File,
    metadata?: { title?: string; description?: string },
  ): Observable<FileAttachment> {
    const formData = new FormData();
    formData.append('attachment', file);
    if (metadata?.title?.trim()) {
      formData.append('title', metadata.title.trim());
    }
    if (metadata?.description?.trim()) {
      formData.append('description', metadata.description.trim());
    }

    return this.http.post<FileAttachment>(`${this.apiUrl}/personality/${personalityId}/file-attachment`, formData);
  }

  /**
   * Delete a file attachment
   * @param attachmentId The ID of the attachment to delete
   * @returns Observable of the deletion result
   */
  deleteFileAttachment(attachmentId: string): Observable<any> {
    return this.http.delete(`${this.apiUrl}/file-attachment/${attachmentId}`);
  }
}


import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { PaginatedResponse } from '../models/common.model';
import { apiErrorMessage } from '../utils/api-error.helpers';
import {
  MCPServer,
  MCPServerFilters,
  CreateMCPServerRequest,
  UpdateMCPServerRequest
} from '../models/mcp-server.model';

@Injectable({
  providedIn: 'root'
})
export class MCPServerService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/mcp-servers`;

  listMCPServers(page: number = 1, limit: number = 20, filters?: MCPServerFilters): Observable<PaginatedResponse<MCPServer>> {
    const params: any = { page: page.toString(), limit: limit.toString() };
    if (filters?.search) {
      params.search = filters.search;
    }
    return this.http.get<PaginatedResponse<MCPServer>>(this.apiUrl, { params })
      .pipe(catchError(this.handleError));
  }

  getMCPServer(id: string): Observable<MCPServer> {
    return this.http.get<MCPServer>(`${this.apiUrl}/${id}`)
      .pipe(catchError(this.handleError));
  }

  createMCPServer(request: CreateMCPServerRequest): Observable<MCPServer> {
    return this.http.post<MCPServer>(this.apiUrl, request)
      .pipe(catchError(this.handleError));
  }

  updateMCPServer(id: string, request: UpdateMCPServerRequest): Observable<MCPServer> {
    return this.http.put<MCPServer>(`${this.apiUrl}/${id}`, request)
      .pipe(catchError(this.handleError));
  }

  listActiveForChat(chatId: string): Observable<MCPServer[]> {
    return this.http.get<MCPServer[]>(`${environment.apiUrl}/chat/${chatId}/mcp-servers`)
      .pipe(catchError(this.handleError));
  }

  listAvailableForChat(chatId: string, page: number = 1, limit: number = 10, filters?: MCPServerFilters): Observable<PaginatedResponse<MCPServer>> {
    const params: any = { page: page.toString(), limit: limit.toString() };
    if (filters?.search) {
      params.search = filters.search;
    }
    return this.http.get<PaginatedResponse<MCPServer>>(`${environment.apiUrl}/chat/${chatId}/available-mcp-servers`, { params })
      .pipe(catchError(this.handleError));
  }

  addToChat(chatId: string, mcpServerId: string): Observable<void> {
    return this.http.post<void>(`${environment.apiUrl}/chat/${chatId}/mcp-servers/${mcpServerId}`, {})
      .pipe(catchError(this.handleError));
  }

  removeFromChat(chatId: string, mcpServerId: string): Observable<void> {
    return this.http.delete<void>(`${environment.apiUrl}/chat/${chatId}/mcp-servers/${mcpServerId}`)
      .pipe(catchError(this.handleError));
  }

  deleteMCPServer(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`)
      .pipe(catchError(this.handleError));
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An error occurred');
    console.error('MCP Server Service Error:', errorMessage);
    return throwError(() => new Error(errorMessage));
  }
}


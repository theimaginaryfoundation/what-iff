import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { apiErrorMessage } from '../utils/api-error.helpers';
import {
  CreateWebhookTokenRequest,
  CreateWebhookTokenResponse,
  WebhookToken
} from '../models/webhook-token.model';

@Injectable({
  providedIn: 'root'
})
export class WebhookTokenService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/webhook-tokens`;

  listWebhookTokens(): Observable<WebhookToken[]> {
    return this.http.get<WebhookToken[]>(this.apiUrl).pipe(catchError(this.handleError));
  }

  createWebhookToken(request: CreateWebhookTokenRequest): Observable<CreateWebhookTokenResponse> {
    return this.http.post<CreateWebhookTokenResponse>(this.apiUrl, request).pipe(catchError(this.handleError));
  }

  revokeWebhookToken(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`).pipe(catchError(this.handleError));
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An error occurred');
    console.error('Webhook Token Service Error:', errorMessage);
    return throwError(() => new Error(errorMessage));
  }
}

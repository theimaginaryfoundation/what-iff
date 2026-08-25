import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { WebhookTokenService } from './webhook-token.service';

describe('WebhookTokenService', () => {
    let service: WebhookTokenService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                WebhookTokenService
            ]
        });

        service = TestBed.inject(WebhookTokenService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('should list webhook tokens', () => {
        service.listWebhookTokens().subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/webhook-tokens`);
        expect(req.request.method).toBe('GET');
        req.flush([]);
    });

    it('should create webhook token', () => {
        service.createWebhookToken({ name: 'Slack webhook' }).subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/webhook-tokens`);
        expect(req.request.method).toBe('POST');
        expect(req.request.body).toEqual({ name: 'Slack webhook' });
        req.flush({
            token: {
                id: 'tok-1',
                user_id: 'user-1',
                name: 'Slack webhook',
                status: 'active',
                created_at: '2026-04-06T00:00:00Z',
                updated_at: '2026-04-06T00:00:00Z'
            },
            api_token: 'wht_example'
        });
    });

    it('should revoke webhook token', () => {
        service.revokeWebhookToken('tok-1').subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/webhook-tokens/tok-1`);
        expect(req.request.method).toBe('DELETE');
        req.flush({});
    });
});

import { HttpErrorResponse, provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting, } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { environment } from '../../../environments/environment';
import { SearchResponse } from '../models/search.model';
import { SearchApiService } from './search-api.service';

describe('SearchApiService', () => {
    let service: SearchApiService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                SearchApiService,
            ],
        });
        service = TestBed.inject(SearchApiService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    function emptyResponse(query = 'atlas'): SearchResponse {
        return {
            query,
            sections: [
                { type: 'chat', results: [] },
                { type: 'personality', results: [] },
                { type: 'ritual', results: [] },
                { type: 'memory', results: [] },
                { type: 'image', results: [] },
            ],
        };
    }

    it('builds a basic GET with just the query', () => {
        let received: SearchResponse | undefined;
        service.search('atlas').subscribe(resp => (received = resp));

        const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/search`);
        expect(req.request.method).toBe('GET');
        expect(req.request.params.get('query')).toBe('atlas');
        expect(req.request.params.has('types')).toBe(false);
        expect(req.request.params.has('limit_per_type')).toBe(false);

        req.flush(emptyResponse());
        expect(received?.query).toBe('atlas');
        expect(received?.sections.length).toBe(5);
    });

    it('includes types csv and limit_per_type when provided', () => {
        service
            .search('atlas', { types: ['chat', 'personality', 'chat'], limitPerType: 10 })
            .subscribe();

        const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/search`);
        expect(req.request.params.get('query')).toBe('atlas');
        expect(req.request.params.get('types')).toBe('chat,personality');
        expect(req.request.params.get('limit_per_type')).toBe('10');
        req.flush(emptyResponse());
    });

    it('surfaces 4xx errors via catchError', async () => {
        let captured: Error | undefined;
        service.search('atlas').subscribe({
            error: err => (captured = err),
        });

        const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/search`);
        req.flush({ error: 'query is required' }, new HttpErrorResponse({ status: 400, statusText: 'Bad Request' }));

        await new Promise(resolve => setTimeout(resolve, 0));
        expect(captured).toBeDefined();
        expect(captured?.message).toContain('query is required');
    });
});
